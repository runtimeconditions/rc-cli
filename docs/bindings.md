# Binding commands (`rc bindings`)

This page is for extension authors and contributors who generate, verify, or release binding
packages. Users profiling a workload only need the `rc profile` commands — see the
[command table](../README.md#commands).

`rc bindings` resolves, generates, and verifies extension bindings. The command group
provides `resolve`, `normalize`, `generate`, `verify`, `package`, `plan-release`, `check`,
and `update`. Cobra owns help, completion, and flag parsing; execution uses the shared
binding library from `extensions`.

Binding commands also require the shared YAML schemas. Installed layouts place them under
`share/rc/extension-bindings/model/`, alongside `bin/rc`. CI stages these files from the
exact `extensions` commit in `distribution/components.lock.yaml`; they remain ordinary YAML
files.

The local build helper builds the executable. For local binding development, supply
`--tooling-dir` pointing to matching schema resources,
such as the `tooling/extension-bindings` directory in a checkout of that pinned revision:

```sh
rc bindings generate \
  --packages /path/to/binding-project/packages.yaml \
  --toolchain-lock /path/to/binding-project/toolchain.lock.yaml \
  --tooling-dir /path/to/extensions/tooling/extension-bindings \
  --core-schema /path/to/runtimeconditions.profile.schema.yaml \
  --output /tmp/generated-bindings
```

`packages.yaml` selects extension roots, language targets, and native package versions.
`toolchain.lock.yaml` declares the applicable toolchain constraints. Use repeatable
`--target package-key:language` flags to limit the selected targets. `generate` requires a
new or empty output directory. `verify --input` accepts the generated tree; use the same
catalog and resource flags for these commands. `update` verifies candidates before changing
committed binding directories; `check` compares regenerated content without updating those
files. Other shared flags include `--cache`, `--work-dir`, `--catalog-root`,
`--dependency-lock`, `--extension-root`, and `--extension-override`; see
`rc bindings generate --help` for the full list.

The core schema must match the version declared in the package catalog. Without
`--core-schema`, normalization uses upstream core-schema resolution. Offline generation
also requires locally available extension definitions; native package verification and
profiling require resolved package dependencies and Go.

The normalizer and Go emitter run as linked libraries. Explicit executable overrides for
those tools (`--normalizer`, `--go-emitter`) conflict with this mode. For binding
provenance, `--go-profiler /absolute/path/to/go-rc-profiler` selects a standalone profiler
file; otherwise the linked profiler's actual module version and checksum are recorded. The
`rc profile` commands always use the linked Go profiler.
