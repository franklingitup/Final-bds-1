// Package agent implements the customer-side cluster installer.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultControlPlaneEndpoint = "https://api.bdsplatform.io"
	maxReportedOutput           = 64 * 1024
)

type StepInfo struct {
	Number      int    `json:"number"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
}

type SessionBundle struct {
	SessionID       string          `json:"sessionId"`
	RequestID       string          `json:"requestId"`
	Provider        string          `json:"provider"`
	TerraformConfig string          `json:"terraformConfig"`
	TerraformVars   json.RawMessage `json:"terraformVars"`
	SessionToken    string          `json:"sessionToken"`
	BootstrapToken  string          `json:"bootstrapToken"`
	Steps           []StepInfo      `json:"steps"`
	Status          string          `json:"status"`
	AgentConnected  bool            `json:"agentConnected"`
	AgentVersion    *string         `json:"agentVersion,omitempty"`
	ExpiresAt       string          `json:"expiresAt"`
}

type UpdateStepRequest struct {
	Status string  `json:"status"`
	Output *string `json:"output,omitempty"`
	Error  *string `json:"error,omitempty"`
}

// Client calls the provisioning service using session/bootstrap bearer tokens.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) GetBundle(ctx context.Context, sessionToken string) (*SessionBundle, error) {
	var bundle SessionBundle
	if err := c.getJSON(ctx, "/v1/sessions/"+sessionToken+"/bundle", &bundle); err != nil {
		return nil, err
	}
	return &bundle, nil
}

func (c *Client) UpdateStep(ctx context.Context, sessionToken string, step int, update UpdateStepRequest) error {
	body, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("marshal step update: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/v1/sessions/%s/steps/%d", c.baseURL, sessionToken, step),
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create step update: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send step update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	var envelope struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		return fmt.Errorf("control plane returned %s: %v", resp.Status, envelope.Error)
	}
	return fmt.Errorf("control plane returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}

type CommandRunner interface {
	Run(ctx context.Context, dir, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

type stepReporter interface {
	UpdateStep(ctx context.Context, sessionToken string, step int, update UpdateStepRequest) error
}

// Installer owns one resumable installation run.
type Installer struct {
	Client       *Client
	Runner       CommandRunner
	LookPath     func(string) (string, error)
	Stdout       io.Writer
	Stderr       io.Writer
	WorkingRoot  string
	sessionToken string
	bundle       *SessionBundle
	terraformBin string
}

func NewInstaller(client *Client) *Installer {
	return &Installer{
		Client:      client,
		Runner:      ExecRunner{},
		LookPath:    exec.LookPath,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		WorkingRoot: ".bds-install",
	}
}

// RunTerraform performs installer steps 1-3.
func (i *Installer) RunTerraform(ctx context.Context, sessionToken string) error {
	if strings.TrimSpace(sessionToken) == "" {
		return errors.New("PLATFORM_INSTALL_TOKEN is required")
	}
	i.sessionToken = sessionToken

	bundle, err := i.Client.GetBundle(ctx, sessionToken)
	if err != nil {
		return fmt.Errorf("fetch installer bundle: %w", err)
	}
	i.bundle = bundle
	if err := i.checkPrerequisites(bundle.Provider); err != nil {
		return err
	}
	workDir, err := i.writeTerraformFiles(bundle)
	if err != nil {
		return err
	}

	if err := i.runStep(ctx, 1, workDir, i.terraformBin, []string{"init", "-input=false"}, nil); err != nil {
		return err
	}
	if err := i.runStep(ctx, 2, workDir, i.terraformBin,
		[]string{"plan", "-input=false", "-out=tfplan"}, nil); err != nil {
		return err
	}
	if err := i.runStep(ctx, 3, workDir, i.terraformBin,
		[]string{"apply", "-input=false", "-auto-approve", "tfplan"}, nil); err != nil {
		return err
	}
	return nil
}

func (i *Installer) checkPrerequisites(provider string) error {
	var missing []string
	for _, candidate := range []string{"tofu", "terraform"} {
		if path, err := i.LookPath(candidate); err == nil {
			i.terraformBin = path
			break
		}
	}
	if i.terraformBin == "" {
		missing = append(missing, "tofu or terraform")
	}
	if _, err := i.LookPath("kubectl"); err != nil {
		missing = append(missing, "kubectl")
	}
	cloudCLI := map[string]string{"aws": "aws", "azure": "az", "gcp": "gcloud"}[provider]
	if cloudCLI == "" {
		return fmt.Errorf("unsupported cloud provider %q", provider)
	}
	if _, err := i.LookPath(cloudCLI); err != nil {
		missing = append(missing, cloudCLI)
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing required executables on PATH: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (i *Installer) writeTerraformFiles(bundle *SessionBundle) (string, error) {
	if bundle.SessionID == "" {
		return "", errors.New("installer bundle is missing sessionId")
	}
	dir := filepath.Join(i.WorkingRoot, bundle.SessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create installer working directory: %w", err)
	}

	// GenerateTerraform stores Terraform's concatenated main/variables/outputs
	// text. Terraform is filename-agnostic, so main.tf contains that complete
	// configuration while the conventional companion files remain valid,
	// intentionally empty files.
	files := map[string][]byte{
		"main.tf":               []byte(bundle.TerraformConfig),
		"variables.tf":          []byte("// declarations are included in main.tf\n"),
		"outputs.tf":            []byte("// outputs are included in main.tf\n"),
		"terraform.tfvars.json": bundle.TerraformVars,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}
	return dir, nil
}

func (i *Installer) runStep(ctx context.Context, step int, dir, command string, args []string, stdin io.Reader) error {
	_ = i.Client.UpdateStep(ctx, i.sessionToken, step, UpdateStepRequest{Status: "running"})

	var captured bytes.Buffer
	stdout := io.MultiWriter(i.Stdout, &captured)
	stderr := io.MultiWriter(i.Stderr, &captured)
	err := i.Runner.Run(ctx, dir, command, args, stdin, stdout, stderr)
	output := truncateOutput(captured.String())
	if err != nil {
		errText := err.Error()
		if reportErr := i.Client.UpdateStep(ctx, i.sessionToken, step, UpdateStepRequest{
			Status: "failed",
			Output: stringPtr(output),
			Error:  &errText,
		}); reportErr != nil {
			return fmt.Errorf("step %d failed: %w (also failed to report: %v)", step, err, reportErr)
		}
		return fmt.Errorf("step %d failed: %w", step, err)
	}
	if err := i.Client.UpdateStep(ctx, i.sessionToken, step, UpdateStepRequest{
		Status: "completed",
		Output: stringPtr(output),
	}); err != nil {
		return fmt.Errorf("report step %d completion: %w", step, err)
	}
	return nil
}

func truncateOutput(output string) string {
	if len(output) <= maxReportedOutput {
		return output
	}
	return output[len(output)-maxReportedOutput:]
}

func stringPtr(value string) *string { return &value }
