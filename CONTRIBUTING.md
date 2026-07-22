# Contributing

[繁體中文](CONTRIBUTING.zh-TW.md)

1. Create a short-lived branch from `master` and change only files directly related to the issue.
2. For Go, run `gofmt`, `go vet ./...`, and `go test ./...`. For Web and Extension, run `npm ci`, lint, typecheck, test, and build. For Client, run `flutter analyze` and `flutter test`.
3. Add a new incrementally numbered migration for database changes; never rewrite a published migration.
4. Update `docs/api.md`, compatibility tests, and CHANGELOG for public interface changes.
5. Describe behavior, test evidence, migration or rollback effects, and known limitations in the pull request. Never commit secrets or generated artifacts.

Use a short imperative commit summary prefixed with `feat:`, `fix:`, or `chore:`.
