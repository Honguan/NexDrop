# NexDrop

[繁體中文](README.zh-TW.md)

NexDrop is a self-hosted hybrid transfer platform for multiple devices. It prefers direct LAN transfer and falls back to private Node storage when peers cannot connect directly. Supported clients include Windows 10/11, Android, Web, Chrome, and Edge.

The current version is **2.0.3**. Core capabilities include node-key onboarding, end-to-end encrypted text and file transfer, resumable chunks, real-time device presence, and per-device statistics. Node administration is intentionally provided through deployment commands instead of a Web administration console.

## Architecture

A modular Go monolith provides the HTTP interface, WebSocket presence, and transfer workers. PostgreSQL stores durable state. A shared Flutter client targets Windows and Android, React powers the Web client, and the Manifest V3 extension registers as an independent device. See the [architecture guide](docs/architecture.md).

## Quick Linux Node deployment

Requirements: Docker Engine 24+, Docker Compose v2, and a domain name that resolves to the host.

```bash
git clone https://github.com/Honguan/NexDrop.git
cd NexDrop
./deploy/nexdrop install
docker compose ps
```

The installer obtains the required Docker privileges, explains every domain and password rule, and generates PostgreSQL, cursor, and administrator secrets. It displays all generated defaults so the operator can accept, edit, or regenerate them. The resulting `.env` remains readable only by the invoking user with mode `600`. Automated environments can use `./deploy/nexdrop install --non-interactive`.

Caddy exposes `80/tcp`, `443/tcp`, and `443/udp` by default. The Node uses `8080/tcp` only inside the Compose network. Build a local source image with `docker compose build --pull nexdrop`.

## Development

```bash
go test ./...
go build ./cmd/nexdrop
cd web && npm ci && npm test && npm run build
cd ../extension && npm ci && npm test && npm run build
cd ../client && flutter analyze && flutter test
```

- Node and command line: [cmd/README.md](cmd/README.md)
- Windows and Android: [client/README.md](client/README.md)
- Web: [web/README.md](web/README.md)
- Browser extension: [extension/README.md](extension/README.md)
- Deployment operations: [deploy/README.md](deploy/README.md)

## Configuration and operations

Safe defaults are documented in [.env.example](.env.example); see the [configuration guide](docs/configuration.md) for every setting. Common commands:

```bash
./deploy/nexdrop status
./deploy/nexdrop info
./deploy/nexdrop credentials
./deploy/nexdrop configure
./deploy/nexdrop configure-secrets
./deploy/nexdrop logs nexdrop
./deploy/nexdrop doctor
./deploy/nexdrop backup --output /var/lib/nexdrop/backups/manual.tar.gz
./deploy/nexdrop cleanup --limit 100
./deploy/nexdrop update
# Pin a version: ./deploy/nexdrop update 2.0.3
```

## Security and releases

Production deployments must use HTTPS, strong passwords, and fully pinned image versions. Never commit `.env`, tokens, private keys, keystores, or signing certificates. Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).

Official artifacts and SHA-256 checksums are published on [GitHub Releases](https://github.com/Honguan/NexDrop/releases). Maintainers can run `release-package` in GitHub Actions to create a version PR, execute required checks, merge it, create an immutable tag, and wait for the release artifacts. See [CHANGELOG.md](CHANGELOG.md) and the [release process](docs/release-process.md).

## Documentation

The [documentation index](docs/README.md) links deployment, interface, security, data model, protocol, and troubleshooting guides. Read [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before contributing.

NexDrop is available under the [MIT License](LICENSE).
