package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"nexdrop/internal/adaptive"
	"nexdrop/internal/auth"
	"nexdrop/internal/delivery"
	"nexdrop/internal/enrollment"
	"nexdrop/internal/folder"
	"nexdrop/internal/messagelifecycle"
	"nexdrop/internal/recovery"
	"nexdrop/internal/relay"
	"nexdrop/internal/routing"
	"nexdrop/internal/v3"
)

func (store *Store) SaveGrant(ctx context.Context, grant enrollment.Grant) error {
	permissions, err := json.Marshal(grant.Permissions)
	if err != nil {
		return err
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO enrollment_grants (
			id, node_id, owner_user_id, token_hash, device_type,
			display_name_hint, permissions, maximum_uses, used_count,
			expires_at, revoked_at, created_at
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, $11, now())
	`, grant.ID, grant.NodeID, grant.OwnerID, grant.TokenHash, grant.DeviceType,
		grant.NameHint, permissions, grant.MaxUses, grant.Uses, grant.ExpiresAt, grant.RevokedAt)
	return err
}

func scanEnrollmentGrant(row pgx.Row) (enrollment.Grant, error) {
	var grant enrollment.Grant
	var permissions []byte
	err := row.Scan(
		&grant.ID, &grant.NodeID, &grant.OwnerID, &grant.TokenHash,
		&grant.DeviceType, &grant.NameHint, &permissions, &grant.MaxUses,
		&grant.Uses, &grant.ExpiresAt, &grant.RevokedAt,
	)
	if err != nil {
		return enrollment.Grant{}, err
	}
	if err := json.Unmarshal(permissions, &grant.Permissions); err != nil {
		return enrollment.Grant{}, err
	}
	return grant, nil
}

func loadEnrollmentGrantForUpdate(ctx context.Context, tx pgx.Tx, grantID string) (enrollment.Grant, error) {
	grant, err := scanEnrollmentGrant(tx.QueryRow(ctx, `
		SELECT id::text, node_id, owner_user_id::text, token_hash,
		       COALESCE(device_type, ''), COALESCE(display_name_hint, ''),
		       permissions, maximum_uses, used_count, expires_at, revoked_at
		FROM enrollment_grants
		WHERE id = $1
		FOR UPDATE
	`, grantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return enrollment.Grant{}, enrollment.ErrInvalidToken
	}
	return grant, err
}

func validateEnrollmentGrant(grant enrollment.Grant, tokenHash []byte, now time.Time) error {
	if !bytes.Equal(grant.TokenHash, tokenHash) {
		return enrollment.ErrInvalidToken
	}
	if grant.RevokedAt != nil {
		return enrollment.ErrRevoked
	}
	if !now.Before(grant.ExpiresAt) {
		return enrollment.ErrExpiredToken
	}
	if grant.Uses >= grant.MaxUses {
		return enrollment.ErrExhausted
	}
	return nil
}

func (store *Store) ConsumeGrant(ctx context.Context, grantID string, tokenHash []byte, now time.Time) (enrollment.Grant, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return enrollment.Grant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	grant, err := loadEnrollmentGrantForUpdate(ctx, tx, grantID)
	if err != nil {
		return enrollment.Grant{}, err
	}
	if err := validateEnrollmentGrant(grant, tokenHash, now); err != nil {
		return enrollment.Grant{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE enrollment_grants SET used_count = used_count + 1 WHERE id = $1`, grantID); err != nil {
		return enrollment.Grant{}, err
	}
	grant.Uses++
	return grant, tx.Commit(ctx)
}

func createDeviceCredentialTx(ctx context.Context, tx pgx.Tx, grant enrollment.Grant, record enrollment.CredentialRecord) (string, error) {
	var deviceID string
	err := tx.QueryRow(ctx, `
		INSERT INTO devices (user_id, display_name, device_type, trust_status)
		VALUES ($1, $2, $3, 'TRUSTED')
		RETURNING id::text
	`, record.OwnerID, record.Name, record.DeviceType).Scan(&deviceID)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO device_keys (device_id, public_key, key_algorithm)
		VALUES ($1, $2, $3)
	`, deviceID, record.PublicKey, record.KeyAlgorithm); err != nil {
		return "", err
	}
	permissions, err := json.Marshal(record.Permissions)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO device_credentials (
			device_id, credential_hash, permissions, issued_from_grant_id,
			credential_version, created_at
		) VALUES ($1, $2, $3, $4, 1, $5)
	`, deviceID, record.SecretHash, permissions, grant.ID, record.CreatedAt); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO device_credential_events (device_id, event_type, metadata, created_at)
		VALUES ($1, 'ISSUED', jsonb_build_object('grantId', $2::text), $3)
	`, deviceID, grant.ID, record.CreatedAt); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (actor_user_id, actor_device_id, action, target_type, target_id, metadata, created_at)
		VALUES ($1, NULL, 'DEVICE_ENROLLED', 'DEVICE', $2, jsonb_build_object('grantId', $3::text), $4)
	`, record.OwnerID, deviceID, grant.ID, record.CreatedAt); err != nil {
		return "", err
	}
	return deviceID, nil
}

func (store *Store) CreateDeviceCredential(ctx context.Context, grant enrollment.Grant, record enrollment.CredentialRecord) (string, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	deviceID, err := createDeviceCredentialTx(ctx, tx, grant, record)
	if err != nil {
		return "", err
	}
	return deviceID, tx.Commit(ctx)
}

func (store *Store) RedeemGrant(ctx context.Context, grantID string, tokenHash []byte, now time.Time, record enrollment.CredentialRecord) (enrollment.Grant, string, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return enrollment.Grant{}, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	grant, err := loadEnrollmentGrantForUpdate(ctx, tx, grantID)
	if err != nil {
		return enrollment.Grant{}, "", err
	}
	if err := validateEnrollmentGrant(grant, tokenHash, now); err != nil {
		return enrollment.Grant{}, "", err
	}
	if grant.DeviceType != "" && grant.DeviceType != record.DeviceType {
		return enrollment.Grant{}, "", enrollment.ErrInvalidToken
	}
	if grant.OwnerID != "" && grant.OwnerID != record.OwnerID {
		return enrollment.Grant{}, "", enrollment.ErrInvalidToken
	}
	if grant.NameHint != "" && record.Name == "" {
		record.Name = grant.NameHint
	}
	record.Permissions = grant.Permissions
	deviceID, err := createDeviceCredentialTx(ctx, tx, grant, record)
	if err != nil {
		return enrollment.Grant{}, "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE enrollment_grants SET used_count = used_count + 1 WHERE id = $1`, grantID); err != nil {
		return enrollment.Grant{}, "", err
	}
	grant.Uses++
	if err := tx.Commit(ctx); err != nil {
		return enrollment.Grant{}, "", err
	}
	return grant, deviceID, nil
}

func (store *Store) RevokeGrant(ctx context.Context, grantID string, now time.Time) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE enrollment_grants SET revoked_at = COALESCE(revoked_at, $2)
		WHERE id = $1
	`, grantID, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return enrollment.ErrInvalidToken
	}
	return nil
}

func (store *Store) AttachSessionByCredential(ctx context.Context, session auth.Session, deviceID string, credentialHash []byte, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
		UPDATE user_sessions s
		SET device_id = d.id
		FROM devices d
		JOIN device_credentials c ON c.device_id = d.id
		WHERE s.id = $1
		  AND s.user_id = $2
		  AND s.revoked_at IS NULL
		  AND d.id = $3
		  AND d.user_id = $2
		  AND d.revoked_at IS NULL
		  AND c.credential_hash = $4
		  AND c.revoked_at IS NULL
	`, session.SessionID, session.ID, deviceID, credentialHash)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `
		UPDATE device_credentials
		SET last_used_at = $2, last_session_id = $3
		WHERE device_id = $1
	`, deviceID, now, session.SessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO device_credential_events (device_id, event_type, session_id, created_at)
		VALUES ($1, 'SESSION_ATTACHED', $2, $3)
	`, deviceID, session.SessionID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) RevokeDeviceCredential(ctx context.Context, session auth.Session, deviceID string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
		UPDATE device_credentials c
		SET revoked_at = COALESCE(c.revoked_at, $3)
		FROM devices d
		WHERE c.device_id = d.id
		  AND d.id = $1
		  AND (d.user_id = $2 OR $4::boolean)
	`, deviceID, session.ID, now, session.Admin && session.AdminVerified)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE device_id = $1`, deviceID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO device_credential_events (device_id, event_type, session_id, created_at)
		VALUES ($1, 'REVOKED', $2, $3)
	`, deviceID, session.SessionID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) DeviceAccessible(ctx context.Context, session auth.Session, deviceID string) (bool, error) {
	var accessible bool
	err := store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM devices d
			WHERE d.id = $1 AND d.revoked_at IS NULL
			  AND (
				d.user_id = $2
				OR EXISTS (
					SELECT 1 FROM group_devices gd
					JOIN group_members gm ON gm.group_id = gd.group_id
					WHERE gd.device_id = d.id AND gm.user_id = $2
				)
			  )
		)
	`, deviceID, session.ID).Scan(&accessible)
	return accessible, err
}

func (store *Store) RouteHistory(ctx context.Context, session auth.Session, deviceID string) ([]v3.RouteHistory, error) {
	accessible, err := store.DeviceAccessible(ctx, session, deviceID)
	if err != nil || !accessible {
		if err != nil {
			return nil, err
		}
		return nil, auth.ErrForbidden
	}
	rows, err := store.pool.Query(ctx, `
		SELECT route_id,
		       COALESCE(handshake_latency_ms, 0),
		       COALESCE(throughput_bytes_per_second, 0),
		       CASE WHEN success_count + failure_count = 0 THEN 0
		            ELSE failure_count::double precision / (success_count + failure_count) END,
		       last_success_at
		FROM device_route_history
		WHERE device_id = $1
	`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []v3.RouteHistory
	for rows.Next() {
		var value v3.RouteHistory
		var latency int64
		if err := rows.Scan(&value.RouteID, &latency, &value.ThroughputBPS, &value.FailureRate, &value.LastSuccess); err != nil {
			return nil, err
		}
		value.HandshakeLatency = time.Duration(latency) * time.Millisecond
		result = append(result, value)
	}
	return result, rows.Err()
}

func (store *Store) RecordRouteObservation(ctx context.Context, session auth.Session, observation v3.RouteObservation) error {
	endpointHash := sha256.Sum256([]byte(strings.TrimSpace(observation.Endpoint)))
	_, err := store.pool.Exec(ctx, `
		INSERT INTO device_route_history (
			device_id, route_id, route_kind, endpoint_hash,
			handshake_latency_ms, throughput_bytes_per_second,
			success_count, failure_count, last_success_at, last_failure_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			CASE WHEN $7 THEN 1 ELSE 0 END,
			CASE WHEN $7 THEN 0 ELSE 1 END,
			CASE WHEN $7 THEN $8 ELSE NULL END,
			CASE WHEN $7 THEN NULL ELSE $8 END,
			$8
		)
		ON CONFLICT (device_id, route_id) DO UPDATE SET
			route_kind = EXCLUDED.route_kind,
			endpoint_hash = EXCLUDED.endpoint_hash,
			handshake_latency_ms = CASE
				WHEN device_route_history.handshake_latency_ms IS NULL THEN EXCLUDED.handshake_latency_ms
				ELSE ((device_route_history.handshake_latency_ms * 3) + EXCLUDED.handshake_latency_ms) / 4 END,
			throughput_bytes_per_second = CASE
				WHEN device_route_history.throughput_bytes_per_second IS NULL THEN EXCLUDED.throughput_bytes_per_second
				ELSE ((device_route_history.throughput_bytes_per_second * 3) + EXCLUDED.throughput_bytes_per_second) / 4 END,
			success_count = device_route_history.success_count + EXCLUDED.success_count,
			failure_count = device_route_history.failure_count + EXCLUDED.failure_count,
			last_success_at = COALESCE(EXCLUDED.last_success_at, device_route_history.last_success_at),
			last_failure_at = COALESCE(EXCLUDED.last_failure_at, device_route_history.last_failure_at),
			updated_at = EXCLUDED.updated_at
	`, observation.DeviceID, observation.RouteID, observation.Kind, endpointHash[:], observation.LatencyMillis,
		int64(observation.ThroughputBPS), observation.Success, observation.ObservedAt)
	return err
}

func selectedRoute(kind routing.Kind) string {
	if kind == routing.KindNode {
		return "NODE"
	}
	return "LAN"
}

func (store *Store) SwitchTransferRoute(ctx context.Context, session auth.Session, request v3.RouteSwitch, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	status := "TRANSFERRING_LAN"
	if request.Route == routing.KindNode {
		status = "QUEUED"
	}
	var targetID string
	err = tx.QueryRow(ctx, `
		UPDATE transfer_targets tt
		SET selected_route = $4,
		    active_route_id = $5,
		    fallback_reason = NULLIF($6, ''),
		    route_changed_at = $7,
		    status = CASE WHEN tt.status IN ('DELIVERED', 'READ', 'CANCELLED', 'EXPIRED') THEN tt.status ELSE $8 END
		FROM transfer_tasks task
		WHERE tt.transfer_id = task.id
		  AND tt.transfer_id = $1
		  AND tt.target_device_id = $2
		  AND (task.sender_user_id = $3 OR tt.target_device_id = $9)
		RETURNING tt.id::text
	`, request.TransferID, request.DeviceID, session.ID, selectedRoute(request.Route), request.Route,
		request.Reason, now, status, nullableDeviceID(session.DeviceID)).Scan(&targetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrForbidden
	}
	if err != nil {
		return err
	}
	verified, err := json.Marshal(request.VerifiedChunks)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transfer_file_targets fft
		SET selected_route = $3,
		    status = CASE WHEN fft.status IN ('DELIVERED', 'READ', 'CANCELLED', 'EXPIRED') THEN fft.status ELSE $4 END,
		    verified_chunks = verified_chunks || $5::jsonb
		FROM files f
		WHERE fft.file_id = f.id
		  AND f.transfer_id = $1
		  AND fft.target_device_id = $2
	`, request.TransferID, request.DeviceID, selectedRoute(request.Route), status, verified); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transfer_routes SET ended_at = COALESCE(ended_at, $2), error_code = NULLIF($3, '')
		WHERE transfer_target_id = $1 AND ended_at IS NULL
	`, targetID, now, request.Reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transfer_routes (transfer_target_id, route, started_at)
		VALUES ($1, $2, $3)
	`, targetID, request.Route, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transfer_timeline_events (
			transfer_id, target_device_id, event_code, route, status, error_code, occurred_at
		) VALUES ($1, $2, 'ROUTE_FALLBACK_SELECTED', $3, $4, NULLIF($5, ''), $6)
	`, request.TransferID, request.DeviceID, request.Route, status, request.Reason, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) SetAdaptiveProfile(ctx context.Context, session auth.Session, transferID string, profile adaptive.Profile, now time.Time) error {
	encoded, err := json.Marshal(map[string]any{"chunkSize": profile.ChunkSize, "parallelChunks": profile.ParallelChunks, "updatedAt": now})
	if err != nil {
		return err
	}
	command, err := store.pool.Exec(ctx, `
		UPDATE transfer_tasks task
		SET adaptive_profile = $3
		WHERE task.id = $1
		  AND (task.sender_user_id = $2 OR EXISTS (
			SELECT 1 FROM transfer_targets tt WHERE tt.transfer_id = task.id AND tt.target_device_id = $4
		  ))
	`, transferID, session.ID, encoded, nullableDeviceID(session.DeviceID))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	return nil
}

func (store *Store) UpsertDeliveryPolicy(ctx context.Context, session auth.Session, record v3.DeliveryRecord) (v3.DeliveryRecord, error) {
	command, err := store.pool.Exec(ctx, `
		INSERT INTO transfer_delivery_policies (
			transfer_id, target_device_id, priority, wifi_only, charging_only,
			maximum_mobile_data_bytes, automatic_download, foreground_only,
			state, reason_code, expires_at, size_bytes, metadata_only, updated_at
		)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11, $12, false, $13
		FROM transfer_tasks task
		WHERE task.id = $1
		  AND (task.sender_user_id = $14 OR $2 = $15)
		ON CONFLICT (transfer_id, target_device_id) DO UPDATE SET
			priority = EXCLUDED.priority,
			wifi_only = EXCLUDED.wifi_only,
			charging_only = EXCLUDED.charging_only,
			maximum_mobile_data_bytes = EXCLUDED.maximum_mobile_data_bytes,
			automatic_download = EXCLUDED.automatic_download,
			foreground_only = EXCLUDED.foreground_only,
			expires_at = EXCLUDED.expires_at,
			size_bytes = EXCLUDED.size_bytes,
			updated_at = EXCLUDED.updated_at
	`, record.TransferID, record.TargetDeviceID, record.Priority, record.Policy.WiFiOnly, record.Policy.ChargingOnly,
		record.Policy.MaxMobileDataSize, record.Policy.AutomaticDownload, record.Policy.ForegroundOnly,
		record.State, record.ReasonCode, nullTime(record.Policy.ExpiresAt), record.Size, record.UpdatedAt,
		session.ID, nullableDeviceID(session.DeviceID))
	if err != nil {
		return v3.DeliveryRecord{}, err
	}
	if command.RowsAffected() == 0 {
		return v3.DeliveryRecord{}, auth.ErrForbidden
	}
	return store.DeliveryPolicy(ctx, session, record.TransferID, record.TargetDeviceID)
}

func (store *Store) DeliveryPolicy(ctx context.Context, session auth.Session, transferID, targetDeviceID string) (v3.DeliveryRecord, error) {
	var record v3.DeliveryRecord
	var expires *time.Time
	var metadataAt, bodyAt *time.Time
	err := store.pool.QueryRow(ctx, `
		SELECT p.transfer_id::text, p.target_device_id::text, p.priority,
		       p.size_bytes, p.wifi_only, p.charging_only,
		       p.maximum_mobile_data_bytes, p.automatic_download,
		       p.foreground_only, p.state, COALESCE(p.reason_code, ''),
		       p.expires_at, p.metadata_synchronized_at, p.body_downloaded_at,
		       p.updated_at
		FROM transfer_delivery_policies p
		JOIN transfer_tasks task ON task.id = p.transfer_id
		WHERE p.transfer_id = $1 AND p.target_device_id = $2
		  AND (task.sender_user_id = $3 OR p.target_device_id = $4 OR $5::boolean)
	`, transferID, targetDeviceID, session.ID, nullableDeviceID(session.DeviceID), session.Admin).Scan(
		&record.TransferID, &record.TargetDeviceID, &record.Priority, &record.Size,
		&record.Policy.WiFiOnly, &record.Policy.ChargingOnly, &record.Policy.MaxMobileDataSize,
		&record.Policy.AutomaticDownload, &record.Policy.ForegroundOnly, &record.State,
		&record.ReasonCode, &expires, &metadataAt, &bodyAt, &record.UpdatedAt,
	)
	if err != nil {
		return v3.DeliveryRecord{}, err
	}
	if expires != nil {
		record.Policy.ExpiresAt = *expires
	}
	record.MetadataSynchronized = metadataAt != nil
	record.BodyDownloaded = bodyAt != nil
	return record, nil
}

func (store *Store) ListDeliveryQueue(ctx context.Context, session auth.Session, targetDeviceID string, limit int) ([]v3.DeliveryRecord, error) {
	if session.DeviceID == nil || *session.DeviceID != targetDeviceID {
		return nil, auth.ErrForbidden
	}
	rows, err := store.pool.Query(ctx, `
		SELECT transfer_id::text
		FROM transfer_delivery_policies
		WHERE target_device_id = $1
		  AND state NOT IN ('DELIVERED', 'READ', 'EXPIRED', 'CANCELLED')
		ORDER BY priority DESC, metadata_synchronized_at NULLS FIRST, size_bytes, updated_at
		LIMIT $2
	`, targetDeviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]v3.DeliveryRecord, 0, len(ids))
	for _, id := range ids {
		record, err := store.DeliveryPolicy(ctx, session, id, targetDeviceID)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}

func (store *Store) UpdateDeliveryDecision(ctx context.Context, session auth.Session, transferID, targetDeviceID string, decision delivery.Decision, now time.Time) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE transfer_delivery_policies
		SET state = $4, reason_code = NULLIF($5, ''), metadata_only = $6,
		    metadata_synchronized_at = CASE WHEN $4 = 'METADATA_AVAILABLE' THEN COALESCE(metadata_synchronized_at, $7) ELSE metadata_synchronized_at END,
		    body_downloaded_at = CASE WHEN $4 IN ('DELIVERED', 'READ') THEN COALESCE(body_downloaded_at, $7) ELSE body_downloaded_at END,
		    updated_at = $7
		WHERE transfer_id = $1 AND target_device_id = $2
		  AND ($2 = $3 OR $8::boolean)
	`, transferID, targetDeviceID, nullableDeviceID(session.DeviceID), decision.State, decision.ReasonCode, decision.MetadataOnly, now, session.Admin)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	return nil
}

func (store *Store) RegisterRelay(ctx context.Context, session auth.Session, record v3.RelayRecord, now time.Time) (relay.Relay, error) {
	var result relay.Relay
	retentionSeconds := int(record.Retention / time.Second)
	err := store.pool.QueryRow(ctx, `
		INSERT INTO relays (
			id, endpoint, region, identity_public_key, credential_hash,
			capacity_bytes, used_bytes, healthy, draining,
			retention_seconds, created_at, updated_at
		) VALUES (COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()), $2, $3, $4, $5, $6, 0, false, false, $7, $8, $8)
		RETURNING id::text, endpoint, region, healthy, draining, capacity_bytes,
		          used_bytes, COALESCE(latency_ms, 0), failure_rate,
		          COALESCE(last_seen_at, 'epoch'::timestamptz)
	`, record.Relay.ID, record.Relay.Endpoint, record.Relay.Region, record.IdentityKey,
		record.CredentialHash, record.Relay.CapacityBytes, retentionSeconds, now).Scan(
		&result.ID, &result.Endpoint, &result.Region, &result.Healthy, &result.Draining,
		&result.CapacityBytes, &result.UsedBytes, durationMillisScanner{target: &result.Latency},
		&result.FailureRate, &result.LastSuccessful,
	)
	if err != nil {
		return relay.Relay{}, err
	}
	_, _ = store.pool.Exec(ctx, `
		INSERT INTO audit_logs (actor_user_id, actor_device_id, action, target_type, target_id, created_at)
		VALUES ($1, $2, 'RELAY_REGISTERED', 'RELAY', $3, $4)
	`, session.ID, nullableDeviceID(session.DeviceID), result.ID, now)
	return result, nil
}

type durationMillisScanner struct{ target *time.Duration }

func (scanner durationMillisScanner) Scan(src any) error {
	var value int64
	switch typed := src.(type) {
	case int64:
		value = typed
	case int32:
		value = int64(typed)
	case nil:
		value = 0
	default:
		return fmt.Errorf("scan duration milliseconds from %T", src)
	}
	*scanner.target = time.Duration(value) * time.Millisecond
	return nil
}

func (store *Store) ListRelays(ctx context.Context, _ auth.Session) ([]relay.Relay, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id::text, endpoint, region, healthy, draining, capacity_bytes,
		       used_bytes, COALESCE(latency_ms, 0), failure_rate,
		       COALESCE(last_seen_at, 'epoch'::timestamptz)
		FROM relays
		ORDER BY region, endpoint
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []relay.Relay
	for rows.Next() {
		var item relay.Relay
		var latency int64
		if err := rows.Scan(&item.ID, &item.Endpoint, &item.Region, &item.Healthy, &item.Draining,
			&item.CapacityBytes, &item.UsedBytes, &latency, &item.FailureRate, &item.LastSuccessful); err != nil {
			return nil, err
		}
		item.Latency = time.Duration(latency) * time.Millisecond
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) RelayByID(ctx context.Context, relayID string) (relay.Relay, error) {
	var item relay.Relay
	var latency int64
	err := store.pool.QueryRow(ctx, `
		SELECT id::text, endpoint, region, healthy, draining, capacity_bytes,
		       used_bytes, COALESCE(latency_ms, 0), failure_rate,
		       COALESCE(last_seen_at, 'epoch'::timestamptz)
		FROM relays WHERE id = $1
	`, relayID).Scan(&item.ID, &item.Endpoint, &item.Region, &item.Healthy, &item.Draining,
		&item.CapacityBytes, &item.UsedBytes, &latency, &item.FailureRate, &item.LastSuccessful)
	item.Latency = time.Duration(latency) * time.Millisecond
	return item, err
}

func (store *Store) RelayHeartbeat(ctx context.Context, heartbeat v3.RelayHeartbeat, credentialHash []byte, now time.Time) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE relays
		SET used_bytes = $3, latency_ms = $4, failure_rate = $5,
		    healthy = $6, last_seen_at = $7, updated_at = $7
		WHERE id = $1 AND credential_hash = $2 AND used_bytes <= capacity_bytes
	`, heartbeat.RelayID, credentialHash, heartbeat.UsedBytes, heartbeat.LatencyMillis,
		heartbeat.FailureRate, heartbeat.Healthy, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	return nil
}

func (store *Store) SetRelayDraining(ctx context.Context, session auth.Session, relayID string, draining bool, now time.Time) error {
	command, err := store.pool.Exec(ctx, `UPDATE relays SET draining = $2, updated_at = $3 WHERE id = $1`, relayID, draining, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	_, _ = store.pool.Exec(ctx, `
		INSERT INTO audit_logs (actor_user_id, actor_device_id, action, target_type, target_id, metadata, created_at)
		VALUES ($1, $2, 'RELAY_DRAIN_CHANGED', 'RELAY', $3, jsonb_build_object('draining', $4), $5)
	`, session.ID, nullableDeviceID(session.DeviceID), relayID, draining, now)
	return nil
}

func (store *Store) RemoveRelay(ctx context.Context, session auth.Session, relayID string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM relay_chunk_assignments WHERE relay_id = $1 AND expires_at > $2`, relayID, now).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return v3.ErrConflict
	}
	command, err := tx.Exec(ctx, `DELETE FROM relays WHERE id = $1`, relayID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (actor_user_id, actor_device_id, action, target_type, target_id, created_at)
		VALUES ($1, $2, 'RELAY_REMOVED', 'RELAY', $3, $4)
	`, session.ID, nullableDeviceID(session.DeviceID), relayID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) AssignRelayChunks(ctx context.Context, session auth.Session, relayID, fileID string, firstChunk, lastChunk int, expiresAt time.Time) error {
	command, err := store.pool.Exec(ctx, `
		INSERT INTO relay_chunk_assignments (relay_id, file_id, first_chunk, last_chunk, expires_at)
		SELECT $1, f.id, $3, $4, $5
		FROM files f
		JOIN transfer_tasks task ON task.id = f.transfer_id
		LEFT JOIN transfer_targets target ON target.transfer_id = task.id AND target.target_device_id = $7
		WHERE f.id = $2 AND (task.sender_user_id = $6 OR target.id IS NOT NULL)
		ON CONFLICT (relay_id, file_id, first_chunk) DO UPDATE SET
			last_chunk = EXCLUDED.last_chunk, expires_at = EXCLUDED.expires_at
	`, relayID, fileID, firstChunk, lastChunk, expiresAt, session.ID, nullableDeviceID(session.DeviceID))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	return nil
}

func (store *Store) SaveFolderManifest(ctx context.Context, session auth.Session, transferID string, manifest folder.Manifest, digest []byte, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM transfer_tasks WHERE id = $1 AND sender_user_id = $2)`, transferID, session.ID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return auth.ErrForbidden
	}
	var total int64
	for _, entry := range manifest.Entries {
		total += entry.Size
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO folder_manifests (transfer_id, manifest_version, root_name, entry_count, total_size, manifest_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (transfer_id) DO UPDATE SET
			manifest_version = EXCLUDED.manifest_version,
			root_name = EXCLUDED.root_name,
			entry_count = EXCLUDED.entry_count,
			total_size = EXCLUDED.total_size,
			manifest_hash = EXCLUDED.manifest_hash
	`, transferID, manifest.Version, manifest.RootName, len(manifest.Entries), total, digest, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM folder_manifest_entries WHERE transfer_id = $1`, transferID); err != nil {
		return err
	}
	for index, entry := range manifest.Entries {
		var fileID any
		if entry.FileID != "" {
			fileID = entry.FileID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO folder_manifest_entries (
				transfer_id, entry_index, normalized_path, entry_type, file_id,
				modified_at, selected, conflict_action, size, sha256, mime_type
			) VALUES ($1, $2, $3, $4, $5, $6, true, 'ASK', $7, NULLIF($8, ''), NULLIF($9, ''))
		`, transferID, index, entry.Path, entry.Type, fileID, nullTime(entry.ModifiedAt), entry.Size, entry.SHA256, entry.MIMEType); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (store *Store) FolderManifest(ctx context.Context, session auth.Session, transferID string) (folder.Manifest, error) {
	var manifest folder.Manifest
	err := store.pool.QueryRow(ctx, `
		SELECT fm.manifest_version, fm.root_name
		FROM folder_manifests fm
		JOIN transfer_tasks task ON task.id = fm.transfer_id
		LEFT JOIN transfer_targets target ON target.transfer_id = task.id AND target.target_device_id = $3
		WHERE fm.transfer_id = $1 AND ($2 = '' OR task.sender_user_id = $2 OR target.id IS NOT NULL)
	`, transferID, session.ID, nullableDeviceID(session.DeviceID)).Scan(&manifest.Version, &manifest.RootName)
	if err != nil {
		return folder.Manifest{}, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT normalized_path, entry_type, size, modified_at,
		       COALESCE(sha256, ''), COALESCE(mime_type, ''), COALESCE(file_id::text, '')
		FROM folder_manifest_entries
		WHERE transfer_id = $1
		ORDER BY entry_index
	`, transferID)
	if err != nil {
		return folder.Manifest{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry folder.Entry
		if err := rows.Scan(&entry.Path, &entry.Type, &entry.Size, &entry.ModifiedAt, &entry.SHA256, &entry.MIMEType, &entry.FileID); err != nil {
			return folder.Manifest{}, err
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	return manifest, rows.Err()
}

func (store *Store) SaveFolderSelection(ctx context.Context, session auth.Session, transferID, targetDeviceID string, selections []v3.FolderSelection, now time.Time) error {
	if session.DeviceID == nil || *session.DeviceID != targetDeviceID {
		return auth.ErrForbidden
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM transfer_targets WHERE transfer_id = $1 AND target_device_id = $2)`, transferID, targetDeviceID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return auth.ErrForbidden
	}
	for _, selection := range selections {
		if _, err := tx.Exec(ctx, `
			INSERT INTO folder_manifest_selections (
				transfer_id, target_device_id, entry_index, accepted, conflict_action, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (transfer_id, target_device_id, entry_index) DO UPDATE SET
				accepted = EXCLUDED.accepted,
				conflict_action = EXCLUDED.conflict_action,
				updated_at = EXCLUDED.updated_at
		`, transferID, targetDeviceID, selection.EntryIndex, selection.Accepted, selection.Conflict, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (store *Store) FolderSelection(ctx context.Context, session auth.Session, transferID, targetDeviceID string) ([]v3.FolderSelection, error) {
	if session.DeviceID == nil || (*session.DeviceID != targetDeviceID && !session.Admin) {
		return nil, auth.ErrForbidden
	}
	rows, err := store.pool.Query(ctx, `
		SELECT entry_index, accepted, conflict_action
		FROM folder_manifest_selections
		WHERE transfer_id = $1 AND target_device_id = $2
		ORDER BY entry_index
	`, transferID, targetDeviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []v3.FolderSelection
	for rows.Next() {
		var selection v3.FolderSelection
		if err := rows.Scan(&selection.EntryIndex, &selection.Accepted, &selection.Conflict); err != nil {
			return nil, err
		}
		result = append(result, selection)
	}
	return result, rows.Err()
}

func conversationGroup(value string) (*string, error) {
	if value == "inbox" {
		return nil, nil
	}
	if strings.HasPrefix(value, "group:") && len(strings.TrimPrefix(value, "group:")) >= 8 {
		groupID := strings.TrimPrefix(value, "group:")
		return &groupID, nil
	}
	return nil, v3.ErrInvalid
}

func nullableDeviceID(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (store *Store) MessagePage(ctx context.Context, session auth.Session, conversationKey string, cursor messagelifecycle.Cursor, limit int) ([]v3.MessageView, messagelifecycle.Cursor, error) {
	groupID, err := conversationGroup(conversationKey)
	if err != nil {
		return nil, messagelifecycle.Cursor{}, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT m.id::text, COALESCE(m.transfer_id::text, ''), COALESCE(m.group_id::text, ''),
		       m.sender_user_id::text, m.sender_device_id::text, m.content_type,
		       COALESCE(m.encrypted_content, ''::bytea), m.created_at, m.expires_at,
		       COALESCE((SELECT sum(f.size) FROM files f WHERE f.transfer_id = m.transfer_id), 0),
		       NOT EXISTS (
			SELECT 1 FROM transfer_targets tt
			WHERE tt.transfer_id = m.transfer_id AND tt.status NOT IN ('DELIVERED', 'READ')
		       ),
		       COALESCE(m.pinned, false), mt.issued_at
		FROM messages m
		LEFT JOIN message_tombstones mt ON mt.message_id = m.id
		WHERE (($3::uuid IS NULL AND m.group_id IS NULL) OR m.group_id = $3)
		  AND (
			$2 = '' OR m.sender_user_id = $2
			OR EXISTS (
				SELECT 1 FROM message_targets target
				JOIN devices d ON d.id = target.target_device_id
				WHERE target.message_id = m.id AND d.user_id = $2
			)
			OR EXISTS (
				SELECT 1 FROM group_members gm WHERE gm.group_id = m.group_id AND gm.user_id = $2
			)
		  )
		  AND ($4::uuid IS NULL OR NOT EXISTS (
			SELECT 1 FROM message_local_removals removed
			WHERE removed.message_id = m.id AND removed.device_id = $4
		  ))
		  AND ($5::timestamptz IS NULL OR m.created_at < $5 OR (m.created_at = $5 AND m.id::text < $6))
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $1
	`, limit, session.ID, groupID, nullableDeviceID(session.DeviceID), nullTime(cursor.CreatedAt), cursor.ID)
	if err != nil {
		return nil, messagelifecycle.Cursor{}, err
	}
	defer rows.Close()
	var result []v3.MessageView
	for rows.Next() {
		var item v3.MessageView
		if err := rows.Scan(
			&item.ID, &item.TransferID, &item.GroupID, &item.SenderUserID,
			&item.SenderDeviceID, &item.ContentType, &item.EncryptedContent,
			&item.CreatedAt, &item.ExpiresAt, &item.AttachmentBytes,
			&item.EveryTargetFetched, &item.Pinned, &item.TombstonedAt,
		); err != nil {
			return nil, messagelifecycle.Cursor{}, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, messagelifecycle.Cursor{}, err
	}
	var next messagelifecycle.Cursor
	if len(result) == limit {
		last := result[len(result)-1]
		next = messagelifecycle.Cursor{ID: last.ID, CreatedAt: last.CreatedAt}
	}
	return result, next, nil
}

func (store *Store) AdvanceMessageRead(ctx context.Context, session auth.Session, conversationKey string, candidate messagelifecycle.ReadCursor, now time.Time) (messagelifecycle.ReadCursor, error) {
	if session.DeviceID == nil {
		return messagelifecycle.ReadCursor{}, auth.ErrForbidden
	}
	groupID, err := conversationGroup(conversationKey)
	if err != nil {
		return messagelifecycle.ReadCursor{}, err
	}
	current := messagelifecycle.ReadCursor{}
	_ = store.pool.QueryRow(ctx, `
		SELECT last_created_at, COALESCE(last_message_id::text, '')
		FROM message_read_cursors
		WHERE device_id = $1 AND conversation_key = $2
	`, *session.DeviceID, conversationKey).Scan(&current.CreatedAt, &current.MessageID)
	next := messagelifecycle.AdvanceRead(current, candidate)
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO message_read_cursors (
			device_id, group_id, conversation_key, last_message_id, last_created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (device_id, conversation_key) DO UPDATE SET
			group_id = EXCLUDED.group_id,
			last_message_id = EXCLUDED.last_message_id,
			last_created_at = EXCLUDED.last_created_at,
			updated_at = EXCLUDED.updated_at
	`, *session.DeviceID, groupID, conversationKey, next.MessageID, next.CreatedAt, now); err != nil {
		return messagelifecycle.ReadCursor{}, err
	}
	return next, nil
}

func (store *Store) messageAccessible(ctx context.Context, session auth.Session, messageID string) (bool, error) {
	if session.ID == "" {
		return true, nil
	}
	var allowed bool
	err := store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM messages m
			WHERE m.id = $1 AND (
				m.sender_user_id = $2
				OR EXISTS (
					SELECT 1 FROM message_targets target
					JOIN devices d ON d.id = target.target_device_id
					WHERE target.message_id = m.id AND d.user_id = $2
				)
				OR EXISTS (SELECT 1 FROM group_members gm WHERE gm.group_id = m.group_id AND gm.user_id = $2)
			)
		)
	`, messageID, session.ID).Scan(&allowed)
	return allowed, err
}

func (store *Store) SaveMessageTombstone(ctx context.Context, session auth.Session, tombstone messagelifecycle.Tombstone, signature []byte, now time.Time) error {
	allowed, err := store.messageAccessible(ctx, session, tombstone.MessageID)
	if err != nil || !allowed {
		if err != nil {
			return err
		}
		return auth.ErrForbidden
	}
	if session.DeviceID != nil && tombstone.ActorID != *session.DeviceID {
		return auth.ErrForbidden
	}
	var actor any
	if tombstone.ActorID != "retention-worker" {
		actor = tombstone.ActorID
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO message_tombstones (
			message_id, scope, actor_device_id, version, signature, issued_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (message_id) DO UPDATE SET
			scope = EXCLUDED.scope,
			actor_device_id = EXCLUDED.actor_device_id,
			version = GREATEST(message_tombstones.version, EXCLUDED.version),
			signature = EXCLUDED.signature,
			issued_at = EXCLUDED.issued_at,
			expires_at = EXCLUDED.expires_at
	`, tombstone.MessageID, tombstone.Scope, actor, tombstone.Version, signature,
		time.Unix(tombstone.IssuedAt, 0), time.Unix(tombstone.ExpiresAt, 0))
	return err
}

func (store *Store) MarkMessageLocalRemoved(ctx context.Context, session auth.Session, messageID string, now time.Time) error {
	if session.DeviceID == nil {
		return auth.ErrForbidden
	}
	allowed, err := store.messageAccessible(ctx, session, messageID)
	if err != nil || !allowed {
		if err != nil {
			return err
		}
		return auth.ErrForbidden
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO message_local_removals (message_id, device_id, removed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (message_id, device_id) DO UPDATE SET removed_at = EXCLUDED.removed_at
	`, messageID, *session.DeviceID, now)
	return err
}

func (store *Store) AttachmentPaths(ctx context.Context, session auth.Session, messageID string) ([]string, error) {
	allowed, err := store.messageAccessible(ctx, session, messageID)
	if err != nil || !allowed {
		if err != nil {
			return nil, err
		}
		return nil, auth.ErrForbidden
	}
	rows, err := store.pool.Query(ctx, `
		SELECT f.storage_path
		FROM files f
		JOIN messages m ON m.transfer_id = f.transfer_id
		WHERE m.id = $1
		UNION
		SELECT chunk.storage_path
		FROM file_chunks chunk
		JOIN files f ON f.id = chunk.file_id
		JOIN messages m ON m.transfer_id = f.transfer_id
		WHERE m.id = $1
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		result = append(result, path)
	}
	return result, rows.Err()
}

func (store *Store) MarkAttachmentBodyDeleted(ctx context.Context, session auth.Session, messageID string, now time.Time) error {
	allowed, err := store.messageAccessible(ctx, session, messageID)
	if err != nil || !allowed {
		if err != nil {
			return err
		}
		return auth.ErrForbidden
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		DELETE FROM file_chunks
		WHERE file_id IN (SELECT f.id FROM files f JOIN messages m ON m.transfer_id = f.transfer_id WHERE m.id = $1)
	`, messageID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE files SET status = 'PURGED', completed_at = COALESCE(completed_at, $2)
		WHERE transfer_id = (SELECT transfer_id FROM messages WHERE id = $1)
	`, messageID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) SetRetentionPolicy(ctx context.Context, session auth.Session, conversationKey string, policy messagelifecycle.RetentionPolicy, tombstoneDays int, now time.Time) error {
	groupID, err := conversationGroup(conversationKey)
	if err != nil {
		return err
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO message_retention_policies (
			group_id, conversation_key, message_mode, message_days,
			attachment_mode, attachment_days, tombstone_days, updated_at
		) VALUES ($1, $2, $3, $4, $3, $4, $5, $6)
		ON CONFLICT (conversation_key) DO UPDATE SET
			group_id = EXCLUDED.group_id,
			message_mode = EXCLUDED.message_mode,
			message_days = EXCLUDED.message_days,
			attachment_mode = EXCLUDED.attachment_mode,
			attachment_days = EXCLUDED.attachment_days,
			tombstone_days = EXCLUDED.tombstone_days,
			updated_at = EXCLUDED.updated_at
	`, groupID, conversationKey, policy.Mode, nullableDays(policy.Days), tombstoneDays, now)
	if err == nil {
		_, _ = store.pool.Exec(ctx, `
			INSERT INTO audit_logs (actor_user_id, actor_device_id, action, target_type, metadata, created_at)
			VALUES ($1, $2, 'RETENTION_POLICY_UPDATED', 'CONVERSATION', jsonb_build_object('conversationKey', $3), $4)
		`, session.ID, nullableDeviceID(session.DeviceID), conversationKey, now)
	}
	return err
}

func nullableDays(days int) any {
	if days <= 0 {
		return nil
	}
	return days
}

func (store *Store) RetentionCandidates(ctx context.Context, now time.Time, limit int) ([]v3.RetentionCandidate, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT m.id::text, COALESCE(m.transfer_id::text, ''), COALESCE(m.group_id::text, ''),
		       m.sender_user_id::text, m.sender_device_id::text, m.content_type,
		       m.created_at, m.expires_at,
		       COALESCE((SELECT sum(f.size) FROM files f WHERE f.transfer_id = m.transfer_id AND f.status <> 'PURGED'), 0),
		       NOT EXISTS (SELECT 1 FROM transfer_targets tt WHERE tt.transfer_id = m.transfer_id AND tt.status NOT IN ('DELIVERED', 'READ')),
		       COALESCE(m.pinned, false), mt.issued_at,
		       CASE WHEN COALESCE((SELECT sum(f.size) FROM files f WHERE f.transfer_id = m.transfer_id AND f.status <> 'PURGED'), 0) > 0
		            THEN COALESCE(policy.attachment_mode, 'PERMANENT')
		            ELSE COALESCE(policy.message_mode, 'PERMANENT') END,
		       CASE WHEN COALESCE((SELECT sum(f.size) FROM files f WHERE f.transfer_id = m.transfer_id AND f.status <> 'PURGED'), 0) > 0
		            THEN COALESCE(policy.attachment_days, 0)
		            ELSE COALESCE(policy.message_days, 0) END
		FROM messages m
		LEFT JOIN message_tombstones mt ON mt.message_id = m.id
		LEFT JOIN message_retention_policies policy
		  ON policy.conversation_key = CASE WHEN m.group_id IS NULL THEN 'inbox' ELSE 'group:' || m.group_id::text END
		WHERE mt.message_id IS NULL
		  AND COALESCE(m.pinned, false) = false
		ORDER BY m.created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []v3.RetentionCandidate
	for rows.Next() {
		var item v3.RetentionCandidate
		if err := rows.Scan(
			&item.Message.ID, &item.Message.TransferID, &item.Message.GroupID,
			&item.Message.SenderUserID, &item.Message.SenderDeviceID, &item.Message.ContentType,
			&item.Message.CreatedAt, &item.Message.ExpiresAt, &item.Message.AttachmentBytes,
			&item.Message.EveryTargetFetched, &item.Message.Pinned, &item.Message.TombstonedAt,
			&item.Policy.Mode, &item.Policy.Days,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) RecordRetentionRun(ctx context.Context, report v3.CleanupReport, started, completed time.Time, errorCode string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO retention_cleanup_runs (
			scanned, bodies_deleted, tombstones_created, started_at, completed_at, error_code
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
	`, report.Scanned, report.BodiesDeleted, report.TombstonesCreated, started, completed, errorCode)
	return err
}

func (store *Store) seedRecoveryQueue(ctx context.Context, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO transfer_recovery_queue (
			transfer_id, target_device_id, classification, retry_count,
			next_attempt_at, stable_error_code, recovery_reason, updated_at
		)
		SELECT tt.transfer_id, tt.target_device_id,
		       CASE
			WHEN tt.error_code IN ('MISSING_CHUNK', 'LOST_ACK') THEN 'RECONCILE_STATE'
			WHEN tt.error_code = 'RECEIVER_OFFLINE' THEN 'WAIT_FOR_RECEIVER'
			WHEN tt.status = 'FAILED' THEN 'RETRY_WITH_BACKOFF'
			ELSE 'RETRY_IMMEDIATELY'
		       END,
		       0, $1, COALESCE(tt.error_code, 'NODE_RESTARTED'),
		       CASE WHEN tt.status = 'FAILED' THEN 'FAILED_TARGET' ELSE 'STALE_NON_TERMINAL_TARGET' END,
		       $1
		FROM transfer_targets tt
		WHERE tt.status IN ('FAILED', 'PAUSED', 'QUEUED', 'WAITING_FOR_TARGET', 'WAITING_FOR_NODE', 'WAITING_FOR_LAN')
		   OR (tt.status IN ('UPLOADING_TO_NODE', 'DOWNLOADING_FROM_NODE', 'TRANSFERRING_LAN', 'VERIFYING')
		       AND COALESCE(tt.started_at, $1) < $1 - interval '5 minutes')
		ON CONFLICT (transfer_id, target_device_id) DO NOTHING
	`, now)
	return err
}

func (store *Store) ListRecoverable(ctx context.Context, now time.Time, limit int) ([]recovery.WorkItem, error) {
	if err := store.seedRecoveryQueue(ctx, now); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT transfer_id::text, target_device_id::text, retry_count,
		       next_attempt_at, COALESCE(stable_error_code, ''), updated_at
		FROM transfer_recovery_queue
		WHERE dead_lettered_at IS NULL
		  AND classification <> 'COMPLETED'
		  AND (next_attempt_at IS NULL OR next_attempt_at <= $1)
		  AND (lease_expires_at IS NULL OR lease_expires_at <= $1)
		ORDER BY COALESCE(next_attempt_at, updated_at), transfer_id, target_device_id
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []recovery.WorkItem
	for rows.Next() {
		var item recovery.WorkItem
		if err := rows.Scan(&item.TransferID, &item.TargetID, &item.RetryCount, &item.NextAttempt, &item.ErrorCode, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) TryLease(ctx context.Context, item recovery.WorkItem, leaseUntil time.Time) (bool, error) {
	command, err := store.pool.Exec(ctx, `
		UPDATE transfer_recovery_queue
		SET lease_expires_at = $3, lease_owner = $4, last_attempt_at = now(), updated_at = now()
		WHERE transfer_id = $1 AND target_device_id = $2
		  AND dead_lettered_at IS NULL
		  AND (lease_expires_at IS NULL OR lease_expires_at <= now())
	`, item.TransferID, item.TargetID, leaseUntil, "node:"+item.TransferID+":"+item.TargetID)
	return command.RowsAffected() == 1, err
}

func (store *Store) ReleaseLease(ctx context.Context, item recovery.WorkItem) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE transfer_recovery_queue
		SET lease_expires_at = NULL, lease_owner = NULL, updated_at = now()
		WHERE transfer_id = $1 AND target_device_id = $2
	`, item.TransferID, item.TargetID)
	return err
}

func (store *Store) MarkCompleted(ctx context.Context, item recovery.WorkItem, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE transfer_recovery_queue
		SET classification = 'COMPLETED', next_attempt_at = NULL,
		    stable_error_code = NULL, recovery_reason = 'RECOVERED', updated_at = $3
		WHERE transfer_id = $1 AND target_device_id = $2
	`, item.TransferID, item.TargetID, now)
	return err
}

func (store *Store) MarkWaiting(ctx context.Context, item recovery.WorkItem, classification recovery.Classification, next time.Time, errorCode string) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE transfer_recovery_queue
		SET classification = $3, retry_count = retry_count + 1,
		    next_attempt_at = $4, stable_error_code = NULLIF($5, ''), updated_at = now()
		WHERE transfer_id = $1 AND target_device_id = $2
	`, item.TransferID, item.TargetID, classification, next, errorCode)
	return err
}

func (store *Store) MarkDeadLetter(ctx context.Context, item recovery.WorkItem, errorCode string, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE transfer_recovery_queue
		SET classification = 'TERMINAL_FAILURE', stable_error_code = NULLIF($3, ''),
		    dead_lettered_at = $4, next_attempt_at = NULL, updated_at = $4
		WHERE transfer_id = $1 AND target_device_id = $2
	`, item.TransferID, item.TargetID, errorCode, now)
	return err
}

func (store *Store) RecordRepair(ctx context.Context, item recovery.WorkItem, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE transfer_recovery_queue
		SET classification = 'COMPLETED', recovery_reason = 'STATE_REPAIRED',
		    stable_error_code = NULL, next_attempt_at = NULL, updated_at = $3
		WHERE transfer_id = $1 AND target_device_id = $2
	`, item.TransferID, item.TargetID, now)
	return err
}

func scanRecoveryItems(rows pgx.Rows) ([]recovery.WorkItem, error) {
	defer rows.Close()
	var result []recovery.WorkItem
	for rows.Next() {
		var item recovery.WorkItem
		if err := rows.Scan(&item.TransferID, &item.TargetID, &item.RetryCount, &item.NextAttempt, &item.ErrorCode, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) FailedRecovery(ctx context.Context, limit int) ([]recovery.WorkItem, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT transfer_id::text, target_device_id::text, retry_count,
		       next_attempt_at, COALESCE(stable_error_code, ''), updated_at
		FROM transfer_recovery_queue
		WHERE dead_lettered_at IS NOT NULL
		ORDER BY dead_lettered_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	return scanRecoveryItems(rows)
}

func (store *Store) InspectRecovery(ctx context.Context, transferID string) ([]recovery.WorkItem, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT transfer_id::text, target_device_id::text, retry_count,
		       next_attempt_at, COALESCE(stable_error_code, ''), updated_at
		FROM transfer_recovery_queue
		WHERE transfer_id = $1
		ORDER BY target_device_id
	`, transferID)
	if err != nil {
		return nil, err
	}
	return scanRecoveryItems(rows)
}

func (store *Store) RetryRecovery(ctx context.Context, transferID string, now time.Time) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE transfer_recovery_queue
		SET classification = 'RETRY_IMMEDIATELY', retry_count = 0,
		    next_attempt_at = $2, lease_expires_at = NULL, lease_owner = NULL,
		    dead_lettered_at = NULL, updated_at = $2
		WHERE transfer_id = $1
	`, transferID, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func safeStoredPath(root, value string) bool {
	root = filepath.Clean(root)
	value = filepath.Clean(value)
	relative, err := filepath.Rel(root, value)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (store *Store) ReconcileRecovery(ctx context.Context, item recovery.WorkItem, storageRoot string, now time.Time) (recovery.Outcome, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status, errorCode string
	var online bool
	err = tx.QueryRow(ctx, `
		SELECT tt.status, COALESCE(tt.error_code, ''),
		       COALESCE(connection.last_seen_at > $3 - interval '90 seconds', false)
		FROM transfer_targets tt
		LEFT JOIN device_connections connection ON connection.device_id = tt.target_device_id
		WHERE tt.transfer_id = $1 AND tt.target_device_id = $2
		FOR UPDATE OF tt
	`, item.TransferID, item.TargetID, now).Scan(&status, &errorCode, &online)
	if errors.Is(err, pgx.ErrNoRows) {
		return recovery.Outcome{ErrorCode: "TARGET_NOT_FOUND"}, nil
	}
	if err != nil {
		return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
	}
	if status == "DELIVERED" || status == "READ" || status == "CANCELLED" || status == "EXPIRED" {
		return recovery.Outcome{Completed: true}, tx.Commit(ctx)
	}
	if !online && errorCode == "RECEIVER_OFFLINE" {
		return recovery.Outcome{ReceiverOffline: true, ErrorCode: "RECEIVER_OFFLINE"}, tx.Commit(ctx)
	}
	rows, err := tx.Query(ctx, `
		SELECT chunk.storage_path
		FROM file_chunks chunk
		JOIN files f ON f.id = chunk.file_id
		WHERE f.transfer_id = $1 AND chunk.status = 'COMPLETE'
	`, item.TransferID)
	if err != nil {
		return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
	}
	var missing bool
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
		}
		if !safeStoredPath(storageRoot, path) {
			rows.Close()
			return recovery.Outcome{ErrorCode: "PERMISSION_DENIED"}, nil
		}
		if _, err := os.Stat(path); err != nil {
			missing = true
			break
		}
	}
	rows.Close()
	if missing {
		if _, err := tx.Exec(ctx, `
			UPDATE transfer_targets SET status = 'FAILED', error_code = 'MISSING_CHUNK'
			WHERE transfer_id = $1 AND target_device_id = $2
		`, item.TransferID, item.TargetID); err != nil {
			return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
		}
		return recovery.Outcome{Inconsistent: true, Repaired: false, ErrorCode: "MISSING_CHUNK"}, nil
	}
	var fileCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM files WHERE transfer_id = $1`, item.TransferID).Scan(&fileCount); err != nil {
		return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
	}
	nextStatus := "QUEUED"
	if fileCount > 0 {
		nextStatus = "AVAILABLE_ON_NODE"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transfer_targets
		SET status = $3, error_code = NULL, selected_route = 'NODE',
		    active_route_id = 'NODE', route_changed_at = $4
		WHERE transfer_id = $1 AND target_device_id = $2
	`, item.TransferID, item.TargetID, nextStatus, now); err != nil {
		return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transfer_timeline_events (
			transfer_id, target_device_id, event_code, route, status, occurred_at
		) VALUES ($1, $2, 'RECOVERY_RESUMED', 'NODE', $3, $4)
	`, item.TransferID, item.TargetID, nextStatus, now); err != nil {
		return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recovery.Outcome{ErrorCode: "DATABASE_UNAVAILABLE"}, err
	}
	return recovery.Outcome{Completed: true, Repaired: errorCode == "LOST_ACK"}, nil
}

func (store *Store) RecordRecoveryRun(ctx context.Context, report recovery.Report, trigger string, started, completed time.Time, errorCode string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO transfer_recovery_runs (
			trigger_type, scanned, leased, completed, waiting, repaired,
			dead_letters, started_at, completed_at, error_code
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''))
	`, trigger, report.Scanned, report.Leased, report.Completed, report.Waiting,
		report.Repaired, report.DeadLetters, started, completed, errorCode)
	return err
}
