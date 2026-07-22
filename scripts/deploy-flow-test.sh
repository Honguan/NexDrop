#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

mkdir -p "$WORK/deploy" "$WORK/bin"
cp "$ROOT/deploy/nexdrop" "$WORK/deploy/nexdrop"
cp "$ROOT/.env.example" "$ROOT/compose.yaml" "$ROOT/VERSION" "$WORK/"
cat >"$WORK/bin/docker" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$DOCKER_LOG"
if [ "${FAIL_UPDATE:-}" = 1 ] && [ "$*" = "compose up -d --wait --wait-timeout 120" ]; then
    exit 1
fi
EOF
cat >"$WORK/bin/curl" <<'EOF'
#!/bin/sh
printf '%s\n' '{"tag_name":"v1.2.5"}'
EOF
chmod +x "$WORK/bin/docker" "$WORK/bin/curl" "$WORK/deploy/nexdrop"

export DOCKER_LOG="$WORK/docker.log"
PATH="$WORK/bin:$PATH"
export PATH
cd "$WORK"

./deploy/nexdrop install --non-interactive

value() {
    sed -n "s/^$1=//p" .env | tail -n 1
}

postgres_password=$(value POSTGRES_PASSWORD)
cursor_secret=$(value NEXDROP_CURSOR_SECRET)
admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)
node_key=$(value NEXDROP_NODE_KEY)
totp_secret=$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)

case "$postgres_password" in
    *[!A-Fa-f0-9]*|'') echo "POSTGRES_PASSWORD is not hexadecimal" >&2; exit 1 ;;
esac
[ "${#postgres_password}" -ge 32 ]
[ "${#cursor_secret}" -ge 32 ]
[ "${#admin_password}" -ge 12 ]
[ "${#node_key}" -ge 32 ]
[ "${#totp_secret}" -ge 26 ]
case "$totp_secret" in
    *[!A-Za-z2-7]*) echo "TOTP secret is not Base32" >&2; exit 1 ;;
esac

rm .env
printf 'r\na\n' | ./deploy/nexdrop install
[ "$(value POSTGRES_PASSWORD)" != "$postgres_password" ]
[ "$(value NEXDROP_CURSOR_SECRET)" != "$cursor_secret" ]
[ "$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)" != "$admin_password" ]
postgres_password=$(value POSTGRES_PASSWORD)
cursor_secret=$(value NEXDROP_CURSOR_SECRET)
admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)
node_key=$(value NEXDROP_NODE_KEY)
totp_secret=$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)

printf 'e\nlocalhost\nadmin\nadmin@example.com\nvalid-admin-password-123\n' | ./deploy/nexdrop install
[ "$(value NEXDROP_DOMAIN)" = localhost ]
[ "$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)" = valid-admin-password-123 ]
admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)

sed '/^NEXDROP_NODE_KEY=/d; /^NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET=/d' .env >.env.tmp
mv .env.tmp .env
./deploy/nexdrop update 1.2.3 >"$WORK/legacy-update.out"

upgraded_node_key=$(value NEXDROP_NODE_KEY)
upgraded_totp_secret=$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)
[ "${#upgraded_node_key}" -ge 32 ]
[ "${#upgraded_totp_secret}" -ge 26 ]
[ "$upgraded_node_key" != "$node_key" ]
[ "$upgraded_totp_secret" != "$totp_secret" ]
grep -q '已自動產生缺少的節點密鑰' "$WORK/legacy-update.out"
grep -q '已自動產生缺少的 Web OTP 密鑰' "$WORK/legacy-update.out"
grep -q "節點密鑰：$upgraded_node_key" "$WORK/legacy-update.out"
grep -q "Web OTP 密鑰：$upgraded_totp_secret" "$WORK/legacy-update.out"
node_key=$upgraded_node_key
totp_secret=$upgraded_totp_secret

[ "$(value POSTGRES_PASSWORD)" = "$postgres_password" ]
[ "$(value NEXDROP_CURSOR_SECRET)" = "$cursor_secret" ]
[ "$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)" = "$admin_password" ]
[ "$(value NEXDROP_NODE_KEY)" = "$node_key" ]
[ "$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)" = "$totp_secret" ]
[ "$(value NEXDROP_IMAGE)" = "ghcr.io/honguan/nexdrop:1.2.3" ]
./deploy/nexdrop update 1.2.3 >"$WORK/repeat-update.out"
[ "$(value NEXDROP_NODE_KEY)" = "$node_key" ]
[ "$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)" = "$totp_secret" ]
! grep -q '已自動產生缺少' "$WORK/repeat-update.out"
grep -q 'compose run --rm nexdrop backup' "$DOCKER_LOG"
grep -q 'compose exec -T postgres' "$DOCKER_LOG"

printf 'a\n' | ./deploy/nexdrop install
./deploy/nexdrop credentials --show-secrets >"$WORK/credentials.out"
grep -q "Bootstrap 初始密碼：$admin_password" "$WORK/credentials.out"
grep -q "節點密鑰：$node_key" "$WORK/credentials.out"
grep -q "Web OTP 密鑰：$totp_secret" "$WORK/credentials.out"
./deploy/nexdrop info >"$WORK/info.out"
grep -q '節點網址：https://localhost' "$WORK/info.out"
grep -q '設定映像：ghcr.io/honguan/nexdrop:1.2.3' "$WORK/info.out"
grep -q 'Bootstrap 管理員：admin <admin@example.com>' "$WORK/info.out"
! grep -q "$postgres_password" "$WORK/info.out"
! grep -q "$cursor_secret" "$WORK/info.out"

old_postgres_password=$(value POSTGRES_PASSWORD)
old_cursor_secret=$(value NEXDROP_CURSOR_SECRET)
printf 'n\ny\nr\ny\nr\n' | ./deploy/nexdrop configure-secrets
[ "$(value POSTGRES_PASSWORD)" != "$old_postgres_password" ]
[ "$(value NEXDROP_CURSOR_SECRET)" != "$old_cursor_secret" ]
postgres_password=$(value POSTGRES_PASSWORD)
cursor_secret=$(value NEXDROP_CURSOR_SECRET)
grep -q 'compose exec -T -e NEXDROP_NEW_PASSWORD=' "$DOCKER_LOG"

if FAIL_UPDATE=1 ./deploy/nexdrop update 1.2.4 >"$WORK/update-failure.out" 2>&1; then
    echo "failed update was accepted" >&2
    exit 1
fi
[ "$(value NEXDROP_IMAGE)" = "ghcr.io/honguan/nexdrop:1.2.3" ]
grep -q 'compose stop nexdrop' "$DOCKER_LOG"
grep -q 'migration' "$WORK/update-failure.out"

sed 's/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=P@ssword:with\/special+chars/' .env >.env.tmp
mv .env.tmp .env
./deploy/nexdrop update
[ "$(value POSTGRES_PASSWORD)" = 'P@ssword:with/special+chars' ]
[ "$(value NEXDROP_IMAGE)" = 'ghcr.io/honguan/nexdrop:1.2.5' ]

echo "deploy flow tests passed"
