#!/usr/bin/env bash
set -euo pipefail

if [[ "${NEXDROP_INTEGRATION_DISPOSABLE:-}" != "true" ]]; then
  echo 'NEXDROP_INTEGRATION_DISPOSABLE=true is required because this test mutates its database' >&2
  exit 2
fi

base_url="${NEXDROP_INTEGRATION_BASE_URL:-http://127.0.0.1:8080}"
admin_username="${NEXDROP_BOOTSTRAP_ADMIN_USERNAME:?admin username is required}"
admin_password="${NEXDROP_BOOTSTRAP_ADMIN_PASSWORD:?admin password is required}"
accept='application/vnd.nexdrop.v1+json'
tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT

uuid() { tr '[:upper:]' '[:lower:]' </proc/sys/kernel/random/uuid; }
token() { jq -er '.accessToken'; }
totp_code() {
  TOTP_SECRET="$1" node <<'NODE'
const crypto = require('node:crypto');
const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
const bits = [...process.env.TOTP_SECRET].map(character => alphabet.indexOf(character).toString(2).padStart(5, '0')).join('');
const key = Buffer.from(bits.match(/.{8}/g).map(value => Number.parseInt(value, 2)));
const counter = Buffer.alloc(8);
counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
const digest = crypto.createHmac('sha1', key).update(counter).digest();
const offset = digest[digest.length - 1] & 15;
const value = (digest.readUInt32BE(offset) & 0x7fffffff) % 1000000;
process.stdout.write(String(value).padStart(6, '0'));
NODE
}
api() {
  local access="$1"
  shift
  curl --fail-with-body --silent --show-error -H "Accept: $accept" \
    -H "Authorization: Bearer $access" "$@"
}
expect_status() {
  local expected="$1"
  shift
  local actual
  actual="$(curl --silent --show-error --output "$tmp_dir/response.json" --write-out '%{http_code}' "$@")"
  if [[ "$actual" != "$expected" ]]; then
    printf 'expected HTTP %s, got %s: ' "$expected" "$actual" >&2
    cat "$tmp_dir/response.json" >&2
    return 1
  fi
}

login="$(curl --fail-with-body --silent --show-error -H 'Content-Type: application/json' -H "Accept: $accept" \
  --data "$(jq -nc --arg identifier "$admin_username" --arg password "$admin_password" '{identifier:$identifier,password:$password}')" \
  "$base_url/api/auth/login")"
refresh="$(jq -er '.refreshToken' <<<"$login")"
admin_tokens="$(curl --fail-with-body --silent --show-error -H 'Content-Type: application/json' -H "Accept: $accept" \
  --data "$(jq -nc --arg refreshToken "$refresh" '{refreshToken:$refreshToken}')" "$base_url/api/auth/refresh")"
admin_token="$(token <<<"$admin_tokens")"
admin_user_id="$(api "$admin_token" "$base_url/api/account" | jq -er '.id')"

sender="$(api "$admin_token" -H 'Content-Type: application/json' \
  --data "$(jq -nc --arg key "$(openssl rand 32 | base64 -w0)" \
  '{displayName:"Integration sender",type:"WINDOWS",publicKey:$key,keyAlgorithm:"X25519"}')" "$base_url/api/devices")"
sender_id="$(jq -er '.id' <<<"$sender")"
api "$admin_token" -X POST "$base_url/api/devices/$sender_id/approve" | jq -e '.trustStatus == "TRUSTED"' >/dev/null

target_login="$(curl --fail-with-body --silent --show-error -H 'Content-Type: application/json' -H "Accept: $accept" \
  --data "$(jq -nc --arg identifier "$admin_username" --arg password "$admin_password" '{identifier:$identifier,password:$password}')" \
  "$base_url/api/auth/login")"
target_token="$(token <<<"$target_login")"
target="$(api "$target_token" -H 'Content-Type: application/json' \
  --data "$(jq -nc --arg key "$(openssl rand 32 | base64 -w0)" \
  '{displayName:"Integration target",type:"ANDROID",publicKey:$key,keyAlgorithm:"X25519"}')" "$base_url/api/devices")"
target_id="$(jq -er '.id' <<<"$target")"
api "$admin_token" -X POST "$base_url/api/devices/$target_id/approve" | jq -e '.trustStatus == "TRUSTED"' >/dev/null

wrapped_key="$(openssl rand 32 | base64 -w0)"
text_key="$(uuid)"
text_request="$(jq -nc --arg target "$target_id" --arg key "$wrapped_key" \
  '{targetType:"SINGLE_DEVICE",targetDeviceIds:[$target],contentType:"TEXT",content:"aW50ZWdyYXRpb24=",routeMode:"AUTOMATIC",wrappedContentKeys:{($target):$key}}')"
text_transfer="$(api "$admin_token" -H 'Content-Type: application/json' -H "Idempotency-Key: $text_key" \
  --data "$text_request" "$base_url/api/transfers")"
text_transfer_id="$(jq -er '.id' <<<"$text_transfer")"
replayed_id="$(api "$admin_token" -H 'Content-Type: application/json' -H "Idempotency-Key: $text_key" \
  --data "$text_request" "$base_url/api/transfers" | jq -er '.id')"
[[ "$replayed_id" == "$text_transfer_id" ]]
expect_status 409 -H 'Content-Type: application/json' -H "Accept: $accept" \
  -H "Authorization: Bearer $admin_token" -H "Idempotency-Key: $text_key" \
  --data "$(jq '.content="Y29uZmxpY3Q="' <<<"$text_request")" "$base_url/api/transfers"
jq -e '.error.code == "IDEMPOTENCY_CONFLICT"' "$tmp_dir/response.json" >/dev/null

printf '0123456789abcdef0123456789abcdef' >"$tmp_dir/source.bin"
file_size="$(wc -c <"$tmp_dir/source.bin" | tr -d ' ')"
file_hash_hex="$(sha256sum "$tmp_dir/source.bin" | cut -d' ' -f1)"
file_hash_base64="$(openssl dgst -sha256 -binary "$tmp_dir/source.bin" | base64 -w0)"
file_request="$(jq -nc --arg target "$target_id" --arg key "$wrapped_key" --arg hash "$file_hash_base64" --argjson size "$file_size" \
  '{targetType:"SINGLE_DEVICE",targetDeviceIds:[$target],contentType:"FILE",files:[{name:"integration.bin",mimeType:"application/octet-stream",size:$size,sha256:$hash,chunkSize:16,chunkCount:2}],routeMode:"NODE_ONLY",wrappedContentKeys:{($target):$key}}')"
file_transfer="$(api "$admin_token" -H 'Content-Type: application/json' -H "Idempotency-Key: $(uuid)" \
  --data "$file_request" "$base_url/api/transfers")"
file_transfer_id="$(jq -er '.id' <<<"$file_transfer")"
file_id="$(jq -er '.files[0].id' <<<"$file_transfer")"
dd if="$tmp_dir/source.bin" of="$tmp_dir/chunk-0.bin" bs=16 count=1 status=none
dd if="$tmp_dir/source.bin" of="$tmp_dir/chunk-1.bin" bs=16 skip=1 count=1 status=none
chunk_key="$(uuid)"
for _ in 1 2; do
  api "$admin_token" -H "Idempotency-Key: $chunk_key" -H "X-Chunk-SHA256: $(sha256sum "$tmp_dir/chunk-0.bin" | cut -d' ' -f1)" \
    --data-binary "@$tmp_dir/chunk-0.bin" "$base_url/api/files/$file_id/chunks/0" | jq -e '.index == 0' >/dev/null
done
api "$admin_token" -H "Idempotency-Key: $(uuid)" -H "X-Chunk-SHA256: $(sha256sum "$tmp_dir/chunk-1.bin" | cut -d' ' -f1)" \
  --data-binary "@$tmp_dir/chunk-1.bin" "$base_url/api/files/$file_id/chunks/1" | jq -e '.index == 1' >/dev/null
api "$admin_token" -X POST -H "Idempotency-Key: $(uuid)" \
  "$base_url/api/files/$file_id/complete" | jq -e '.status == "AVAILABLE_ON_NODE"' >/dev/null
api "$target_token" "$base_url/api/files/$file_id/chunks/0" >"$tmp_dir/download.bin"
api "$target_token" "$base_url/api/files/$file_id/chunks/1" >>"$tmp_dir/download.bin"
cmp "$tmp_dir/source.bin" "$tmp_dir/download.bin"
[[ "$(sha256sum "$tmp_dir/download.bin" | cut -d' ' -f1)" == "$file_hash_hex" ]]

page="$(api "$admin_token" "$base_url/api/transfers?limit=1")"
cursor="$(jq -er '.nextCursor' <<<"$page")"
api "$admin_token" "$base_url/api/transfers?limit=1&cursor=$cursor" | jq -e '.items | length >= 1' >/dev/null
expect_status 400 -H "Accept: $accept" -H "Authorization: Bearer $admin_token" \
  "$base_url/api/transfers?limit=1&cursor=${cursor}x"

WS_TOKEN="$admin_token" WS_BASE_URL="$base_url" node <<'NODE'
const url = new URL(process.env.WS_BASE_URL.replace(/^http/, 'ws') + '/ws');
url.searchParams.set('access_token', process.env.WS_TOKEN);
url.searchParams.set('protocolVersion', '1.1');
url.searchParams.set('clientVersion', 'integration-v1.1');
const socket = new WebSocket(url, 'nexdrop.v1');
const timeout = setTimeout(() => { console.error('WebSocket heartbeat timed out'); process.exit(1); }, 10000);
socket.addEventListener('error', event => { console.error(event.message || 'WebSocket failed'); process.exit(1); });
socket.addEventListener('message', event => {
  const message = JSON.parse(event.data);
  if (message.type === 'connected') socket.send(JSON.stringify({type: 'heartbeat'}));
  if (message.type === 'heartbeat_ack') { clearTimeout(timeout); socket.close(1000, 'done'); }
});
socket.addEventListener('close', event => {
  if (event.code === 1000) process.exit(0);
  console.error(`WebSocket closed with ${event.code}`);
  process.exit(1);
});
NODE

totp_setup="$(api "$admin_token" -H 'Content-Type: application/json' \
  --data "$(jq -nc --arg password "$admin_password" '{password:$password}')" "$base_url/api/auth/totp/setup")"
totp_secret="$(jq -er '.secret' <<<"$totp_setup")"
api "$admin_token" -H 'Content-Type: application/json' \
  --data "$(jq -nc --arg password "$admin_password" --arg secret "$totp_secret" --arg code "$(totp_code "$totp_secret")" \
    '{password:$password,secret:$secret,code:$code}')" "$base_url/api/auth/totp/enable" >/dev/null
api "$admin_token" -X POST -H 'Content-Type: application/json' \
  --data "$(jq -nc --arg password "$admin_password" --arg totp "$(totp_code "$totp_secret")" '{password:$password,totp:$totp}')" \
  "$base_url/api/auth/admin-verify" >/dev/null
isolated_username="integration-$(uuid)"
invitation="$(api "$admin_token" -H 'Content-Type: application/json' \
  --data "$(jq -nc --arg username "$isolated_username" --arg email "$isolated_username@example.invalid" \
    '{username:$username,email:$email,admin:false}')" \
  "$base_url/api/admin/invitations")"
isolated_password='Integration-isolated-1234'
curl --fail-with-body --silent --show-error -H 'Content-Type: application/json' -H "Accept: $accept" \
  --data "$(jq -nc --arg token "$(jq -er '.token' <<<"$invitation")" --arg password "$isolated_password" '{token:$token,password:$password}')" \
  "$base_url/api/auth/invitations/accept" | jq -e --arg username "$isolated_username" '.username == $username' >/dev/null
isolated_login="$(curl --fail-with-body --silent --show-error -H 'Content-Type: application/json' -H "Accept: $accept" \
  --data "$(jq -nc --arg identifier "$isolated_username" --arg password "$isolated_password" \
    '{identifier:$identifier,password:$password}')" "$base_url/api/auth/login")"
expect_status 404 -H "Accept: $accept" -H "Authorization: Bearer $(token <<<"$isolated_login")" \
  "$base_url/api/transfers/$text_transfer_id"

api "$admin_token" -X PUT -H 'Content-Type: application/json' --data '{"byteLimit":1,"dailyTransferLimit":1}' \
  "$base_url/api/admin/quotas/USER/$admin_user_id" | jq -e '.byteLimit == 1' >/dev/null
audit_page="$(api "$admin_token" "$base_url/api/admin/audit-logs?limit=1")"
audit_cursor="$(jq -er '.nextCursor' <<<"$audit_page")"
api "$admin_token" "$base_url/api/admin/audit-logs?limit=1&cursor=$audit_cursor" | jq -e '.items | length >= 1' >/dev/null
expect_status 400 -H "Accept: $accept" -H "Authorization: Bearer $admin_token" \
  "$base_url/api/admin/audit-logs?cursor=${audit_cursor}x"
quota_request="$(jq '.files[0].name="over-quota.bin"' <<<"$file_request")"
expect_status 507 -H 'Content-Type: application/json' -H "Accept: $accept" \
  -H "Authorization: Bearer $admin_token" -H "Idempotency-Key: $(uuid)" \
  --data "$quota_request" "$base_url/api/transfers"
jq -e '.error.code == "QUOTA_EXCEEDED"' "$tmp_dir/response.json" >/dev/null

api "$admin_token" -X POST "$base_url/api/devices/$target_id/revoke" | jq -e '.trustStatus == "REVOKED"' >/dev/null
expect_status 401 -H "Accept: $accept" -H "Authorization: Bearer $target_token" "$base_url/api/account"
printf 'integration smoke test passed: %s %s\n' "$text_transfer_id" "$file_transfer_id"
