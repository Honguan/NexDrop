package v3

import (
	"path/filepath"
	"strings"
	"time"

	"nexdrop/internal/enrollment"
	"nexdrop/internal/messagelifecycle"
	"nexdrop/internal/recovery"
	"nexdrop/internal/relay"
)

// NewWithRelaySeed creates the production v3 service with an independent relay
// signing seed. The Node key bootstraps enrollment and tombstones; the relay
// signing seed is used only for short-lived encrypted chunk grants.
func NewWithRelaySeed(store Store, nodeID string, rootSecret, cursorSecret, relaySeed []byte, storageRoot string) (*Service, error) {
	if store == nil || strings.TrimSpace(storageRoot) == "" {
		return nil, ErrInvalid
	}
	enrollmentService, err := enrollment.New(store, nodeID, rootSecret)
	if err != nil {
		return nil, err
	}
	relaySigner, err := relay.NewGrantSigner(relaySeed)
	if err != nil {
		return nil, err
	}
	cursor, err := messagelifecycle.NewCursorCodec(cursorSecret)
	if err != nil {
		return nil, err
	}
	tombstones, err := messagelifecycle.NewTombstoneSigner(rootSecret)
	if err != nil {
		return nil, err
	}
	service := &Service{
		store: store, enrollment: enrollmentService, relaySigner: relaySigner,
		cursor: cursor, tombstones: tombstones, storageRoot: filepath.Clean(storageRoot), now: time.Now,
	}
	service.recovery = recovery.New(store, service, recovery.Config{})
	return service, nil
}

func (service *Service) RelayGrantPublicKey() string {
	return service.relaySigner.PublicKeyBase64()
}
