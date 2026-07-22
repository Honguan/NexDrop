#!/usr/bin/env bash
set -euo pipefail

if [[ "${NEXDROP_INTEGRATION_DISPOSABLE:-}" != "true" ]]; then
  echo 'NEXDROP_INTEGRATION_DISPOSABLE=true is required because this test mutates its database' >&2
  exit 2
fi

base_url="${NEXDROP_INTEGRATION_BASE_URL:-http://127.0.0.1:8080}"
restart_barrier="${NEXDROP_INTEGRATION_RESTART_BARRIER:-}"
admin_username="${NEXDROP_BOOTSTRAP_ADMIN_USERNAME:?admin username is required}"
admin_password="${NEXDROP_BOOTSTRAP_ADMIN_PASSWORD:?admin password is required}"
node_key="${NEXDROP_NODE_KEY:?node key is required}"
accept='application/vnd.nexdrop.v1+json'
tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT

curl() {
  command curl --connect-timeout 5 --max-time 30 "$@"
}

uuid() { tr '[:upper:]' '[:lower:]' </proc/sys/kernel/random/uuid; }
token() { jq -er '.accessToken'; }
api() {
  local access="$1"
  shift
  curl --fail-with-body --silent --show-error -H "Accept: $accept" \
    -H "Authorization: Bearer $access" -H "X-NexDrop-Node-Key: $node_key" "$@"
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
  -H "X-NexDrop-Node-Key: $node_key" \
  --data "$(jq -nc --arg identifier "$admin_username" --arg password "$admin_password" '{identifier:$identifier,password:$password}')" \
  "$base_url/api/auth/login")"
refresh="$(jq -er '.refreshToken' <<<"$login")"
admin_tokens="$(curl --fail-with-body --silent --show-error -H 'Content-Type: application/json' -H "Accept: $accept" \
  -H "X-NexDrop-Node-Key: $node_key" \
  --data "$(jq -nc --arg refreshToken "$refresh" '{refreshToken:$refreshToken}')" "$base_url/api/auth/refresh")"
admin_token="$(token <<<"$admin_tokens")"

sender="$(api "$admin_token" -H 'Content-Type: application/json' \
  --data "$(jq -nc --arg key "$(openssl rand 32 | base64 -w0)" \
  '{displayName:"Integration sender",type:"WINDOWS",publicKey:$key,keyAlgorithm:"X25519"}')" "$base_url/api/devices")"
sender_id="$(jq -er '.id' <<<"$sender")"
jq -e '.trustStatus == "TRUSTED"' <<<"$sender" >/dev/null

target_login="$(curl --fail-with-body --silent --show-error -H 'Content-Type: application/json' -H "Accept: $accept" \
  -H "X-NexDrop-Node-Key: $node_key" \
  --data "$(jq -nc --arg identifier "$admin_username" --arg password "$admin_password" '{identifier:$identifier,password:$password}')" \
  "$base_url/api/auth/login")"
target_token="$(token <<<"$target_login")"
target="$(api "$target_token" -H 'Content-Type: application/json' \
  --data "$(jq -nc --arg key "$(openssl rand 32 | base64 -w0)" \
  '{displayName:"Integration target",type:"ANDROID",publicKey:$key,keyAlgorithm:"X25519"}')" "$base_url/api/devices")"
target_id="$(jq -er '.id' <<<"$target")"
jq -e '.trustStatus == "TRUSTED"' <<<"$target" >/dev/null

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
if [[ -n "$restart_barrier" ]]; then
  printf '%s\n' "$file_id" >"$restart_barrier.file-id"
fi
dd if="$tmp_dir/source.bin" of="$tmp_dir/chunk-0.bin" bs=16 count=1 status=none
dd if="$tmp_dir/source.bin" of="$tmp_dir/chunk-1.bin" bs=16 skip=1 count=1 status=none
chunk_key="$(uuid)"
api "$admin_token" -H "Idempotency-Key: $chunk_key" -H "X-Chunk-SHA256: $(sha256sum "$tmp_dir/chunk-0.bin" | cut -d' ' -f1)" \
  --data-binary "@$tmp_dir/chunk-0.bin" "$base_url/api/files/$file_id/chunks/0" | jq -e '.index == 0' >/dev/null
if [[ -n "$restart_barrier" ]]; then
  : >"$restart_barrier.ready"
  resumed=false
  for _ in $(seq 1 60); do
    if [[ -f "$restart_barrier.continue" ]]; then resumed=true; break; fi
    sleep 1
  done
  [[ "$resumed" == "true" ]]
fi
api "$admin_token" -H "Idempotency-Key: $chunk_key" -H "X-Chunk-SHA256: $(sha256sum "$tmp_dir/chunk-0.bin" | cut -d' ' -f1)" \
  --data-binary "@$tmp_dir/chunk-0.bin" "$base_url/api/files/$file_id/chunks/0" | jq -e '.index == 0' >/dev/null
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

api "$admin_token" -X POST "$base_url/api/devices/$target_id/revoke" | jq -e '.trustStatus == "REVOKED"' >/dev/null
expect_status 401 -H "Accept: $accept" -H "Authorization: Bearer $target_token" "$base_url/api/account"
printf 'integration smoke test passed: %s %s\n' "$text_transfer_id" "$file_transfer_id"
