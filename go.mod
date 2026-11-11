module github.com/runtimeconditions/rc-cli

go 1.25.0

toolchain go1.26.5

require (
	github.com/runtimeconditions/extensions/tooling/extension-bindings/emitters/go v0.0.0-20261111072413-dd96ddaebb0d
	github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer v0.0.0-20261111072413-dd96ddaebb0d
	github.com/runtimeconditions/extensions/tooling/extension-bindings/orchestrator v0.0.0-20261111072413-dd96ddaebb0d
	github.com/runtimeconditions/go-rc-profiler v0.6.0
	github.com/spf13/cobra v1.10.2
	go.yaml.in/yaml/v3 v3.0.4
	golang.org/x/mod v0.28.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/tools v0.37.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Upstream workspace manifests require these unpublished placeholders.
// Exclude them so graph checks use the exact source pins above.
exclude (
	github.com/runtimeconditions/extensions/tooling/extension-bindings/emitters/go v0.0.0
	github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer v0.0.0
)
