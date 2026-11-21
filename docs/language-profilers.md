# Optional language profilers

Go profiling is built into `rc`. Other languages run a separate add-on that `rc` finds on
`PATH`. It can come from the language's own tooling or from a system package, and `rc` does
not care which:

- **TypeScript:** `brew install runtimeconditions/tools/rc-profiler-typescript`,
  `apt install rc-profiler-typescript` (Debian/Ubuntu), or `dnf`/`yum install rc-profiler-typescript`
  (Fedora/RHEL). apt/dnf/yum need the [package source setup](../README.md#install) first.
- **Java:** `brew install runtimeconditions/tools/rc-profiler-java`,
  `apt install rc-profiler-java`, or `dnf`/`yum install rc-profiler-java`.
- **Python:** `pip install runtimeconditions-profiler` into the workload's own virtual
  environment (it finds binding packages through that environment's metadata) and put the
  environment's `bin` directory on `PATH`. Do not use `pipx`, which would give it an
  environment of its own.

| Command | Purpose |
| --- | --- |
| `rc profile typescript <command> [flags]` | Run the installed TypeScript profiler. |
| `rc profile java <command> [flags]` | Run the installed Java profiler. |
| `rc profile python <command> [flags]` | Run the installed Python profiler. |
| `rc plugins list [--output yaml]` | Show the Go profiler and each add-on's status, version, and path. |

Everything after `typescript`, `java`, or `python` goes to the add-on unchanged, so it keeps
its own flags (for example `--project` instead of `--dir`, and `--classpath` for Java). `rc`
looks for `rc-profiler-typescript`, then `runtimeconditions-typescript-profiler`, or
`rc-profiler-java`, or `runtimeconditions-python-profiler`, on `PATH`;
`RC_TYPESCRIPT_PROFILER`, `RC_JAVA_PROFILER`, or `RC_PYTHON_PROFILER` can name an absolute
path instead. It runs the add-on only if `--version --json` lists the core profile schema
`rc` targets (0.4.0). The add-on's output and exit status pass through.

The Java launcher chooses the JDK, not `rc`: `RC_JAVA_HOME`, then `JAVA_HOME`, then the
runtime bundled in its system package, then `java` on `PATH`. It needs JDK 17 or newer and
says so when none is found.

The Python profiler runs on the interpreter of the environment it is installed in (Python
3.11 or newer), and sees the binding packages installed there.
