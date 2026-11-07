// Package cli constructs the Runtime Conditions command tree without process-global state.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// BuildInfo identifies the CLI build independently of its installed components.
type BuildInfo struct {
	Version string `yaml:"version"`
	Commit  string `yaml:"commit"`
}

// IOStreams gives each invocation its own input and output destinations.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

func (s IOStreams) normalized() IOStreams {
	if s.In == nil {
		s.In = strings.NewReader("")
	}
	if s.Out == nil {
		s.Out = io.Discard
	}
	if s.ErrOut == nil {
		s.ErrOut = io.Discard
	}
	return s
}

type usageError struct{ error }

// Execute reports errors once and returns the process exit code without calling os.Exit.
func Execute(ctx context.Context, args []string, build BuildInfo, streams IOStreams) int {
	streams = streams.normalized()
	root := NewRootCommand(build, streams)
	// A non-nil empty slice prevents Cobra from falling back to os.Args.
	root.SetArgs(append([]string{}, args...))
	err := ctx.Err()
	var command *cobra.Command
	if err == nil {
		command, err = root.ExecuteContextC(ctx)
	}
	if err == nil {
		return 0
	}
	var backendExit profilerExitError
	if errors.As(err, &backendExit) {
		return int(backendExit)
	}
	fmt.Fprintf(streams.ErrOut, "rc: %v\n", err)
	if errors.Is(err, context.Canceled) {
		return 130
	}
	var usage usageError
	if errors.As(err, &usage) {
		if command != nil {
			fmt.Fprint(streams.ErrOut, command.UsageString())
		}
		return 2
	}
	return 1
}

// NewRootCommand returns a fresh command tree. Callers may attach feature commands before execution.
func NewRootCommand(build BuildInfo, streams IOStreams) *cobra.Command {
	streams = streams.normalized()
	if build.Version == "" {
		build.Version = "dev"
	}
	if build.Commit == "" {
		build.Commit = "unknown"
	}
	root := &cobra.Command{
		Use:               "rc",
		Short:             "Runtime Conditions CLI",
		Version:           build.Version,
		Annotations:       map[string]string{"commit": build.Commit},
		Args:              usageArgs(cobra.NoArgs),
		ValidArgsFunction: cobra.NoFileCompletions,
		SilenceErrors:     true,
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE:              func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.ErrOut)
	root.SetVersionTemplate("{{printf \"%s %s (%s)\\n\" .Name .Version (index .Annotations \"commit\")}}")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.AddCommand(newVersionCommand(build), newCompletionCommand(), newProfileCommand())
	root.InitDefaultHelpCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "help" {
			// Find can return the nearest command with unrecognized arguments left over.
			cmd.Args = usageArgs(func(_ *cobra.Command, args []string) error {
				_, remaining, err := root.Find(args)
				if err != nil {
					return err
				}
				if len(remaining) != 0 {
					return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
				}
				return nil
			})
		}
	}
	return root
}

func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

func newVersionCommand(build BuildInfo) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:               "version",
		Short:             "Show CLI build information",
		Args:              usageArgs(cobra.NoArgs),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch output {
			case "text":
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "rc %s (%s)\n", build.Version, build.Commit)
				return err
			case "yaml":
				components, err := goProfilerComponents()
				if err != nil {
					return err
				}
				document := struct {
					CLI        BuildInfo            `yaml:"cli"`
					GoVersion  string               `yaml:"goVersion"`
					Platform   string               `yaml:"platform"`
					Components []goProfilerIdentity `yaml:"components"`
				}{build, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH, components}
				encoder := yaml.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent(2)
				if err := encoder.Encode(document); err != nil {
					return err
				}
				return encoder.Close()
			default:
				return usageError{fmt.Errorf("unsupported output format %q; use text or yaml", output)}
			}
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "Output format: text or yaml")
	return cmd
}

func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:       "completion <bash|zsh|fish|powershell>",
		Short:     "Generate a shell completion script",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      usageArgs(cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs)),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(out, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(out)
			case "fish":
				return cmd.Root().GenFishCompletion(out, true)
			default: // OnlyValidArgs has already restricted this to powershell.
				return cmd.Root().GenPowerShellCompletionWithDesc(out)
			}
		},
	}
}
