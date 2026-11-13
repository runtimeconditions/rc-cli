package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/runtimeconditions/rc-cli/internal/plugins"
	"github.com/runtimeconditions/rc-cli/internal/process"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// coreSchemaVersion is the core profile schema this rc is built against. It is the
// version pinned in distribution/components.lock.yaml and scripts/build.go, and an
// add-on must report it in `--version --json` before rc runs the add-on.
const coreSchemaVersion = "0.4.0"

// Dependencies are the process-level services optional language commands use.
type Dependencies struct {
	Finder plugins.Finder
	Runner process.Runner
}

// DefaultDependencies looks add-ons up on the real PATH and runs real subprocesses.
func DefaultDependencies() Dependencies {
	return Dependencies{
		Finder: plugins.Finder{LookPath: exec.LookPath, Getenv: os.Getenv},
		Runner: process.OS{},
	}
}

// processExit carries an add-on's nonzero exit status out as the exit status of rc.
// The add-on has already printed its own diagnostics, so rc prints nothing more.
type processExit struct{ code int }

func (e processExit) Error() string { return fmt.Sprintf("add-on exited with status %d", e.code) }

func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// newLanguageCommand runs an installed add-on: `rc profile <language> <its own arguments>`.
// Everything after the language word reaches the add-on unchanged, so its own command
// tree, flags, repeated values, and `--` keep their meaning.
func newLanguageCommand(language plugins.Language, deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   language.ID + " [command] [flags]",
		Short: "Generate profiles with the installed " + language.Display + " profiler",
		Long: fmt.Sprintf("Run the installed %s profiler. The arguments after %q go to it unchanged, so its own help applies: rc profile %s --help.\n"+
			"It must be installed separately (%s) and report core profile schema %s.",
			language.Display, language.ID, language.ID, language.InstallHint, coreSchemaVersion),
		Example:            fmt.Sprintf("  rc profile %s generate --project ./service --name orders --workload-uri https://example.com/orders", language.ID),
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		ValidArgsFunction:  cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := commandContext(cmd)
			found, err := deps.Finder.Find(language)
			if err != nil {
				return err
			}
			info, err := plugins.Describe(ctx, deps.Runner, found.Path)
			if err != nil {
				return fmt.Errorf("the %s profiler at %s: %w", language.Display, found.Path, err)
			}
			if !info.Supports(coreSchemaVersion) {
				return fmt.Errorf("the %s profiler %s at %s supports core profile schema %s, but this rc targets %s; install a matching version (%s)",
					language.Display, info.Version, found.Path, strings.Join(info.CoreSchemas, ", "), coreSchemaVersion, language.InstallHint)
			}
			code, err := deps.Runner.Run(ctx, process.Spec{
				Path: found.Path, Args: args,
				Stdin: cmd.InOrStdin(), Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			if code != 0 {
				return processExit{code}
			}
			return nil
		},
	}
}

type pluginRow struct {
	Language    string   `yaml:"language"`
	Status      string   `yaml:"status"`
	Origin      string   `yaml:"origin,omitempty"`
	Path        string   `yaml:"path,omitempty"`
	Version     string   `yaml:"version,omitempty"`
	CoreSchemas []string `yaml:"coreSchemas,omitempty"`
	Detail      string   `yaml:"detail,omitempty"`
}

func inspectLanguage(ctx context.Context, language plugins.Language, deps Dependencies) pluginRow {
	row := pluginRow{Language: language.ID}
	found, err := deps.Finder.Find(language)
	var missing plugins.NotInstalledError
	var runtime plugins.RuntimeMissingError
	switch {
	case errors.As(err, &missing):
		row.Status, row.Detail = "missing", "install with: "+language.InstallHint
		return row
	case errors.As(err, &runtime):
		row.Status, row.Detail = "runtime-missing", err.Error()
		return row
	case err != nil:
		row.Status, row.Detail = "unusable", err.Error()
		return row
	}
	row.Origin, row.Path = found.Origin, found.Path
	info, err := plugins.Describe(ctx, deps.Runner, found.Path)
	if err != nil {
		row.Status, row.Detail = "unreadable", err.Error()
		return row
	}
	row.Version, row.CoreSchemas = info.Version, info.CoreSchemas
	if info.Supports(coreSchemaVersion) {
		row.Status = "ready"
	} else {
		row.Status, row.Detail = "incompatible", "this rc targets core profile schema "+coreSchemaVersion
	}
	return row
}

// newPluginsCommand lists the profilers rc can use. It reads metadata only: it never
// runs a profiler on a workload.
func newPluginsCommand(deps Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use: "plugins", Short: "Inspect optional language profilers",
		Args: usageArgs(cobra.NoArgs), ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	var output string
	list := &cobra.Command{
		Use: "list", Short: "List the Go profiler built into rc and the installed language add-ons",
		Args: usageArgs(cobra.NoArgs), ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows := []pluginRow{{
				Language: "go", Status: "built-in", Origin: "linked",
				Version: linkedGoProfilerIdentity().Version, CoreSchemas: []string{coreSchemaVersion},
			}}
			for _, language := range plugins.Languages() {
				rows = append(rows, inspectLanguage(commandContext(cmd), language, deps))
			}
			switch output {
			case "text":
				table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(table, "LANGUAGE\tSTATUS\tVERSION\tPATH")
				for _, row := range rows {
					fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", row.Language, row.Status, row.Version, row.Path)
				}
				return table.Flush()
			case "yaml":
				encoder := yaml.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent(2)
				if err := encoder.Encode(struct {
					Plugins []pluginRow `yaml:"plugins"`
				}{rows}); err != nil {
					return err
				}
				return encoder.Close()
			default:
				return usageError{fmt.Errorf("unsupported output format %q; use text or yaml", output)}
			}
		},
	}
	list.Flags().StringVar(&output, "output", "text", "Output format: text or yaml")
	cmd.AddCommand(list)
	return cmd
}
