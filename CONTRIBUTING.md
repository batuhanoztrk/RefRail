# Contributing to RefRail

RefRail is a Go CLI. Keep policy validation separate from Git processes, installation lifecycle, and localized presentation.

## Development

1. Install Go 1.27+ and Git.
2. Run `go run ./tools/release --version v0.1.0 --targets <your-goos>/<your-goarch>` to build a native bundle with checksum-verified Lefthook 2.1.17.
3. Run `go test ./... -count=1`, `go vet ./...`, and `gofmt` on changed Go files.

Real-hook tests discover a native bundle's Lefthook, accept `REFRAIL_TEST_LEFTHOOK`, or use Lefthook 2.1.17 from PATH. They use isolated Git repositories and compiled executables; never install hooks in unrelated repositories during tests.

## Compatibility

- Project settings may tighten shared rules, never relax them.
- Preserve installation baselines through update and rollback.
- Never overwrite hooks silently or change global Git settings.
- Keep runtime validation offline.
- Report cross-builds and actual platform execution separately.
- Keep credentials and access-bearing URLs out of logs and fixtures.

## Languages

English templates are keys in `internal/i18n/catalog.go`; supply a Turkish translation for new user-facing messages. Use structured `i18n.Errorf` errors and render at the CLI boundary so nested causes and exit codes remain intact. OS and third-party diagnostics may retain their own language.

Test `REFRAIL_LANG=en` and `REFRAIL_LANG=tr`. Keep JSON keys and machine-readable output language-independent.

## Changes

Include OS/architecture, Git/RefRail/Lefthook versions, a minimal reproduction, and expected versus actual behavior. Remove secrets before sharing output. Add regression coverage and use Conventional Commit messages. Contributions are under this repository's MIT license. Publishing a release is separate from building local packages.
