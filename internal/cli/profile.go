package cli

import "github.com/spf13/cobra"

type goProfilerIdentity struct {
	Name      string `yaml:"name"`
	Path      string `yaml:"path,omitempty"`
	Origin    string `yaml:"origin"`
	Version   string `yaml:"version,omitempty"`
	Commit    string `yaml:"commit,omitempty"`
	SHA256    string `yaml:"sha256,omitempty"`
	Module    string `yaml:"module,omitempty"`
	ModuleSum string `yaml:"moduleSum,omitempty"`
}

func newProfileCommand() *cobra.Command {
	return newGoProfileCommands(linkedGoProfilerAPI())
}

func newGoProfileCommands(api goProfilerAPI) *cobra.Command {
	profile := &cobra.Command{
		Use: "profile", Short: "Generate profiles with the built-in Go profiler",
		Long:    "Generate profiles with the Go profiler linked into rc. Go analysis requires a working Go toolchain.",
		Example: "  rc profile generate --dir ./service\n  rc profile go generate --dir ./service",
	}
	goCommand := &cobra.Command{Use: "go", Short: "Inspect Go bindings and generate profiles"}
	for _, parent := range []*cobra.Command{profile, goCommand} {
		parent.Args = usageArgs(cobra.NoArgs)
		parent.ValidArgsFunction = cobra.NoFileCompletions
		parent.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
		parent.Version = linkedGoProfilerIdentity().Version
		parent.SetVersionTemplate("{{printf \"go-rc-profiler %s (linked into rc)\\n\" .Version}}")
		parent.AddCommand(newGoGenerateCommand(api), newGoValidateCommand(api, true),
			newGoValidateCommand(api, false), newGoVersionCommand())
	}
	profile.AddCommand(goCommand)
	return profile
}

func goProfilerComponents() ([]goProfilerIdentity, error) {
	return []goProfilerIdentity{linkedGoProfilerIdentity()}, nil
}
