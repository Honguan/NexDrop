# Release process

[繁體中文](release-process.zh-TW.md)

## One-click release

Open `release-package` in GitHub Actions, select `patch`, `minor`, or `major`, enter English and Traditional Chinese release summaries, and choose **Run workflow**. The workflow automatically:

1. Synchronizes `VERSION`, every package version, Windows resource versions, CHANGELOG, and release notes.
2. Creates or reuses the single `release/v<version>` pull request.
3. Runs the required integration, server, web, extension, flutter, docker, security, and docs checks.
4. Rebases an outdated release branch when necessary and squash-merges only after all required checks succeed.
5. Creates the immutable `v<version>` tag from the latest `master`, starts the `release` workflow, and waits for it to finish.

If a required check fails, the workflow preserves the release pull request and its evidence without creating a tag. Fix the same pull request and run `release-package` again to resume; it does not create a duplicate pull request. If the release branch falls behind `master` again while checks run, the workflow synchronizes and revalidates it up to three times.

If the release pull request is already merged but its tag or release is incomplete, rerunning inspects the tag, successful release workflow, and published release. It resumes from the same `master` commit without incrementing the version again. If that commit already has a running release workflow, the package workflow waits for it instead of starting another run.

## Prepare a version locally

To inspect or customize a version before publishing, run:

```powershell
./scripts/prepare-release.ps1 -Bump patch -Summary 'Release summary'
./scripts/check-docs.ps1
```

After the version pull request merges, the existing `publish-on-version` workflow still creates the tag and starts the formal release when `VERSION` changes on `master`. This remains a compatible alternative to the one-click workflow.

## Dependency update pull requests

Every Monday at 03:00 Asia/Taipei, Dependabot groups routine Go, Web, Extension, Flutter, and GitHub Actions upgrades into one `nexdrop-dependencies` pull request. The pull request rebases automatically and merges through GitHub Auto-merge only after every required check succeeds. No workflow merges directly from an arbitrary successful `check_suite` event.

GitHub currently groups routine cross-ecosystem upgrades, while urgent security fixes can still appear as independent Dependabot pull requests. Do not close those fixes merely to keep a single pull request; the next routine cycle remains grouped.

## Release artifacts

The Release workflow builds Node archives for amd64 and arm64, Windows EXE and ZIP packages, an Android APK, Chrome and Edge ZIP packages, and a multi-architecture GHCR image. It verifies SHA-256, SBOM, Artifact Attestation, container images, and platform artifacts before creating the release.

Never rewrite a release tag. Publish a new PATCH tag for a corrected release. Production deployments should pin a complete container version; `latest` exists only for user convenience.

## Signing policy for a personal project

Android and Windows code signing are optional and do not block a tag or release:

- When all six signing secrets are configured, the workflow creates and verifies formally signed artifacts.
- Without Android signing secrets, it creates an installable APK for Android 6.0+ with a temporary v1/v2 signature. Without a Windows certificate, it still creates unsigned EXE and ZIP artifacts.
- A temporary Android certificate can differ between releases, so upgrading may require uninstalling the older build. In-place upgrades require a securely retained keystore and all four Android secrets. Unsigned Windows artifacts can display a SmartScreen warning.
- GHCR images still use GitHub OIDC, Cosign, Artifact Attestation, SBOM, and SHA-256 verification without a long-lived signing private key.

When formal platform signing is needed, create a `release` Environment under Repository Settings → Environments and optionally add:

| Secret | Format and purpose |
| --- | --- |
| `ANDROID_KEYSTORE_BASE64` | Single-line Base64 of an Android JKS or PKCS12 keystore |
| `ANDROID_STORE_PASSWORD` | Keystore password |
| `ANDROID_KEY_ALIAS` | Android signing-key alias |
| `ANDROID_KEY_PASSWORD` | Android signing-key password |
| `WINDOWS_CERTIFICATE_BASE64` | Single-line Base64 of a Windows PFX containing the private key |
| `WINDOWS_CERTIFICATE_PASSWORD` | PFX password |

A personal project does not require an external reviewer. The maintainer decides whether to enable Environment approval.
