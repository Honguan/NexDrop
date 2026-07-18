#!/usr/bin/env bash
set -euo pipefail

if (( $# != 5 )); then
  echo "usage: sign-android-release.sh <apk> <keystore> <alias> <store-password-file> <key-password-file>" >&2
  exit 2
fi

apk=$1
keystore=$2
key_alias=$3
store_password_file=$4
key_password_file=$5
apksigner=$(find "$ANDROID_HOME/build-tools" -type f -name apksigner | sort -V | tail -n 1)

test -f "$apk"
test -f "$keystore"
test -f "$store_password_file"
test -f "$key_password_file"
test -x "$apksigner"

signed_apk="${apk%.apk}.signed.apk"
report=$(mktemp)
trap 'rm -f -- "$signed_apk" "$report"' EXIT

"$apksigner" sign \
  --ks "$keystore" \
  --ks-key-alias "$key_alias" \
  --ks-pass "file:$store_password_file" \
  --key-pass "file:$key_password_file" \
  --v1-signing-enabled true \
  --v2-signing-enabled true \
  --v3-signing-enabled false \
  --v4-signing-enabled false \
  --out "$signed_apk" \
  "$apk"
mv -f -- "$signed_apk" "$apk"

"$apksigner" verify --verbose --print-certs "$apk" | tee "$report"
! grep -qi 'Android Debug' "$report"
grep -q 'Verified using v1 scheme (JAR signing): true' "$report"
grep -q 'Verified using v2 scheme (APK Signature Scheme v2): true' "$report"
