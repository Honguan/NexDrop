# NexDrop 1.0.0 release-readiness evidence

[繁體中文](release-readiness-v1.0.0.zh-TW.md)

This document records the validation state for `1.0.0`. A stable release can be created after all required CI succeeds and every formal artifact is traceable through its commit, tag, checksum, and attestation.

## Passed

| Item | Evidence |
| --- | --- |
| Delivery changes merged | Every required check for PR #15 succeeded; it merged as commit `47c56fc3262cbe658d191fe6a7f6a9b81b977e20` |
| Product and documentation versions | The PR #16 docs job compared `VERSION` with Web, Extension, Flutter, CHANGELOG, and release notes |
| Go Node | `server-ci` ran formatting, vet, unit tests, race tests, and build |
| Web | `web-ci` ran locked installation, lint, typecheck, unit tests, and production build |
| Extension | `extension-ci` verified separate Chrome and Edge Manifest V3 packages, permissions, secret scans, and ZIP archives |
| Flutter baseline | Ubuntu passed analyze, test, and Android Debug build; Windows passed analyze, test, and Windows Release build |
| PostgreSQL and end-to-end data flow | `integration-test` verified migrations, login, devices, transfers, resume, WebSocket, authorization, cleanup, restart, and recovery |
| Container and security baseline | Container builds, health checks, dependency scans, secret scans, SBOM, and license checks passed |
| Documentation | `docs` verified Markdown, relative links, versions, and package manifests |

## Release conditions

| Item | Completion condition |
| --- | --- |
| Git tag | Create immutable tag `v1.0.0` on `master` |
| Release workflow | Every build job succeeds after the tag is pushed |
| Release integrity | Generate and recompute SHA-256, SPDX SBOM, and Artifact Attestation |
| Container | Push `1.0.0`, `1.0`, and `latest`, and record the image digest |
| Draft release | Verify artifact names, checksums, commit, and tag before publishing |

## Signing notes

An Android keystore and Windows PFX are not release blockers:

- With all six secrets configured, create and verify the formally signed artifacts.
- Without those secrets, Android creates an installable APK with a temporary v1/v2 signature and Windows creates unsigned EXE and ZIP packages. Release notes must warn that a later Android update can require uninstalling the older build, or that Windows can display SmartScreen.
- GHCR images still use GitHub OIDC, Cosign, Artifact Attestation, SBOM, and SHA-256 verification.

Therefore, a personal maintainer can create tag `v1.0.0` and complete the release without an external reviewer. Formal platform signing can be added later and published in a new PATCH release.
