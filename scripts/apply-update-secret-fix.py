from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if text.count(old) != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {text.count(old)}")
    return text.replace(old, new, 1)


deploy_path = Path("deploy/nexdrop")
deploy = deploy_path.read_text()

deploy = replace_once(
    deploy,
    """random_hex() {
    od -An -N\"$1\" -tx1 /dev/urandom | tr -d ' \\n'
}
""",
    """random_hex() {
    od -An -N\"$1\" -tx1 /dev/urandom | tr -d ' \\n'
}

random_totp_secret() {
    head -c 20 /dev/urandom | base32 | tr -d '=\\n'
}
""",
    "add random_totp_secret",
)

deploy = replace_once(
    deploy,
    """    set_env_value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET \"$(od -An -N20 -tx1 /dev/urandom | tr -d ' \\n' | xxd -r -p | base32 | tr -d '=\\n')\"
""",
    """    set_env_value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET \"$(random_totp_secret)\"
""",
    "use random_totp_secret",
)

deploy = replace_once(
    deploy,
    """ensure_env() {
    if [ ! -f .env ]; then
        create_default_env
        CREATED_ENV=true
    fi
    if ! grep -q '^NEXDROP_CURSOR_SECRET=' .env; then
        set_env_value NEXDROP_CURSOR_SECRET \"$(random_hex 32)\"
        GENERATED_DEFAULTS=true
        echo \"已補上缺少的 NEXDROP_CURSOR_SECRET；後續更新會保留此值。\"
    fi
}
""",
    """ensure_env() {
    if [ ! -f .env ]; then
        create_default_env
        CREATED_ENV=true
    fi
    case \"$(env_value NEXDROP_CURSOR_SECRET)\" in
        ''|change-me|replace-with-openssl-rand-hex-32)
            set_env_value NEXDROP_CURSOR_SECRET \"$(random_hex 32)\"
            GENERATED_DEFAULTS=true
            echo \"已補上缺少的 NEXDROP_CURSOR_SECRET；後續更新會保留此值。\"
            ;;
    esac
    case \"$(env_value NEXDROP_NODE_KEY)\" in
        ''|change-me|replace-with-random-node-key)
            set_env_value NEXDROP_NODE_KEY \"$(random_hex 32)\"
            GENERATED_DEFAULTS=true
            echo \"已自動產生缺少的節點密鑰；後續更新會保留此值。\"
            ;;
    esac
    case \"$(env_value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)\" in
        ''|change-me|replace-with-base32-secret)
            set_env_value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET \"$(random_totp_secret)\"
            GENERATED_DEFAULTS=true
            echo \"已自動產生缺少的 Web OTP 密鑰；啟動新版後會套用至尚未啟用 OTP 的管理員。\"
            ;;
    esac
}
""",
    "ensure all upgrade secrets",
)

deploy = replace_once(
    deploy,
    """    cursor_secret=$(env_value NEXDROP_CURSOR_SECRET)

    if ! valid_env_password \"$postgres_password\" 16; then
""",
    """    cursor_secret=$(env_value NEXDROP_CURSOR_SECRET)
    node_key=$(env_value NEXDROP_NODE_KEY)
    totp_secret=$(env_value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)

    if ! valid_env_password \"$postgres_password\" 16; then
""",
    "read generated secrets during validation",
)

deploy = replace_once(
    deploy,
    """    if ! valid_minimum_length \"$cursor_secret\" 32; then
        echo \"NEXDROP_CURSOR_SECRET 必須至少 32 字元；首次安裝請重新執行 ./deploy/nexdrop install\" >&2
        return 1
    fi
    validate_public_configuration
""",
    """    if ! valid_minimum_length \"$cursor_secret\" 32; then
        echo \"NEXDROP_CURSOR_SECRET 必須至少 32 字元；首次安裝請重新執行 ./deploy/nexdrop install\" >&2
        return 1
    fi
    if ! valid_minimum_length \"$node_key\" 32; then
        echo \"NEXDROP_NODE_KEY 必須至少 32 字元；請重新執行 ./deploy/nexdrop update 讓系統自動補齊。\" >&2
        return 1
    fi
    if [ \"${#totp_secret}\" -lt 26 ]; then
        echo \"NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET 必須是有效的 Base32 密鑰；請重新執行 ./deploy/nexdrop update 讓系統自動補齊。\" >&2
        return 1
    fi
    case \"$totp_secret\" in
        *[!A-Za-z2-7]*)
            echo \"NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET 只能包含 Base32 字元 A-Z 與 2-7。\" >&2
            return 1
            ;;
    esac
    validate_public_configuration
""",
    "validate generated secrets",
)

deploy = replace_once(
    deploy,
    """        change-me|change-this-password|replace-with-a-random-admin-password)
            set_env_value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD \"$(random_hex 24)\"
    set_env_value NEXDROP_NODE_KEY \"$(random_hex 32)\"
    set_env_value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET \"$(od -An -N20 -tx1 /dev/urandom | tr -d ' \\n' | xxd -r -p | base32 | tr -d '=\\n')\"
            GENERATED_DEFAULTS=true
""",
    """        change-me|change-this-password|replace-with-a-random-admin-password)
            set_env_value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD \"$(random_hex 24)\"
            GENERATED_DEFAULTS=true
""",
    "remove incorrectly nested secret generation",
)

deploy = replace_once(
    deploy,
    """run_update() {
    ensure_env
    validate_env
""",
    """run_update() {
    GENERATED_DEFAULTS=false
    ensure_env
    validate_env
    generated_upgrade_secrets=$GENERATED_DEFAULTS
""",
    "track generated upgrade secrets",
)

deploy = replace_once(
    deploy,
    """    echo \"更新完成；.env、PostgreSQL 密碼與 NEXDROP_CURSOR_SECRET 均保持不變。\"
}
""",
    """    echo \"更新完成；既有 .env、PostgreSQL 密碼與所有已存在秘密均保持不變。\"
    if [ \"$generated_upgrade_secrets\" = true ]; then
        echo \"已為舊版設定自動補齊以下 2.0 必要秘密，請立即安全保存：\"
        echo \"  節點密鑰：$(env_value NEXDROP_NODE_KEY)\"
        echo \"  Web OTP 密鑰：$(env_value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)\"
        echo \"之後可執行 ./deploy/nexdrop credentials --show-secrets 再次查看。\"
    fi
}
""",
    "show generated upgrade secrets",
)

deploy_path.write_text(deploy)

test_path = Path("scripts/deploy-flow-test.sh")
test = test_path.read_text()

test = replace_once(
    test,
    """admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)

case \"$postgres_password\" in
""",
    """admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)
node_key=$(value NEXDROP_NODE_KEY)
totp_secret=$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)

case \"$postgres_password\" in
""",
    "capture generated install secrets",
)

test = replace_once(
    test,
    """[ \"${#cursor_secret}\" -ge 32 ]
[ \"${#admin_password}\" -ge 12 ]
""",
    """[ \"${#cursor_secret}\" -ge 32 ]
[ \"${#admin_password}\" -ge 12 ]
[ \"${#node_key}\" -ge 32 ]
[ \"${#totp_secret}\" -ge 26 ]
case \"$totp_secret\" in
    *[!A-Za-z2-7]*) echo \"TOTP secret is not Base32\" >&2; exit 1 ;;
esac
""",
    "validate install secrets",
)

test = replace_once(
    test,
    """admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)

printf 'e\\nlocalhost\\nadmin\\nadmin@example.com\\nvalid-admin-password-123\\n' | ./deploy/nexdrop install
""",
    """admin_password=$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)
node_key=$(value NEXDROP_NODE_KEY)
totp_secret=$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)

printf 'e\\nlocalhost\\nadmin\\nadmin@example.com\\nvalid-admin-password-123\\n' | ./deploy/nexdrop install
""",
    "refresh generated secrets after reinstall",
)

test = replace_once(
    test,
    """./deploy/nexdrop update 1.2.3

[ \"$(value POSTGRES_PASSWORD)\" = \"$postgres_password\" ]
""",
    """sed '/^NEXDROP_NODE_KEY=/d; /^NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET=/d' .env >.env.tmp
mv .env.tmp .env
./deploy/nexdrop update 1.2.3 >\"$WORK/legacy-update.out\"

upgraded_node_key=$(value NEXDROP_NODE_KEY)
upgraded_totp_secret=$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)
[ \"${#upgraded_node_key}\" -ge 32 ]
[ \"${#upgraded_totp_secret}\" -ge 26 ]
[ \"$upgraded_node_key\" != \"$node_key\" ]
[ \"$upgraded_totp_secret\" != \"$totp_secret\" ]
grep -q '已自動產生缺少的節點密鑰' \"$WORK/legacy-update.out\"
grep -q '已自動產生缺少的 Web OTP 密鑰' \"$WORK/legacy-update.out\"
grep -q \"節點密鑰：$upgraded_node_key\" \"$WORK/legacy-update.out\"
grep -q \"Web OTP 密鑰：$upgraded_totp_secret\" \"$WORK/legacy-update.out\"
node_key=$upgraded_node_key
totp_secret=$upgraded_totp_secret

[ \"$(value POSTGRES_PASSWORD)\" = \"$postgres_password\" ]
""",
    "simulate legacy update",
)

test = replace_once(
    test,
    """[ \"$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)\" = \"$admin_password\" ]
[ \"$(value NEXDROP_IMAGE)\" = \"ghcr.io/honguan/nexdrop:1.2.3\" ]
""",
    """[ \"$(value NEXDROP_BOOTSTRAP_ADMIN_PASSWORD)\" = \"$admin_password\" ]
[ \"$(value NEXDROP_NODE_KEY)\" = \"$node_key\" ]
[ \"$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)\" = \"$totp_secret\" ]
[ \"$(value NEXDROP_IMAGE)\" = \"ghcr.io/honguan/nexdrop:1.2.3\" ]
./deploy/nexdrop update 1.2.3 >\"$WORK/repeat-update.out\"
[ \"$(value NEXDROP_NODE_KEY)\" = \"$node_key\" ]
[ \"$(value NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET)\" = \"$totp_secret\" ]
! grep -q '已自動產生缺少' \"$WORK/repeat-update.out\"
""",
    "preserve generated upgrade secrets",
)

test = replace_once(
    test,
    """grep -q \"Bootstrap 初始密碼：$admin_password\" \"$WORK/credentials.out\"
""",
    """grep -q \"Bootstrap 初始密碼：$admin_password\" \"$WORK/credentials.out\"
grep -q \"節點密鑰：$node_key\" \"$WORK/credentials.out\"
grep -q \"Web OTP 密鑰：$totp_secret\" \"$WORK/credentials.out\"
""",
    "verify credentials output",
)

test_path.write_text(test)
