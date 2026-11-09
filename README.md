# rc-cli
The CLI entrypoint with Golang profiling support, extendable with language-specific profilers

## Build locally

From this repository's root, build the executable with the schema-aware helper:

```sh
go run ./scripts/build.go --output ./rc
./rc --version
./rc profile generate --dir /path/to/go/workload --out profile.yaml
```

The helper downloads the pinned core profile schema, verifies its SHA-256,
and embeds it alongside the linked Go profiler. The resulting binary needs no
schema download or separate profiler installation at runtime. Analyzing a
workload requires a working Go toolchain and its binding dependencies.

A plain `go build` or `go install` omits the schema linker value. Such a binary
can display help and version information, but profile generation fails with
`rc: installed profiler has no verified core profile schema`.
Rebuild it using the command above and run the resulting `./rc` executable.

For a build using an already downloaded schema, supply the approved artifact:

```sh
go run ./scripts/build.go --output ./rc \
  --core-schema /path/to/runtimeconditions.profile.schema.yaml
```

The local schema must match the helper's pinned checksum. Release builds also
pass `--version <release-tag>` and `--commit <source-commit>` to embed provenance.
