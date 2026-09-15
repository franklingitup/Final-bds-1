package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordedUpdate struct {
	Step   int
	Update UpdateStepRequest
}

type installerTestServer struct {
	server  *httptest.Server
	mu      sync.Mutex
	updates []recordedUpdate
}

func newInstallerTestServer(t *testing.T, bundle SessionBundle) *installerTestServer {
	t.Helper()
	h := &installerTestServer{}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/token/bundle":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(bundle)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bootstrap/bootstrap/manifest.yaml":
			w.Header().Set("Content-Type", "application/x-yaml")
			_, _ = io.WriteString(w, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: bds-platform\n")
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/sessions/token/steps/"):
			var step int
			if _, err := fmtSscanfStep(r.URL.Path, &step); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var update UpdateStepRequest
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			h.mu.Lock()
			h.updates = append(h.updates, recordedUpdate{Step: step, Update: update})
			h.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.server.Close)
	return h
}

func fmtSscanfStep(path string, step *int) (int, error) {
	return fmt.Sscanf(path, "/v1/sessions/token/steps/%d", step)
}

func (h *installerTestServer) client() *Client {
	return NewClient(h.server.URL, 0)
}

func (h *installerTestServer) snapshot() []recordedUpdate {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]recordedUpdate(nil), h.updates...)
}

type fakeRunner struct {
	commands []string
	failAt   string
}

func (r *fakeRunner) Run(_ context.Context, _ string, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	command := filepath.Base(name) + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	if strings.Contains(command, "output -raw kubeconfig_command") {
		_, _ = io.WriteString(stdout, "aws eks update-kubeconfig --region us-east-1 --name demo")
	} else {
		_, _ = io.WriteString(stdout, "output for "+command)
	}
	if r.failAt != "" && strings.HasPrefix(command, r.failAt) {
		_, _ = io.WriteString(stderr, "\napply error")
		return errors.New("exit status 1")
	}
	return nil
}

func testBundle() SessionBundle {
	return SessionBundle{
		SessionID:       "session-1",
		Provider:        "aws",
		TerraformConfig: "terraform {}\nvariable \"cluster_name\" {}\noutput \"name\" { value = var.cluster_name }\n",
		TerraformVars:   json.RawMessage(`{"cluster_name":"demo"}`),
		SessionToken:    "token",
		BootstrapToken:  "bootstrap",
	}
}

func allToolsPresent(name string) (string, error) {
	if name == "terraform" {
		return "", errors.New("not found")
	}
	return filepath.Join("bin", name), nil
}

func TestRunTerraformRunsAndReportsStepsOneThroughThree(t *testing.T) {
	server := newInstallerTestServer(t, testBundle())
	runner := &fakeRunner{}
	installer := NewInstaller(server.client())
	installer.Runner = runner
	installer.LookPath = allToolsPresent
	installer.WorkingRoot = t.TempDir()
	installer.Stdout = io.Discard
	installer.Stderr = io.Discard

	if err := installer.RunTerraform(context.Background(), "token"); err != nil {
		t.Fatalf("RunTerraform: %v", err)
	}
	wantCommands := []string{
		"tofu init -input=false",
		"tofu plan -input=false -out=tfplan",
		"tofu apply -input=false -auto-approve tfplan",
	}
	if !reflect.DeepEqual(runner.commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, wantCommands)
	}

	updates := server.snapshot()
	if len(updates) != 6 {
		t.Fatalf("step updates = %d, want running+completed for 3 steps", len(updates))
	}
	for index, step := range []int{1, 2, 3} {
		running := updates[index*2]
		completed := updates[index*2+1]
		if running.Step != step || running.Update.Status != "running" {
			t.Fatalf("step %d running update = %+v", step, running)
		}
		if completed.Step != step || completed.Update.Status != "completed" || completed.Update.Output == nil {
			t.Fatalf("step %d completion update = %+v", step, completed)
		}
	}

	dir := filepath.Join(installer.WorkingRoot, "session-1")
	for _, name := range []string{"main.tf", "variables.tf", "outputs.tf", "terraform.tfvars.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s was not written: %v", name, err)
		}
	}
	vars, _ := os.ReadFile(filepath.Join(dir, "terraform.tfvars.json"))
	if string(vars) != `{"cluster_name":"demo"}` {
		t.Fatalf("tfvars = %s", vars)
	}
}

func TestRunTerraformFailedApplyReportsFailureAndStops(t *testing.T) {
	server := newInstallerTestServer(t, testBundle())
	runner := &fakeRunner{failAt: "tofu apply"}
	installer := NewInstaller(server.client())
	installer.Runner = runner
	installer.LookPath = allToolsPresent
	installer.WorkingRoot = t.TempDir()
	installer.Stdout = io.Discard
	installer.Stderr = io.Discard

	err := installer.RunTerraform(context.Background(), "token")
	if err == nil || !strings.Contains(err.Error(), "step 3 failed") {
		t.Fatalf("expected step 3 failure, got %v", err)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("commands after failed apply = %v", runner.commands)
	}
	updates := server.snapshot()
	last := updates[len(updates)-1]
	if last.Step != 3 || last.Update.Status != "failed" {
		t.Fatalf("last update = %+v, want step 3 failed", last)
	}
	if last.Update.Error == nil || *last.Update.Error != "exit status 1" {
		t.Fatalf("failed update error = %+v", last.Update.Error)
	}
	if last.Update.Output == nil || !strings.Contains(*last.Update.Output, "apply error") {
		t.Fatalf("failed update output = %+v", last.Update.Output)
	}
}

func TestCheckPrerequisitesListsAllMissingTools(t *testing.T) {
	installer := NewInstaller(nil)
	installer.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	err := installer.checkPrerequisites("gcp")
	if err == nil {
		t.Fatal("expected missing prerequisites")
	}
	for _, want := range []string{"tofu or terraform", "kubectl", "gcloud"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestTerraformInitIsSafeForExistingWorkingDirectory(t *testing.T) {
	server := newInstallerTestServer(t, testBundle())
	runner := &fakeRunner{}
	root := t.TempDir()
	stateDir := filepath.Join(root, "session-1")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "terraform.tfstate"), []byte(`{"version":4}`), 0o600); err != nil {
		t.Fatal(err)
	}

	installer := NewInstaller(server.client())
	installer.Runner = runner
	installer.LookPath = allToolsPresent
	installer.WorkingRoot = root
	installer.Stdout = io.Discard
	installer.Stderr = io.Discard
	if err := installer.RunTerraform(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "terraform.tfstate")); err != nil {
		t.Fatalf("existing Terraform state was removed: %v", err)
	}
	if !strings.HasPrefix(runner.commands[0], "tofu init ") {
		t.Fatalf("first command = %q", runner.commands[0])
	}
}

func TestRunCompletesClusterSetupAndAgentConnection(t *testing.T) {
	bundle := testBundle()
	bundle.AgentConnected = true
	version := "1.2.3"
	bundle.AgentVersion = &version
	server := newInstallerTestServer(t, bundle)
	runner := &fakeRunner{}
	installer := NewInstaller(server.client())
	installer.Runner = runner
	installer.LookPath = allToolsPresent
	installer.WorkingRoot = t.TempDir()
	installer.Stdout = io.Discard
	installer.Stderr = io.Discard
	installer.PollInterval = time.Millisecond
	installer.PollTimeout = time.Second

	if err := installer.Run(context.Background(), "token"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(runner.commands) != 6 {
		t.Fatalf("commands = %v, want Terraform 1-4, kubeconfig, kubectl", runner.commands)
	}
	if !strings.Contains(runner.commands[3], "output -raw kubeconfig_command") {
		t.Fatalf("step 4 command = %q", runner.commands[3])
	}
	if !strings.Contains(runner.commands[4], "aws eks update-kubeconfig") {
		t.Fatalf("step 5 command = %q", runner.commands[4])
	}
	if runner.commands[5] != "kubectl apply -f -" {
		t.Fatalf("step 6 command = %q", runner.commands[5])
	}

	completed := map[int]bool{}
	for _, update := range server.snapshot() {
		if update.Update.Status == "completed" {
			completed[update.Step] = true
		}
	}
	for step := 1; step <= 8; step++ {
		if !completed[step] {
			t.Fatalf("step %d was not reported completed", step)
		}
	}
}

func TestRunAgentConnectionTimeoutReportsStepSevenFailed(t *testing.T) {
	server := newInstallerTestServer(t, testBundle())
	installer := NewInstaller(server.client())
	installer.Runner = &fakeRunner{}
	installer.LookPath = allToolsPresent
	installer.WorkingRoot = t.TempDir()
	installer.Stdout = io.Discard
	installer.Stderr = io.Discard
	installer.PollInterval = time.Millisecond
	installer.PollTimeout = 5 * time.Millisecond

	err := installer.Run(context.Background(), "token")
	if err == nil || !strings.Contains(err.Error(), "step 7 failed") {
		t.Fatalf("expected step 7 timeout, got %v", err)
	}
	updates := server.snapshot()
	last := updates[len(updates)-1]
	if last.Step != 7 || last.Update.Status != "failed" {
		t.Fatalf("last update = %+v, want step 7 failed", last)
	}
}
