# rc-cli

`rc-cli` is the Runtime Conditions CLI. A Runtime Conditions Profile is a portable,
machine-readable list of what one workload needs at runtime: the APIs, caches, datastores,
and environment configuration it depends on.

`rc-cli` **generates** Runtime Conditions Profiles from your workload's binding declarations
and **validates** profiles against the spec and the extensions it declares, whether the
profile was written by hand, by an agent, or by a profiler. The profile states the demand;
platforms and adapters decide how to fulfill it.

**Example scenario:** Your service depends on a [Todos API](https://github.com/runtimeconditions/rc-demos/tree/main/apps/todos-api)
reachable at `TODOS_API_URL` and a
Redis cache at `REDIS_URL`. Today that knowledge lives in a wiki page or someone's head, and
the platform team provisions based on often incomplete or outdated information. A Runtime Conditions Profile writes it down where
the code lives: you declare the need once in your source, `rc profile generate` reads those
declarations and emits the profile as YAML, and `rc profile validate` checks the profile is
well-formed and matches the extensions it uses. Then Kratix, Crossplane, Radius, or Backstage
can read the same file and fulfill the need. No re-explaining, no drift between docs and
reality.

- Guide and concepts: <https://runtimeconditions.io>
- Profile specification: <https://github.com/runtimeconditions/spec> defines the profile
  format. `rc` pins the spec's core schema (checksum-verified) at build time and validates
  every profile against it.

## Install

The package is named `rc-cli`; the executable is named `rc`.

**Homebrew (macOS and Linux):**

```sh
brew install runtimeconditions/tools/rc-cli
```

**Linux packages (apt / dnf / yum):** configure the signed package source once, then install
with your package manager:

```sh
curl -fsSL https://runtimeconditions.io/packages/setup.sh | sudo sh
sudo apt install rc-cli    # Debian / Ubuntu
sudo dnf install rc-cli    # Fedora / RHEL
```

Also:

- Language profilers are separate add-ons. Go profiling is built into `rc`, so Go workloads
  need nothing else. TypeScript, Java, and Python profilers install through their own
  channels; see [Language profilers](docs/language-profilers.md).
- To build the CLI yourself, see [Build from source](docs/build-from-source.md).

## Quickstart

This works on a fresh install. It validates a committed demo profile; it does not create
one.

Check a profile against the core schema embedded in rc and the definitions of the extensions it declares:

```console
$ git clone https://github.com/runtimeconditions/rc-demos.git
$ rc profile validate rc-demos/artifacts/request-logger-http.profile.yaml
runtimeconditions: profile validated
```

Definitions download from the https ids the profile names by default. Use `--catalog <dir>` (repeatable) to read them from local directories:

```console
$ rc profile validate profile.yaml --catalog ./my-extensions
# Use - to read the profile from stdin:
$ rc profile validate - < profile.yaml
```

## Commands

Verify the install:

```console
$ rc --version
rc v0.6.0 (a1b2c3d)
# rc version --output yaml adds platform and linked-component details:
$ rc version --output yaml
```

```console
$ rc plugins list
LANGUAGE    STATUS    VERSION  PATH
go          built-in  v0.8.0
typescript  missing
java        missing
python      missing
# --output yaml prints each missing add-on's install command:
$ rc plugins list --output yaml
```

- `go` is the profiler linked into `rc`; nothing extra to install.
- `missing` add-ons only matter for workloads in that language.

`rc profile` uses the built-in Go profiler by default; the same commands exist under
`rc profile go` with identical flags. Full command list:

| Command | Purpose |
| --- | --- |
| `rc profile generate` | Generate a validated profile from Go binding declarations. Flags: `--dir` (default `.`), `--name`, `--workload-uri`, `--workload-version` (default `dev`), `--out` (default stdout). |
| `rc profile validate <profile.yaml>` | Validate any profile against the embedded core schema and its extensions. Flags: `--catalog` (repeatable). |
| `rc profile validate-extensions` | Validate all imported generated binding packages. Flag: `--dir`. |
| `rc profile validate-extension` | Validate one imported binding package. Flags: `--dir`, `--package`. |
| `rc profile version` | Show the linked Go profiler version. |
| `rc plugins list [--output yaml]` | Show the Go profiler and each add-on's status, version, and path. |
| `rc version [--output text\|yaml]` | Show CLI build information. `rc --version` prints the same one-liner. |
| `rc completion <bash\|zsh\|fish\|powershell>` | Print a shell completion script. |

Every command has `--help` for its full flag list, e.g. `rc profile go generate --help` and
`rc bindings --help`.

## Documentation

- [Generating a profile](docs/generating-profiles.md) — prerequisites, example workload, flags
- [Language profilers](docs/language-profilers.md) — TypeScript, Java, Python add-ons
- [Binding commands](docs/bindings.md) — `rc bindings`, for extension authors
- [Build from source](docs/build-from-source.md) — building and hacking on `rc` itself

## Ecosystem

Independently versioned repos around `rc-cli`. How each connects to the CLI:

- [extensions](https://github.com/runtimeconditions/extensions) — `rc bindings` calls its
  tooling as a library pinned to an exact commit in `distribution/components.lock.yaml`
- [go-rc-profiler](https://github.com/runtimeconditions/go-rc-profiler) — compiled into `rc`
  as a Go module, so Go profiling needs no separate install
- [ts-rc-profiler](https://github.com/runtimeconditions/ts-rc-profiler),
  [java-rc-profiler](https://github.com/runtimeconditions/java-rc-profiler),
  [python-rc-profiler](https://github.com/runtimeconditions/python-rc-profiler) — add-ons
  that `rc` finds on `PATH` and runs via `rc profile <language> ...` (see
  [Language profilers](docs/language-profilers.md))
- [rc-demos](https://github.com/runtimeconditions/rc-demos) — demos with committed profiles;
  the [quickstart](#quickstart) validates one of them
- [backstage-plugin](https://github.com/runtimeconditions/backstage-plugin) — consumes
  profiles in Backstage; independent of `rc`
- [ebpf-rc-profiler](https://github.com/runtimeconditions/ebpf-rc-profiler) — legacy,
  experimental eBPF prototype (watches syscalls/network traffic); kept for reference, not
  wired into `rc`
- [homebrew-tools](https://github.com/runtimeconditions/homebrew-tools) and
  [packages](https://github.com/runtimeconditions/packages) — distribution channels that
  ship `rc` itself: the Homebrew tap and the apt/dnf/yum source

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) — Copyright (c) 2026 Runtime Conditions.
