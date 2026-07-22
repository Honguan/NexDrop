# Deployment and operations

[繁體中文](README.zh-TW.md)

Requirements: Linux, Docker Engine 24+, and Docker Compose v2. Start a first installation with:

```bash
./deploy/nexdrop install
./deploy/nexdrop status
./deploy/nexdrop info
./deploy/nexdrop doctor
```

The installer explains every rule, creates `.env` with mode `600`, generates secure defaults, and lets the operator accept, edit, or regenerate public, administrator, and secret values. Use `./deploy/nexdrop install --non-interactive` for automation. Later, run `./deploy/nexdrop configure` to change the domain or bootstrap administrator fields.

`./deploy/nexdrop info` prints the public Node URL, configured image, bootstrap identifier, configuration path, and source version without exposing any secret. Use `credentials --show-secrets` only when the secrets are explicitly required.

Changing bootstrap fields does not change an existing account. Reset an existing administrator password without writing it to shell history:

```bash
read -rsp 'New password: ' P; echo
printf '%s\n' "$P" | ./deploy/nexdrop reset-password --identifier admin
unset P
```

Production deployments should pin a complete image version. `./deploy/nexdrop update 2.0.3` backs up the database and files before switching `NEXDROP_IMAGE`, then waits for health checks. On failure it stops the Node and restores the previous image setting; it does not automatically start an older image against an unknown migration state. Updates preserve the PostgreSQL password, administrator password, and `NEXDROP_CURSOR_SECRET`. Store backups separately and rehearse restoration.

PostgreSQL passwords must contain at least 16 characters. Supported characters are letters, digits, `.`, `_`, `~`, `!`, `@`, `%`, `+`, `,`, `:`, `/`, and `-`; Compose passes the password separately from the database URL. `openssl rand -hex 32` remains the simplest safe generator. Administrator names contain 3–64 characters, email addresses include `@`, and administrator passwords contain at least 12 characters with the same supported character set. Enter only a host name in the Caddy domain setting, without `https://`.
