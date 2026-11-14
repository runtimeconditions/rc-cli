package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	commonShort = "https://runtimeconditions.io/extensions/rc/common-integrations:v1alpha2"
	envShort    = "https://runtimeconditions.io/extensions/rc/env-configuration:v1alpha2"
	validHTTP   = `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsProfile
metadata: {name: orders}
workload: {uri: "https://example.test/orders"}
extensions:
  - ` + commonShort + `
  - ` + envShort + `
conditions:
  - kind: cache
    interface: {type: key_value, engine: redis}
    configuration:
      env:
        - {property: url, name: REDIS_URL}
`
)

var fixtureRoot = filepath.Join("..", "..", "testdata", "profile-validate", "extensions")

// definitionServer serves the committed extension definitions for https://runtimeconditions.io/extensions/.
type definitionServer struct{ requests []string }

func (s *definitionServer) RoundTrip(request *http.Request) (*http.Response, error) {
	s.requests = append(s.requests, request.URL.String())
	status, body := http.StatusNotFound, "not found"
	const prefix = "https://runtimeconditions.io/extensions/"
	if address := request.URL.String(); strings.HasPrefix(address, prefix) {
		if data, err := os.ReadFile(filepath.Join(fixtureRoot, filepath.FromSlash(strings.TrimPrefix(address, prefix)))); err == nil {
			status, body = http.StatusOK, string(data)
		}
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

type refusingTransport struct{}

func (refusingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("the network must not be used")
}

func runProfileValidate(t *testing.T, transport http.RoundTripper, profile string, args ...string) (string, error) {
	t.Helper()
	api := linkedProfileValidateAPI()
	api.Client = &http.Client{Transport: transport}
	command := newProfileValidateCommand(api)
	var stderr bytes.Buffer
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetIn(strings.NewReader(profile))
	command.SetArgs(append([]string{"-"}, args...))
	err := command.Execute()
	return stderr.String(), err
}

func TestValidateAcceptsAProfileAndDownloadsItsExtensionsAndDependencies(t *testing.T) {
	server := &definitionServer{}
	stderr, err := runProfileValidate(t, server, validHTTP)
	if err != nil {
		t.Fatalf("a valid profile was rejected: %v", err)
	}
	if !strings.Contains(stderr, "profile validated") {
		t.Fatalf("missing success message: %q", stderr)
	}
	if len(server.requests) < 2 {
		t.Fatalf("expected both extensions to be downloaded, got %v", server.requests)
	}
}

func TestValidateRejectsAValueTheExtensionDoesNotAllow(t *testing.T) {
	_, err := runProfileValidate(t, &definitionServer{}, strings.Replace(validHTTP, "engine: redis", "engine: oracle", 1))
	if err == nil || !strings.Contains(err.Error(), "oracle") {
		t.Fatalf("expected the engine to be rejected, got %v", err)
	}
}

func TestValidateRejectsConfigurationWhenItsExtensionIsNotDeclared(t *testing.T) {
	withoutEnv := strings.Replace(validHTTP, "  - "+envShort+"\n", "", 1)
	_, err := runProfileValidate(t, &definitionServer{}, withoutEnv)
	if err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("expected configuration to be rejected without env-configuration, got %v", err)
	}
}

func TestValidateSaysHowToProceedWhenADefinitionCannotBeDownloaded(t *testing.T) {
	profile := strings.Replace(validHTTP, "common-integrations:v1alpha2", "missing:v9", 1)
	_, err := runProfileValidate(t, &definitionServer{}, profile)
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "--catalog") {
		t.Fatalf("expected a download error that mentions --catalog, got %v", err)
	}
}

func TestValidateDownloadsOnlyOverHTTPS(t *testing.T) {
	profile := strings.Replace(validHTTP, "https://runtimeconditions.io/extensions/rc/common-integrations:v1alpha2", "http://runtimeconditions.io/extensions/rc/common-integrations:v1alpha2", 1)
	server := &definitionServer{}
	_, err := runProfileValidate(t, server, profile)
	if err == nil || !strings.Contains(err.Error(), "only https") {
		t.Fatalf("expected a plain http id to be refused, got %v", err)
	}
	for _, request := range server.requests {
		if strings.HasPrefix(request, "http://") {
			t.Fatalf("a plain http request was made: %s", request)
		}
	}
}

func TestValidateReadsLocalDefinitionsWithoutTheNetworkWhenACatalogIsGiven(t *testing.T) {
	stderr, err := runProfileValidate(t, refusingTransport{}, validHTTP, "--catalog", fixtureRoot)
	if err != nil {
		t.Fatalf("a valid profile was rejected with a local catalog: %v", err)
	}
	if !strings.Contains(stderr, "profile validated") {
		t.Fatalf("missing success message: %q", stderr)
	}
}

func TestValidateReadsTheProfileFromAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(path, []byte(validHTTP), 0o600); err != nil {
		t.Fatal(err)
	}
	api := linkedProfileValidateAPI()
	api.Client = &http.Client{Transport: refusingTransport{}}
	command := newProfileValidateCommand(api)
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{path, "--catalog", fixtureRoot})
	if err := command.Execute(); err != nil {
		t.Fatalf("a valid profile file was rejected: %v", err)
	}
}

func TestValidateIsPartOfTheProfileCommand(t *testing.T) {
	root := NewRootCommandWith(BuildInfo{}, IOStreams{}.normalized(), DefaultDependencies())
	command, _, err := root.Find([]string{"profile", "validate"})
	if err != nil || command.Name() != "validate" {
		t.Fatalf("rc profile validate is not registered: %v %v", command, err)
	}
}
