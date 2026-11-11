# CLI implementation plan

Status: implementation sequence, updated 2026-11-11. Native Go profiling and binding
commands are implemented, with CI coverage for the relocated CLI and shared YAML resources.
System-package distribution, add-ons, and upstream workflow migration remain planned.
Repository: github.com/runtimeconditions/rc-cli; executable and command name: `rc`.
System package and Homebrew formula name: `rc-cli`; releases use independent `vMAJOR.MINOR.PATCH` tags.
This plan implements [CLI-PLANNING.md](../extensions/tooling/extension-bindings/CLI-PLANNING.md).
Binding behavior follows [IMPLEMENTATION.md, sections 12–13](../extensions/tooling/extension-bindings/IMPLEMENTATION.md).
These sibling links assume the repositories share a parent directory; published documentation must link to pinned GitHub revisions.

## 1. Scope and ownership

- This repository owns Cobra commands, binding adapters, profiler routing, diagnostics, and distribution.
- The extensions repository owns resolver, normalizer, orchestrator, emitters, schemas, and binding release rules.
- Each profiler repository owns its parser, binding inspection, semantic validation, runtime requirements, and releases.
- Link the independently versioned Go profiler into rc, with no separate profiler installation.
- Optional languages use add-on system packages; their runtimes are not dependencies of the default installation.
- Generated binding SDKs remain workload dependencies obtained through native language package managers.
- Preserve YAML artifact contracts, stdout formats, filesystem behavior, and actual tool provenance.
- This document authorizes no external publication or edits to another repository.

## 2. Architecture and repository layout

Use Cobra for native commands and direct Go library calls for binding operations and Go profiling.
Import the orchestrator, normalizer, and Go emitter as a resolvable, pinned dependency set.
Pin the three nested binding modules to the same full extensions commit recorded in
distribution/components.lock.yaml. go.mod explicitly requires those versions and excludes
the normalizer/emitter v0.0.0 workspace placeholders; local replace directives are not
propagated into rc-cli. Keep binding logic in extensions and retain its standalone CLI.
Subprocesses remain appropriate for optional language backends and native language toolchains.

~~~text
cmd/rc/main.go                         # signals, streams, root execution, process exit
internal/cli/{root,profile,plugins,doctor,version}.go
internal/cli/bindings.go              # Cobra options and direct orchestrator.Execute calls
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
rc profile generate --dir ./service --out profile.yaml
rc profile go generate --dir ./service --out profile.yaml
rc profile go validate-extensions --dir ./service
rc profile python profile generate --project ./service
rc plugins list [--output yaml]
rc doctor [--language go|python|java|...] [--output yaml]
~~~

- `rc profile generate` and `rc profile go generate` use the same Cobra constructors and linked Go APIs.
- Register generate, validate-extension, validate-extensions, and version on both Go command routes.
  Commands bind typed options; there is no manual Go argument parser or implicit verb for leading flags.
- Bare `rc profile`, `rc profile --help`, and `rc profile go --help` show native Cobra help.
  `rc --version` reports CLI version; `rc profile go --version` reports profiler version.
- After an optional language selector, preserve backend tokens, including values, repetition, and `--`.
  Preserve each optional backend's native command tree. Python currently needs `profile generate`;
  do not invent interpreter-selection flags or strip that command prefix.
- Reserve unqualified Go verb names so future language IDs cannot shadow them.
- No root persistent flags in version 1. Scope configuration and diagnostics options to native commands;
  use `RC_CONFIG` for an explicitly selected host YAML configuration file.
- Optional language branches may use DisableFlagParsing; Go commands use native Cobra flags.
  Avoid unknown-flag whitelists that can consume optional backend arguments.
- Cobra owns native help/completion. Complete installed optional languages; their flag completion is deferred.
  Shell completion and top-level help must not scan workloads, contact networks, or run all discovered plugins.

## 4. Binding commands and migration

All eight binding operations are Cobra children with command-specific flags and RunE handlers.
bindings.go calls orchestrator.Execute with context, absolute caller directory, operation,
Options, and output streams. There is no separate adapter package or second argument parser.
Cobra maintains the command interface in rc-cli; the shared library owns operation behavior.
Parity tests compare command and flag names against upstream Run help, defaults against
DefaultOptions with declared linked-library overrides, and results against Execute.
Initialize native-tool defaults with orchestrator.DefaultOptions, then apply Cobra values and declared
installed-library defaults. Preserve explicitly supplied values, including empty tool selections.
Use repeatable string-array flags without comma splitting; preserve values containing spaces or commas.
Keep --packages, --toolchain-lock, --tooling-dir, --cache, --work-dir, and --target within bindings.
Mirror the current backend's resolution, input/output/report, native-tool, and explicit artifact-path options.
The upstream library remains authoritative for schema checks, project discovery, target ordering, and output safety.

| Operation | Required preservation |
| --- | --- |
| resolve / normalize | YAML stdout; write only explicitly requested closure, model, and lock files. |
| generate | Required new/empty output directory; generate selected target trees. |
| verify | Inspect supplied or committed trees; optional explicit report. |
| package | Compare regeneration with committed trees, verify and archive committed source. |
| plan-release | Report compatibility and required versions without publishing. |
| check | Verify and compare committed targets without updating them. |
| update | Sole operation that synchronizes committed target trees after all candidates pass. |

Link the versioned binding modules and call exported normalizer and Go emitter functions directly.
Provide the approved schema bundle and required resources without requiring an extensions checkout.
Keep Python tooling optional and require it only for Python targets. Missing upstream module versions
or installed resources remain explicit prerequisites; do not build arbitrary workspace tools at runtime.
rc-cli selects direct Go normalizer/emitter execution and linked profiler identities by default.
The normalizer/emitter executable flags are retained for interface parity; nonempty selections
conflict with the linked-library mode. Explicit --go-profiler selects a standalone artifact,
including preserving an explicitly empty selection as an error when an artifact is required.
Help and completion do not perform resource discovery or require a profiler identity.
Preserve --toolchain-lock checks and report actual orchestrator, emitter, normalizer, and profiler identities.
Linked profiler identities use their module version and dependency checksum; executable identities retain
their artifact SHA-256. Update schemas and their consumers before emitting the new identity form.
A linked identity cannot satisfy an executable lock; every identity mismatch requires an error.

After parity checks, extensions' shared binding-tools action may adopt a pinned rc-cli artifact
with explicit resource and tool selections matching its lock.
Keep generation.py and publish.py as the owners of bot commits and publication preflight.
Retain extensions' standalone cmd/rc entry point and generation workflows independently of rc-cli.
Standalone Go profiler files remain supported by rc bindings through --go-profiler.
The independent extensions CLI retains its existing flag and environment selections.

CI checks out the locked extensions revision during the build and verifies that all three
Go dependency origins match it. It copies model/*.schema.yaml into the staged
share/rc/extension-bindings/model directory and generates checksums in the build output.
Schemas are not a Go module or a separately versioned archive. Installed lookup resolves
the executable's symlinks before finding this directory; --tooling-dir overrides it.
Local executable-only builds require --tooling-dir for binding commands.

CI downloads the core schema using its component-lock URL and verifies its checksum before
passing the same file to the build helper and binding smoke test. Go profiling embeds that
verified core schema; binding normalization accepts --core-schema or upstream resolution.
Offline binding operations also need local definitions and resolved native dependencies.
Linked normalizer/emitter provenance records each component's name/version and the host
executable SHA-256. Rebuilding rc therefore changes these identities and the model digest.
The linked Go profiler uses its independently versioned module and Go dependency checksum.
go-rc-profiler v0.6.1 supports this identity and the existing standalone artifact form.

## 5. Default Go profiler integration

Require go-rc-profiler in go.mod at a pinned version and commit its dependency checksum in go.sum.
Native Cobra commands call extractor.ExtractGeneratedDir, extractor.MarshalProfileYAML, and
extensioncheck.ValidateImportedGoPackages through typed options. Report the actual linked module
version and checksum in version YAML. A CLI upgrade may change that independently versioned dependency.
The schema-aware build helper downloads and verifies the pinned core schema, then embeds it in rc.
No profiler executable, profiler PATH lookup, or runtime schema download is needed for Go profiling.
Component metadata must describe linked modules and the approved core schema. Existing standalone
profiler archive locks/staging helpers are legacy utilities, not the default Go runtime path.
Go extraction currently has no context parameter; check cancellation before and after that API call.

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
The built-in Go profiler uses linked dependency metadata and needs no add-on descriptor or handshake.
Future add-ons expose a bounded, versioned YAML describe operation as well as their native argv entry point.

Optional add-on lookup order: explicit configured override, managed installed
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
The extensions CLI retains RC_GO_PROFILER_BIN for standalone binding-tool selection.
rc bindings uses its linked profiler by default and accepts explicit --go-profiler selection;
neither setting replaces the profiler linked into native rc profile commands.

## 7. Process behavior and diagnostics

For subprocesses, attach caller streams, preserve cwd and environment, and use argument slices without a shell.
Linked APIs receive context and output writers; their YAML serialization stays with the owning library.
Keep progress off stdout. Propagate subprocess exit codes; native usage errors return 2 and other failures 1.
Print each launcher error once; use Cobra SilenceErrors/SilenceUsage and explicitly show usage when appropriate.
Native commands produce stable YAML only when requested; version YAML includes CLI and component identities.
Doctor defaults to Go, checks the selected tools/runtime, and reports optional absent languages without
failing unrelated commands. Listing metadata does not require runtimes or network access.
On Unix, forward interrupt/termination to the child process group, allow bounded cleanup, then terminate
remaining descendants. Preserve conventional signal exit results and avoid leaking compiler subprocesses.

## 8. Distribution contents and installation

One rc-cli system package includes rc with its linked Go libraries, binding resources, manifests,
licenses, notices, and static shell completions. Other language tooling is optional.
Use prefix-relative bin/rc, libexec/rc-cli tools, and share/rc/extension-bindings for archives/Homebrew.
Use /usr/bin/rc, /usr/lib/rc-cli tools, and /usr/share/rc/extension-bindings in Debian packages.
Resolve Homebrew symlinks to the formula prefix; never hard-code /opt/homebrew or /usr/local.
For archives, require extraction of the complete tree and document adding its bin directory to PATH.
Ship install-layout metadata for both profiles; relocating the tree must preserve managed tool lookup.
Use rc-cli as the package/formula name and rc as the executable; do not rename the accepted command.
Do not declare replacement of unrelated distro packages. Package removal deletes only its owned files.
Go tooling is a documented analysis prerequisite, not an unconditional dependency of commands like help.
Installed tooling and shared contracts require no source checkout or bootstrap download.
Extension definitions and normalization's core-schema inputs retain upstream resolution rules;
supply local definitions and --core-schema when those inputs must remain offline.

Initial proposed matrix: macOS and Linux amd64/arm64; Debian 12/13 and Ubuntu 24.04/26.04 for APT.
Declare tested macOS minimum versions and any libc requirements in platforms.yaml before publishing.
Validate linked dependency compatibility and resource availability for every advertised target; no partial bundles.
Windows and RPM channels follow after the initial package contract; do not advertise them as supported yet.

## 9. Build, release, and package channels

- Tag rc-cli independently; pin component locks, Cobra, build Go, packaging tools, and Actions by full SHA.
- For tag-triggered release builds, validate the vMAJOR.MINOR.PATCH tag (including prereleases) and
  checked-out commit, then use the schema-aware build helper to embed GITHUB_REF_NAME,
  GITHUB_SHA, and the verified core profile schema as linker values:

  ~~~sh
  go run ./scripts/build.go --output dist/bin/rc \
    --version "${GITHUB_REF_NAME}" --commit "${GITHUB_SHA}"
  ~~~

  The helper downloads the pinned schema and verifies its SHA-256 before embedding it.
  For an already downloaded artifact, add --core-schema /path/to/runtimeconditions.profile.schema.yaml;
  the same checksum verification applies. Plain go build/go install omit the required schema value,
  causing profile generation to fail with "installed profiler has no verified core profile schema".
  Local builds use go run ./scripts/build.go --output ./rc, with optional version/commit flags.
  Verify rc version, rc --version, and rc version --output yaml report the release tag and source commit.
  Reject dev/unknown metadata in release artifacts; ordinary local builds retain those defaults.
  The helper builds the executable; staging the matching YAML resources remains a separate
  build step, as implemented in CI. A complete package includes both bin/rc and share/rc.
- Build once from pinned modules and verified resources; inventory the resulting CLI and package without rebuilding.
- Produce platform tar.gz archives, SHA256SUMS, component YAML, and .deb packages using pinned nFPM.
  Map Go amd64/arm64 to the corresponding Debian architecture fields and include package descriptions/licenses.
- Verify staged files against the inventory and verify linked module versions/checksums in build metadata.
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
| 2. Go default | Pinned linked profiler, native Cobra commands, identical default/explicit Go results and verified core schema. |
| 3. Add-on contract | Strict YAML schema, configuration/discovery, compatibility errors, plugin list and doctor. |
| 4. Binding commands | Implemented: pinned libraries, direct typed execution, staged YAML resources, actual identities, and all eight Cobra operations. |
| 5. Packaging | Relocatable archives, Homebrew formula, .deb installs/upgrades/removals on the stated matrix. |
| 6. Workflow migration | Extensions helpers may use a pinned common CLI after parity; standalone generation remains supported. |
| 7. Publication | Protected release, tap/APT promotion, documented installation, checksums and rollback procedure. |

Add focused tests for Cobra values and optional backend argv boundaries, default Go aliases,
native/optional help, error/exit propagation, cancellation, duplicate IDs, missing/incompatible tools,
explicit override selection, and stdout byte preservation using controlled subprocess fixtures.
Compare generated Go profiles and negative-case behavior with the linked profiler API.
Compare binding YAML, target defaults, lock failures, cleanup, atomic updates, and output paths with the old CLI.
The CI binding smoke test relocates the complete staged prefix and copies the neutral YAML
fixture into an isolated project. Only Go is available on its subprocess PATH. All eight
binding operations run, with initial release planning using a local read-only GitHub API
fixture. A generated module is installed through a private file proxy and then profiled by
both native Go command routes. Altered-source checks, nonempty generation destinations,
and invalid declarations must fail without replacing existing output. Generated package
trees, archives, profiles, and caches remain temporary build outputs.
Install in clean VMs/containers from complete artifacts, away from checkout paths and without a profiler on PATH.
Run a Go workload using rc-cli alone plus the documented Go toolchain and resolved workload bindings.
Check that no Python/JVM is required; optional-language errors identify the add-on/runtime to install.
Verify upgrade/removal ownership, Homebrew prefix handling, APT metadata/signatures, and retained component hashes.
A release is ready only after all eight binding operations and linked Go profiling pass installed-package
acceptance, and both distribution channels install the same reviewed component versions.

## References

- [Cobra command/flag behavior](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md) and [DisableFlagParsing](https://github.com/spf13/cobra/blob/main/command.go).
- [Homebrew formula packaging](https://docs.brew.sh/Formula-Cookbook) and [nFPM configuration](https://nfpm.goreleaser.com/docs/configuration/).
- [APT authentication](https://manpages.debian.org/testing/apt/apt-secure.8.en.html) and [repository key scoping](https://manpages.debian.org/testing/apt/sources.list.5.en.html).
