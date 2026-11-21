# Build from source

Everything on this page is for people building or modifying `rc` itself. Users who just
installed the CLI don't need any of it.

From this repository's root, build the executable with the schema-aware helper:

```sh
go run ./scripts/build.go --output ./rc
./rc --version
./rc profile generate --dir /path/to/go/workload --out profile.yaml
```

The helper downloads the pinned core profile schema, verifies its SHA-256, and embeds it
alongside the linked Go profiler. Go profiling needs no runtime schema download or separate
profiler installation. Analyzing a workload requires a working Go toolchain and its binding
dependencies. CI builds with Go 1.26.5 and tests workload analysis with Go 1.25.0.

A plain `go build` or `go install` omits the schema linker value. Such a binary can display
help and version information, but profile generation fails with
`rc: installed profiler has no verified core profile schema`. Rebuild it using the command
above and run the resulting `./rc` executable.

For a build using an already downloaded schema, supply the approved artifact:

```sh
go run ./scripts/build.go --output ./rc \
  --core-schema /path/to/runtimeconditions.profile.schema.yaml
```

The local schema must match the helper's pinned checksum. Release builds also pass
`--version <release-tag>` and `--commit <source-commit>` to embed provenance.

CI tests Linux and macOS on amd64 and arm64. It relocates the staged executable and YAML
resources, exercises all eight binding operations, installs a freshly generated Go module
through a local proxy, and profiles its declarations through both Go command routes.
Negative checks cover altered committed source, nonempty output directories, and invalid
declarations preserving existing profile output. Only Go is available on the smoke test's
subprocess PATH; Python is used by the CI harness, and is not a runtime dependency of Go
binding or profiling commands.

The small extension fixture is checked in as YAML under `testdata/ci/bindings/`. Generated
packages, archives, profiles, and module caches remain temporary CI outputs. Homebrew/APT
packaging and optional profiler add-ons remain planned.
