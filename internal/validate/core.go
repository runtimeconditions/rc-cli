package validate

import (
	"errors"
	"fmt"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

var coreSchemaOverride []byte

// UseCoreSchema makes the validator use the given core profile schema instead of the one the build
// embedded. It is meant for tests and for tools that bring their own schema.
func UseCoreSchema(schema []byte) { coreSchemaOverride = schema }

var errNoCoreSchema = errors.New("this build of rc has no verified core profile schema")

// loadCoreSchema compiles the core profile schema.
func loadCoreSchema() (*jsonschema.Schema, error) {
	data := coreSchemaOverride
	if len(data) == 0 {
		schema, err := extensioncheck.InstalledCoreSchema()
		if errors.Is(err, extensioncheck.ErrNoInstalledCoreSchema) {
			return nil, errNoCoreSchema
		}
		if err != nil {
			return nil, err
		}
		data = schema
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("read the core profile schema: %w", err)
	}
	if document["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		return nil, errors.New("the core profile schema must use JSON Schema Draft 2020-12")
	}
	identifier, _ := document["$id"].(string)
	if identifier == "" {
		return nil, errors.New("the core profile schema has no $id")
	}
	parsed, err := jsonValue(document)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(identifier, parsed); err != nil {
		return nil, err
	}
	return compiler.Compile(identifier)
}
