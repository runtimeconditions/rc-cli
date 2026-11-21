# Generating a profile

`rc profile generate` and its prerequisites, in detail. For the one-step quickstart see the
[README](../README.md#quickstart). 

This page covers Go, the profiler built into `rc`; TypeScript, Java, and Python workloads use add-on profilers instead. See
[Language profilers](language-profilers.md).

`rc profile generate` reads the Go binding declarations in a workload and writes a validated
profile. Profiling analyzes declarations without executing the workload. Resolve the
workload's Go binding dependencies before running these commands. Three prerequisites:

1. A Go toolchain on `PATH`. Without one, generation fails with
   `exec: "go": executable file not found in $PATH`. `rc` does not download Go for you.
2. `--dir` must point at a directory containing a `go.mod`. Current releases fail fast:

   ```text
   rc: /path/to/dir: workload must have a go.mod: stat /path/to/dir/go.mod: no such file or directory
   ```

   Always point `--dir` at a Go module.
3. The workload must import published RC binding modules (`runtimeconditions.io/x/rc/...`).

## Example workload

`go.mod`:

```go
module example.com/my-service

go 1.25.0

require (
	runtimeconditions.io/x/rc/common v0.3.0
	runtimeconditions.io/x/rc/env v0.3.0
)
```

`main.go` — declare conditions with the published binding packages:

```go
package main

import (
	"runtimeconditions.io/x/rc/common"
	"runtimeconditions.io/x/rc/env"
)

var todosAPI = common.Api(
	common.Http{
		Spec: &common.Spec{Format: common.FormatOpenapi, Uri: "catalog://api/default/todos-api"},
		Operations: common.HttpOperations{
			{Method: common.MethodGet, Path: "/todos/{id}",
				ResponseSchema: common.ResponseSchemaMap{"id": common.SimpleSchemaStringInteger}},
		},
	},
	env.HttpConfiguration{
		Env: env.HttpConfigurationEnv{
			{Name: "TODOS_API_URL", Property: env.HttpConfigurationEnvPropertyBaseUrl},
		},
	},
)

var requestCache = common.Cache(
	common.KeyValue{Engine: &[]common.KeyValueEngine{common.KeyValueEngineRedis}[0]},
	env.KeyValueConfiguration{
		Env: env.KeyValueConfigurationEnv{
			{Name: "REDIS_URL", Property: env.KeyValueConfigurationEnvPropertyUrl},
		},
	},
)

func main() {}
```

Resolve dependencies, then generate:

```sh
go mod tidy
rc profile generate --dir ./my-service --out profile.yaml
```

Defaults: `--dir` is the current directory, `--out` is stdout, `--name` is the directory's
basename, `--workload-uri` is the enclosing Go module path, `--workload-version` is `dev`.
A failed generation leaves an existing `--out` file unchanged. You can set them explicitly:

```sh
rc profile generate \
  --dir ./my-service \
  --name my-service \
  --workload-uri https://example.com/my-service \
  --workload-version 1.0.0 \
  --out profile.yaml
```

The published-module pattern above is the supported direction; treat that error as a
known gap in the current published `env` release, not as a mistake in your workload.

## Checking a workload's bindings

```sh
rc profile validate-extensions --dir ./my-service
rc profile validate-extension --dir ./my-service --package runtimeconditions.io/x/rc/common
rc profile go version    # linked Go profiler version, e.g. "go-rc-profiler v0.8.0 (linked into rc)"
```
