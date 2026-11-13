# rc-cli
Runtime Conditions commands for extension bindings and workload profiling.

## Build locally

From this repository's root, build the executable with the schema-aware helper:

```sh
go run ./scripts/build.go --output ./rc
./rc --version
./rc profile generate --dir /path/to/go/workload --out profile.yaml
```

The helper downloads the pinned core profile schema, verifies its SHA-256,
and embeds it alongside the linked Go profiler. Go profiling needs no runtime
schema download or separate profiler installation. Analyzing a workload requires
a working Go toolchain and its binding dependencies. CI builds with Go 1.26.5
and tests workload analysis with Go 1.25.0.

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

## Built-in Go profiler commands

`rc profile` uses the built-in Go profiler by default. The same commands are
available under `rc profile go`, with identical flags and behavior:

| Command | Purpose |
| --- | --- |
| `rc profile generate` | Generate a validated profile from Go binding declarations. |
| `rc profile validate-extensions` | Validate all imported generated binding packages. |
| `rc profile validate-extension` | Validate one imported binding package selected with `--package`. |
| `rc profile version` | Show the linked Go profiler version. |

For command help:

```sh
./rc profile --help
./rc profile go --help
./rc profile go generate --help
```

Generate a profile using the default or explicit Go route:

```sh
./rc profile generate --dir /path/to/go/workload --out profile.yaml

./rc profile go generate \
  --dir /path/to/go/workload \
  --name my-workload \
  --workload-uri https://example.com/my-workload \
  --workload-version 1.0.0 \
  --out profile.yaml
```

`--dir` defaults to the current directory. Generation writes YAML to stdout
when `--out` is omitted. The default profile name is the workload directory's
basename; the default workload URI is its enclosing Go module path, falling
back to the directory path. The workload version defaults to `dev`.

Validate imported bindings or inspect the profiler version:

```sh
./rc profile go validate-extensions --dir /path/to/go/workload
./rc profile go validate-extension \
  --dir /path/to/go/workload \
  --package runtimeconditions.io/x/rc/common
./rc profile go version
```

Profiling analyzes declarations without executing the workload. Resolve the
workload's Go binding dependencies before running these commands. A failed
generation leaves an existing `--out` file unchanged.


## Optional language profilers

Other languages run a separate add-on that is installed next to rc. `rc` finds it,
checks that it fits, and runs it with your arguments unchanged.

| Command | Purpose |
| --- | --- |
| `rc profile typescript <command> [flags]` | Run the installed TypeScript profiler. |
| `rc plugins list [--output yaml]` | Show the built-in Go profiler and each add-on's status, version, and path. |

The TypeScript profiler is the npm package `@runtimeconditions/typescript-profiler`:

```sh
npm install --global @runtimeconditions/typescript-profiler
rc plugins list
rc profile typescript generate --project ./service --name orders \
  --workload-uri https://example.com/orders --out profile.yaml
```

- Everything after `typescript` goes to the add-on as it is, including flags, repeated
  values, `--`, and `--help`. The add-on keeps its own command tree and flags (the
  TypeScript profiler takes `--project`, where the Go commands take `--dir`).
- `rc` looks for `rc-profiler-typescript`, then `runtimeconditions-typescript-profiler`, on
  `PATH`. `RC_TYPESCRIPT_PROFILER` can name an absolute path instead, and then wins.
- Before it runs the add-on, `rc` runs `<add-on> --version --json` and requires its
  `coreSchemas` to include the core profile schema `rc` targets (0.4.0).
- The add-on's output and exit status pass through. `rc` adds nothing when the add-on fails.
- A missing add-on, a missing runtime (Node.js for TypeScript), and an incompatible
  version are each reported once, with what to install.
- Ctrl-C and termination signals are forwarded to the add-on's process group.

## Binding commands

```sh
./rc bindings --help
./rc bindings generate --help
```

The command group provides `resolve`, `normalize`, `generate`, `verify`,
`package`, `plan-release`, `check`, and `update`. Cobra owns help, completion,
and flag parsing; execution uses the shared binding library from `extensions`.

Binding commands also require the shared YAML schemas. Installed layouts place
them under `share/rc/extension-bindings/model/`, alongside `bin/rc`. CI stages
these files from the exact `extensions` commit in
`distribution/components.lock.yaml`; they remain ordinary YAML files.

The local build helper builds the executable. For local binding development,
supply `--tooling-dir` pointing to matching schema resources, such as the
`tooling/extension-bindings` directory in a checkout of that pinned revision:

```sh
./rc bindings generate \
  --packages /path/to/binding-project/packages.yaml \
  --toolchain-lock /path/to/binding-project/toolchain.lock.yaml \
  --tooling-dir /path/to/extensions/tooling/extension-bindings \
  --core-schema /path/to/runtimeconditions.profile.schema.yaml \
  --output /tmp/generated-bindings
```

`packages.yaml` selects extension roots, language targets, and native package
versions. `toolchain.lock.yaml` declares the applicable toolchain constraints.
Use repeatable `--target package-key:language` flags to limit the selected targets.
`generate` requires a new or empty output directory. `verify --input` accepts the
generated tree; use the same catalog and resource flags for these commands.
`update` verifies candidates before changing committed binding
directories; `check` compares regenerated content without updating those files.

The core schema must match the version declared in the package catalog. Without
`--core-schema`, normalization uses upstream core-schema resolution. Offline
generation also requires locally available extension definitions; native package
verification and profiling require resolved package dependencies and Go.

The normalizer and Go emitter run as linked libraries. Explicit executable
overrides for those tools conflict with this mode. For binding provenance,
`--go-profiler /absolute/path/to/go-rc-profiler` selects a standalone profiler
file; otherwise the linked profiler's actual module version and checksum are
recorded. The `rc profile` commands always use the linked Go profiler.

## CI coverage and distribution

CI tests Linux and macOS on amd64 and arm64. It relocates the staged executable
and YAML resources, exercises all eight binding operations, installs a freshly
generated Go module through a local proxy, and profiles its declarations through
both Go command routes. Negative checks cover altered committed source, nonempty
output directories, and invalid declarations preserving existing profile output.
Only Go is available on the smoke test's subprocess PATH; Python is used by the
CI harness, and is not a runtime dependency of Go binding or profiling commands.

The small extension fixture is checked in as YAML under `testdata/ci/bindings/`.
Generated packages, archives, profiles, and module caches remain temporary CI
outputs. Homebrew/APT packaging and optional profiler add-ons remain planned.
