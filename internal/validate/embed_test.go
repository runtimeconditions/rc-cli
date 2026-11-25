package validate

import (
	"os"
	"strings"
	"testing"
)

// Both ways of building rc embed the core schema into the linked Go profiler: scripts/build.go
// for local and CI builds, scripts/release-package.py for the release archives and packages.
// rc's validator reads that copy through go-rc-profiler/extensioncheck.InstalledCoreSchema, so
// neither build script should embed a second copy in internal/validate.
func TestEveryBuildEmbedsOnlyTheProfilerCoreSchema(t *testing.T) {
	const profilerVar = "github.com/runtimeconditions/go-rc-profiler/extensioncheck.installedCoreSchemaBase64"
	const validatorVar = "github.com/runtimeconditions/rc-cli/internal/validate.coreSchemaBase64"
	for _, script := range []string{"../../scripts/build.go", "../../scripts/release-package.py"} {
		data, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		if !strings.Contains(content, profilerVar) {
			t.Errorf("%s does not set %s, so a build made with it cannot validate profiles", script, profilerVar)
		}
		if strings.Contains(content, validatorVar) {
			t.Errorf("%s still sets obsolete %s; only the profiler variable should be embedded", script, validatorVar)
		}
	}
}
