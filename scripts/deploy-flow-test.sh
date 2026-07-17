#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

mkdir -p "$WORK/deploy" "$WORK/bin"
cp "$ROOT/deploy/nexdrop" "$WORK/deploy/nexdrop"
cp "$ROOT/.env.example" "$ROOT/compose.yaml" "$WORK/"
cat >"$WORK/bin/docker" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$DOCKER_LOG"
if [ "${FAIL_UPDATE:-}" = 1 ] && [ "$*" = "compose up -d --wait --wait-timeout 120" ]; then
    exit 1
fi
EOF
chmod +x "$WORK/bin/docker" "$WORK/deploy/nexdrop"

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

case "$postgres_password" in
    *[!A-Fa-f0-9]*|'') echo "POSTGRES_PASSWORD is not hexadecimal" >&2; exit 1 ;;
esac
[ "${#postgres_password}" -ge 32 ]
[ "${#cursor_secret}" -ge 32 ]
[ "${#admin_password}" -ge 12 ]

rm .env
printf 'r\na\n' | ./deploy/nexdrop install
[ "$(value POSTGRES_PASSWORD)" != "$postgres_password" ]
[ "$(value NEXDROP_CURSOR_SECRET)" != "$cursor_secret" ]
[ "$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)" != "$admin_password" ]
postgres_password=$(value POSTGRES_PASSWORD)
cursor_secret=$(value NEXDROP_CURSOR_SECRET)
admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)

printf 'e\nhttps://invalid\nadmin\nadmin@example.com\nshort\nlocalhost\nadmin\nadmin@example.com\nvalid-admin-password-123\n' | ./deploy/nexdrop install
[ "$(value NEXDROP_DOMAIN)" = localhost ]
[ "$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)" = valid-admin-password-123 ]
admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)

./deploy/nexdrop update 1.2.3

[ "$(value POSTGRES_PASSWORD)" = "$postgres_password" ]
[ "$(value NEXDROP_CURSOR_SECRET)" = "$cursor_secret" ]
[ "$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)" = "$admin_password" ]
[ "$(value NEXDROP_IMAGE)" = "ghcr.io/honguan/nexdrop:1.2.3" ]
grep -q 'compose run --rm nexdrop backup' "$DOCKER_LOG"
grep -q 'compose exec -T postgres' "$DOCKER_LOG"

printf 'a\n' | ./deploy/nexdrop install

if FAIL_UPDATE=1 ./deploy/nexdrop update 1.2.4 >"$WORK/update-failure.out" 2>&1; then
    echo "failed update was accepted" >&2
    exit 1
fi
[ "$(value NEXDROP_IMAGE)" = "ghcr.io/honguan/nexdrop:1.2.3" ]
grep -q 'compose stop nexdrop' "$DOCKER_LOG"
grep -q 'migration' "$WORK/update-failure.out"

sed 's/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=unsafe@password/' .env >.env.tmp
mv .env.tmp .env
if ./deploy/nexdrop update >"$WORK/invalid.out" 2>&1; then
    echo "unsafe PostgreSQL password was accepted" >&2
    exit 1
fi
grep -q 'POSTGRES_PASSWORD' "$WORK/invalid.out"

echo "deploy flow tests passed"
