# CLI implementation plan

Status: proposed implementation sequence, 2026-11-07.
Repository: github.com/runtimeconditions/rc-cli; executable and command name: `rc`.
System package and Homebrew formula name: `rc-cli`; releases use independent `vMAJOR.MINOR.PATCH` tags.
This plan implements [CLI-PLANNING.md](../extensions/tooling/extension-bindings/CLI-PLANNING.md).
Binding behavior follows [IMPLEMENTATION.md, sections 12–13](../extensions/tooling/extension-bindings/IMPLEMENTATION.md).
These sibling links assume the repositories share a parent directory; published documentation must link to pinned GitHub revisions.

## 1. Scope and ownership

- This repository owns Cobra commands, binding adapters, profiler routing, diagnostics, and distribution.
- The extensions repository owns resolver, normalizer, orchestrator, emitters, schemas, and binding release rules.
- Each profiler repository owns its parser, binding inspection, semantic validation, runtime requirements, and releases.
- Ship the independently released Go profiler in every default rc-cli installation, with no separate plugin install.
- Optional languages use add-on system packages; their runtimes are not dependencies of the default installation.
- Generated binding SDKs remain workload dependencies obtained through native language package managers.
- Preserve YAML artifact contracts, stdout formats, filesystem behavior, and actual tool provenance.
- This document authorizes no external publication or edits to another repository.

## 2. Architecture and repository layout

Use Cobra for native command discovery and help, and subprocesses for independently released tools.
Choose the standalone binding-tool boundary initially: the existing Go modules have local replace directives
and are not yet a published dependency set suitable for importing into this repository.
Package that backend privately as `rc-bindings`; never resolve another `rc` executable as its backend.
A future in-process adapter requires published modules and interface parity, without copying binding logic.

~~~text
cmd/rc/main.go                         # signals, streams, root execution, process exit
internal/cli/{root,bindings,profile,plugins,doctor,version}.go
internal/bindings/adapter.go           # translates explicit Cobra options to backend argv
internal/plugins/{descriptor,discover,compatibility}.go
internal/process/runner.go             # invocation, exit status, cancellation
internal/install/layout.go            # archive, Homebrew, and Linux installation roots
schemas/plugin.schema.yaml            # add-on metadata contract
distribution/{components.lock.yaml,nfpm.yaml,platforms.yaml}
distribution/homebrew/rc-cli.rb        # template for the organization's tap
scripts/                              # build, stage, package, verify, publish helpers
.github/workflows/{checks,release}.yml
go.mod / go.sum
~~~

Pin Cobra, the CLI build Go version, and packaging tools; commit dependency checksums.
Keep command construction injectable through streams, discovery, and runner interfaces.
Follow AGENTS.md: implement complete units in batches of at most five named files and 250 changed lines per file.

## 3. Command and argument contract

~~~text
rc --help
rc version [--output yaml]
rc completion <bash|zsh|fish|powershell>
rc bindings <resolve|normalize|generate|verify|package|plan-release|check|update> [binding flags]
rc profile generate -dir ./service -out profile.yaml
rc profile go generate -dir ./service -out profile.yaml
rc profile go validate-extensions -dir ./service
rc profile python profile generate --project ./service
rc plugins list [--output yaml]
rc doctor [--language go|python|java|...] [--output yaml]
~~~

- `rc profile generate` and `rc profile go generate` invoke the same bundled Go backend and argv.
- After an explicit language selector, preserve all tokens verbatim, including flags, values, repetition, and `--`.
- Without a selector, current Go verbs generate, validate-extension, validate-extensions, and version select Go.
  Leading backend flags also select Go; unknown leading names are language errors, with no silent Go fallback.
- Bare `rc profile` and sole `rc profile --help` show host routing help; `rc profile go --help` delegates to Go.
  `rc --version` reports CLI version; `rc profile go --version` reports profiler version.
- Preserve each backend's native command tree. In particular, Python currently needs `profile generate`;
  do not invent interpreter-selection flags or strip that command prefix.
- Reserve unqualified Go verb names so future language IDs cannot shadow them.
- No root persistent flags in version 1. Scope configuration and diagnostics options to native commands;
  use `RC_CONFIG` for an explicitly selected host YAML configuration file.
- Use one raw Cobra profile dispatcher with DisableFlagParsing; do not register backend flags in Cobra.
  Handle the host-help exception explicitly; avoid unknown-flag whitelists that can consume backend arguments.
- Cobra owns native help/completion. Complete installed language names; backend flag completion is deferred.
  Shell completion and top-level help must not scan workloads, contact networks, or run all discovered plugins.

## 4. Binding commands and migration

Register all eight binding operations as Cobra children with command-specific flags and RunE handlers.
The installed adapter calls `rc-bindings bindings <operation> ...` with the caller's working directory.
Keep backend defaults by forwarding explicitly supplied flags only, except declared installed-tool defaults.
Use repeatable string-array flags without comma splitting; preserve values containing spaces or commas.
Keep --packages, --toolchain-lock, --tooling-dir, --cache, --work-dir, and --target within bindings.
Mirror the current backend's resolution, input/output/report, native-tool, and explicit artifact-path options.
The backend remains authoritative for schema checks, project discovery, target ordering, and output safety.

| Operation | Required preservation |
| --- | --- |
| resolve / normalize | YAML stdout; write only explicitly requested closure, model, and lock files. |
| generate | Required new/empty output directory; generate selected target trees. |
| verify | Inspect supplied or committed trees; optional explicit report. |
| package | Compare regeneration with committed trees, verify and archive committed source. |
| plan-release | Report compatibility and required versions without publishing. |
| check | Verify and compare committed targets without updating them. |
| update | Sole operation that synchronizes committed target trees after all candidates pass. |

Install a versioned binding-tool bundle containing the backend, schemas, normalizer, Go emitter,
and required non-language resources. Keep Python tooling optional and require it only for Python targets.
The bundle must run against an external project without an extensions source checkout.
Its release is an upstream prerequisite; missing released artifacts must remain an explicit dependency,
not trigger a build from arbitrary workspace source or an implicit network install.
The existing backend has workspace build fallbacks: the packaged release path must require explicit
installed tooling and fail if any component is absent, rather than relying on its build-time source path.
Reject symlinks resolving the backend to the common rc executable to prevent recursive invocation.
Preserve --toolchain-lock checks and report the actual executing backend and profiler identities.
Only select bundled tools automatically when they satisfy the requested lock; mismatch requires an error.

After parity checks, migrate extensions' shared binding-tools action to a pinned rc-cli artifact,
and supply explicit backend, schema, normalizer/emitter, and profiler paths for its locked workflow.
Keep generation.py and publish.py as the owners of bot commits and publication preflight.
Remove the old user-facing cmd/rc entry point only after local helpers and both binding workflows use
the common CLI successfully. Retain the independent binding backend; this is not an orchestration rewrite.

## 5. Default Go profiler integration

Use the existing go-rc-profiler executable through argv; do not embed its parser or rebuild it in rc-cli.
Add a release component lock with exact release tag, source commit, per-platform archive URL,
archive SHA-256, extracted executable SHA-256, core-schema identity, and compatibility metadata.
Resolve and review actual release values when implementing; never select "latest" during a release build.
Stage the verified upstream executable unchanged under the package's private tool directory.
The Go profiler already embeds its approved schema; preserve that binary and schema provenance.
Default discovery chooses this installed copy, with no PATH dependency for locating the profiler itself.
Explicit user overrides are permitted for interactive work and must be visible in version/doctor output.
A CLI upgrade can change the pinned profiler version; the profiler keeps its independent release identity.

A working Go toolchain and workload go.mod/dependencies remain prerequisites for analysis.
Document the tested analyzer toolchain range independently of the Go version used to compile rc-cli.
Do not infer target-toolchain compatibility solely from the profiler's current go.mod build version.
Missing Go must not prevent help, plugin listing, version output, or other languages from working.
Report the missing prerequisite without downloading Go or installing workload modules automatically.
Go-only users install neither Python nor a JVM. Native SDK resolution remains the profiler's responsibility.

## 6. Add-on protocol, configuration, and lookup

Use YAML descriptors with apiVersion, ID/language, version, protocol version, compatible CLI/schema ranges,
runtime requirements, and an executable plus fixed argument prefix. Do not encode shell command strings.
Define the strict schema and size limits before implementing discovery.
The initial Go compatibility adapter supplies descriptors from its locked package manifest and verifies
the actual binary digest and --version output; current go-rc-profiler has no capabilities handshake.
Future add-ons expose a bounded, versioned YAML describe operation as well as their native argv entry point.

Lookup order: explicit configured override (including existing RC_GO_PROFILER_BIN), managed installed
descriptor, then an explicitly named PATH fallback. Do not enumerate or execute arbitrary rc-* files.
Read descriptor directories from the installed package and explicit configuration; detect duplicate IDs,
reject incompatible protocols, and show selected origin/version/path. Help/listing can inspect metadata only.
Relative managed executable paths stay within their package root; user overrides are explicit absolute paths.
User configuration describes the profiler environment, including an interpreter and argv prefix when needed.
Python inspection must use the workload's intended binding environment; an isolated system CLI environment
cannot assume access to its distributions. Resolve that backend requirement before shipping its add-on.
Define optional packages rc-profiler-python/java/rust/node/ruby in the same OS channels as rc-cli.
Add-ons declare their own compatible runtimes; SDK installation continues through pip, Maven, npm, etc.
Initial CLI plugin commands list and inspect installations; package managers own installation and upgrades.

## 7. Process behavior and diagnostics

Attach caller stdin/stdout/stderr, preserve cwd and environment, and invoke argument slices without a shell.
Never buffer/reformat profile YAML or print launcher progress on stdout.
Propagate backend exit codes; native CLI usage errors return 2 and launcher/setup failures return 1.
Print each launcher error once; use Cobra SilenceErrors/SilenceUsage and explicitly show usage when appropriate.
Native commands produce stable YAML only when requested; version YAML includes CLI and component identities.
Doctor defaults to Go, checks the selected tools/runtime, and reports optional absent languages without
failing unrelated commands. Listing metadata does not require runtimes or network access.
On Unix, forward interrupt/termination to the child process group, allow bounded cleanup, then terminate
remaining descendants. Preserve conventional signal exit results and avoid leaking compiler subprocesses.

## 8. Distribution contents and installation

One rc-cli system package includes rc, the Go profiler, binding backend/resources, manifests, licenses,
notices, and static shell completions. Core generation tools are included; other language tooling is optional.
Use prefix-relative bin/rc, libexec/rc-cli tools, and share/rc/extension-bindings for archives/Homebrew.
Use /usr/bin/rc, /usr/lib/rc-cli tools, and /usr/share/rc/extension-bindings in Debian packages.
Resolve Homebrew symlinks to the formula prefix; never hard-code /opt/homebrew or /usr/local.
For archives, require extraction of the complete tree and document adding its bin directory to PATH.
Ship install-layout metadata for both profiles; relocating the tree must preserve managed tool lookup.
Use rc-cli as the package/formula name and rc as the executable; do not rename the accepted command.
Do not declare replacement of unrelated distro packages. Package removal deletes only its owned files.
Go tooling is a documented analysis prerequisite, not an unconditional dependency of commands like help.
The installed bundle runs without source checkouts and without bootstrap downloads at first invocation.

Initial proposed matrix: macOS and Linux amd64/arm64; Debian 12/13 and Ubuntu 24.04/26.04 for APT.
Declare tested macOS minimum versions and any libc requirements in platforms.yaml before publishing.
Validate the upstream profiler and binding-tool availability for every advertised target; no partial bundles.
Windows and RPM channels follow after the initial package contract; do not advertise them as supported yet.

## 9. Build, release, and package channels

- Tag rc-cli independently; pin component locks, Cobra, build Go, packaging tools, and Actions by full SHA.
- For tag-triggered release builds, validate the vMAJOR.MINOR.PATCH tag (including prereleases) and
  checked-out commit, then use the schema-aware build helper to embed GITHUB_REF_NAME,
  GITHUB_SHA, and the verified core profile schema as linker values:

  ~~~sh
  go run ./scripts/build.go --output dist/rc \
    --version "${GITHUB_REF_NAME}" --commit "${GITHUB_SHA}"
  ~~~

  The helper downloads the pinned schema and verifies its SHA-256 before embedding it.
  For an already downloaded artifact, add --core-schema /path/to/runtimeconditions.profile.schema.yaml;
  the same checksum verification applies. Plain go build/go install omit the required schema value,
  causing profile generation to fail with "installed profiler has no verified core profile schema".
  Local builds use go run ./scripts/build.go --output ./rc, with optional version/commit flags.
  Verify rc version, rc --version, and rc version --output yaml report the release tag and source commit.
  Reject dev/unknown metadata in release artifacts; ordinary local builds retain those defaults.
- Assemble locked upstream bytes and the verified CLI once; create a complete inventory and package without rebuilding.
- Produce platform tar.gz archives, SHA256SUMS, component YAML, and .deb packages using pinned nFPM.
  Map Go amd64/arm64 to the corresponding Debian architecture fields and include package descriptions/licenses.
- Verify staged files against the inventory and compare Go binary bytes with the pinned profiler artifact.
- Keep native package dependencies explicit; no install hooks invoking pip/npm, curl scripts, or source builds.
- Publish GitHub Release assets only after platform acceptance; promote verified bytes without rebuilding.
- Publish Formula/rc-cli.rb into runtimeconditions/homebrew-tools using platform archive URLs and SHA-256.
  A default brew install runtimeconditions/tools/rc-cli installs the complete bundle and completions.
- Publish Debian packages through one signed APT host with separate tested suite indexes where needed.
  Generate Packages/Release/InRelease from immutable packages with a pinned repository tool such as reprepro.
- Document the public key fingerprint and bootstrap; use a dedicated keyring and Signed-By in a .sources entry.
  APT users configure the repository once, then apt install/upgrade rc-cli and optional language packages.
- Put signing credentials in the protected publication environment; serialize repository updates and publish
  metadata after referenced packages. Retain prior artifacts and document key rotation and rollback policy.
- A hosting repository/site and release credentials are deployment inputs; create no organization requirement.
  GitHub Pages/static HTTPS hosting or a managed APT service can host the same repository layout.
- Keep prereleases out of the stable tap/APT channel. Retries verify existing asset hashes and are idempotent.

## 10. Acceptance and ordered work packages

| Step | Deliverable and completion evidence |
| --- | --- |
| 1. CLI foundation | Go module, Cobra root/version/completion, injectable dependencies; clean independent build. |
| 2. Go default | Locked Go bundle, raw profile dispatch, identical default/explicit Go results and native help. |
| 3. Add-on contract | Strict YAML schema, configuration/discovery, compatibility errors, plugin list and doctor. |
| 4. Binding adapter | Released standalone bundle prerequisite; all eight commands match the existing interface. |
| 5. Packaging | Relocatable archives, Homebrew formula, .deb installs/upgrades/removals on the stated matrix. |
| 6. Workflow migration | Extensions helpers use a pinned common CLI; old entry point is removed after parity. |
| 7. Publication | Protected release, tap/APT promotion, documented installation, checksums and rollback procedure. |

Add focused tests for argv boundaries (spaces, repeated options, --, unknown flags), default Go aliases,
backend-owned help, error/exit propagation, cancellation, duplicate IDs, missing/incompatible tools,
explicit override selection, and stdout byte preservation using controlled subprocess fixtures.
Compare generated Go profile bytes and negative-case behavior with direct go-rc-profiler invocation.
Compare binding YAML, target defaults, lock failures, cleanup, atomic updates, and output paths with the old CLI.
Install in clean VMs/containers from complete artifacts, away from checkout paths and without a profiler on PATH.
Run a Go workload using rc-cli alone plus the documented Go toolchain and resolved workload bindings.
Check that no Python/JVM is required; optional-language errors identify the add-on/runtime to install.
Verify upgrade/removal ownership, Homebrew prefix handling, APT metadata/signatures, and retained component hashes.
A release is ready only after all eight binding operations and bundled Go profiling pass installed-package
acceptance, and both distribution channels install the same reviewed component versions.

## References

- [Cobra command/flag behavior](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md) and [DisableFlagParsing](https://github.com/spf13/cobra/blob/main/command.go).
- [Homebrew formula packaging](https://docs.brew.sh/Formula-Cookbook) and [nFPM configuration](https://nfpm.goreleaser.com/docs/configuration/).
- [APT authentication](https://manpages.debian.org/testing/apt/apt-secure.8.en.html) and [repository key scoping](https://manpages.debian.org/testing/apt/sources.list.5.en.html).
