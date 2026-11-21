# Contributing

## Building from Source

```bash
# Clone the repository
git clone https://github.com/runtimeconditions/rc-cli.git
cd rc-cli

# Build the CLI with the schema-aware helper (plain `go build` omits the embedded schema)
go run ./scripts/build.go --output ./rc

# Verify it works
./rc --version
```

See [docs/build-from-source.md](docs/build-from-source.md) for the schema checksum,
`--core-schema`, and release `--version`/`--commit` flags.

## Development Workflow

| Path | What lives there |
| --- | --- |
| `cmd/rc/` | Executable entry point: signals, streams, exit codes |
| `internal/cli/` | Cobra command tree (`root`, `profile`, `plugins`, `bindings`) and their tests |
| `internal/plugins/` | Language add-on discovery and compatibility checks |
| `internal/process/` | Subprocess runner for add-ons |
| `internal/validate/` | Profile validation against the embedded core schema |
| `scripts/` | Build, stage, smoke-test, and release helpers |
| `distribution/` | Component lock, nFPM config, Homebrew formula template, platform matrix |
| `testdata/` | CI fixtures (bindings, profiles) |

To add or change a command:

1. Add the Cobra command in `internal/cli/` — Go commands use native flags; language add-on
   branches pass argv through unchanged
2. Add tests in the matching `*_test.go` file
3. Update `--help` examples and `README.md` / `docs/` when the user-visible surface changes

Binding logic (resolve, normalize, generate, verify, …) lives in
[`extensions`](https://github.com/runtimeconditions/extensions), pinned in
`distribution/components.lock.yaml`; this repo calls it as a library, it does not reimplement it.

## Testing

```bash
gofmt -l .        # CI fails if this prints anything
go vet ./...
go test -race ./...
go mod verify
```

CI runs these on Linux and macOS (amd64/arm64), then builds with `scripts/build.go`, relocates
the staged executable and YAML resources, and runs the smoke harnesses (`scripts/ci-smoke.go`,
`scripts/ci-bindings-smoke.py`) against `testdata/ci/` fixtures, including negative checks.
CI builds with Go 1.26.5 and tests workload analysis with Go 1.25.0. Releases are tag-driven
(`.github/workflows/release.yml`).

## Pull Requests

- Build with the schema-aware helper (`go run ./scripts/build.go --output ./rc`), not plain `go build`
- Keep `gofmt` clean and `go test -race ./...` passing
- Update `README.md` / `docs/` alongside any command, flag, or output change
- Keep error messages actionable: name the missing prerequisite and how to install it
- Don't commit generated bindings, profiles, or other CI outputs
