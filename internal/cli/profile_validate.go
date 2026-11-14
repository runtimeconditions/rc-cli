package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

const (
	extensionDefinitionFile = "runtimeconditions.extension.yaml"
	maxDefinitionBytes      = 4 << 20
	maxDefinitions          = 64
)

// profileValidateAPI holds what the command needs from outside the process, so tests can replace it.
type profileValidateAPI struct {
	Validate func(profile []byte, catalogRoots []string) error
	Client   *http.Client
}

func linkedProfileValidateAPI() profileValidateAPI {
	return profileValidateAPI{
		Validate: func(profile []byte, catalogRoots []string) error {
			return extensioncheck.ValidateProfileYAML(profile, extensioncheck.ProfileOptions{CatalogRoots: catalogRoots})
		},
		Client: &http.Client{Timeout: 20 * time.Second},
	}
}

func newProfileValidateCommand(api profileValidateAPI) *cobra.Command {
	var catalogs []string
	cmd := &cobra.Command{
		Use:   "validate <profile.yaml>",
		Short: "Validate a hand-written profile against its extensions",
		Long: "Validate a Runtime Conditions Profile against the extensions it declares, whoever wrote it.\n\n" +
			"By default the extension definitions are downloaded from the https ids the profile names, " +
			"including their dependencies. With --catalog the definitions are read from local directories " +
			"instead and nothing is downloaded. Use - to read the profile from standard input.\n\n" +
			"This checks the profile against the extension vocabulary and schemas. It does not check the " +
			"core profile fields (apiVersion, metadata, workload) against the core schema.",
		Example:           "  rc profile validate profile.yaml\n  rc profile validate profile.yaml --catalog ./extensions",
		Args:              usageArgs(cobra.ExactArgs(1)),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			return validateProfileFile(cmd.Context(), api, args[0], catalogs, cmd.InOrStdin(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringArrayVar(&catalogs, "catalog", nil, "directory of extension definitions to use instead of downloading them (repeatable)")
	return cmd
}

func validateProfileFile(ctx context.Context, api profileValidateAPI, path string, catalogs []string, stdin io.Reader, stderr io.Writer) error {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}
	roots := catalogs
	if len(roots) == 0 {
		directory, err := os.MkdirTemp("", "rc-extensions-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(directory)
		if err := downloadDefinitions(ctx, api.Client, data, directory); err != nil {
			return err
		}
		roots = []string{directory}
	}
	if err := api.Validate(data, roots); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stderr, "runtimeconditions: profile validated")
	return err
}

type extensionRef struct{ ID, Version string }

// referencesOf reads a list of extension references written as a string
// ("<id>" or "<id>:<version>") or as an id/version object.
func referencesOf(nodes []yaml.Node) ([]extensionRef, error) {
	var refs []extensionRef
	for _, node := range nodes {
		switch node.Kind {
		case yaml.ScalarNode:
			if node.Value == "" {
				return nil, errors.New("extension reference must not be empty")
			}
			id, version := node.Value, ""
			colon, scheme := strings.LastIndex(id, ":"), strings.Index(id, "://")
			if colon > 0 && colon > scheme+2 && colon > strings.LastIndex(id, "/") {
				id, version = node.Value[:colon], node.Value[colon+1:]
			}
			refs = append(refs, extensionRef{id, version})
		case yaml.MappingNode:
			var fields struct{ ID, Version string }
			if err := node.Decode(&fields); err != nil {
				return nil, err
			}
			refs = append(refs, extensionRef{fields.ID, fields.Version})
		default:
			return nil, errors.New("extension reference must be a string or an id/version object")
		}
	}
	return refs, nil
}

func (r extensionRef) candidateURLs() []string {
	if r.Version == "" {
		return []string{r.ID}
	}
	return []string{strings.TrimRight(r.ID, "/") + "/" + r.Version + "/" + extensionDefinitionFile, r.ID}
}

// downloadDefinitions fetches the definitions of the extensions a profile declares, and of their
// dependencies, into directory so the catalog loader can read them.
func downloadDefinitions(ctx context.Context, client *http.Client, profile []byte, directory string) error {
	var document struct {
		Extensions []yaml.Node `yaml:"extensions"`
	}
	if err := yaml.Unmarshal(profile, &document); err != nil {
		return fmt.Errorf("read profile: %w", err)
	}
	queue, err := referencesOf(document.Extensions)
	if err != nil {
		return fmt.Errorf("profile extensions: %w", err)
	}
	fetched := map[string]bool{}
	count := 0
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		address, body, err := fetchDefinition(ctx, client, ref)
		if err != nil {
			return err
		}
		// The same release can be named by a short reference and by its full id.
		if fetched[address] {
			continue
		}
		fetched[address] = true
		if count++; count > maxDefinitions {
			return fmt.Errorf("the profile needs more than %d extension definitions", maxDefinitions)
		}
		subdirectory := filepath.Join(directory, strconv.Itoa(count))
		if err := os.MkdirAll(subdirectory, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(subdirectory, extensionDefinitionFile), body, 0o600); err != nil {
			return err
		}
		var definition struct {
			Spec struct {
				Dependencies []yaml.Node `yaml:"dependencies"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal(body, &definition); err != nil {
			return fmt.Errorf("read the definition of %s: %w", ref.ID, err)
		}
		dependencies, err := referencesOf(definition.Spec.Dependencies)
		if err != nil {
			return fmt.Errorf("dependencies of %s: %w", ref.ID, err)
		}
		queue = append(queue, dependencies...)
	}
	return nil
}

func fetchDefinition(ctx context.Context, client *http.Client, ref extensionRef) (string, []byte, error) {
	var failures []string
	for _, candidate := range ref.candidateURLs() {
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			failures = append(failures, candidate+": only https extension ids can be downloaded")
			continue
		}
		body, err := get(ctx, client, candidate)
		if err == nil {
			return candidate, body, nil
		}
		failures = append(failures, err.Error())
	}
	return "", nil, fmt.Errorf("cannot get the definition of extension %s (%s); use --catalog to read definitions from a directory", ref.ID, strings.Join(failures, "; "))
}

func get(ctx context.Context, client *http.Client, address string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", address, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDefinitionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	if len(body) > maxDefinitionBytes {
		return nil, fmt.Errorf("%s: the definition is larger than %d bytes", address, maxDefinitionBytes)
	}
	return body, nil
}
