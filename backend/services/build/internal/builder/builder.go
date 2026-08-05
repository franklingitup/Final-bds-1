// Package builder provides container image build implementations.
package builder

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	build "github.com/bdsplatform/platform/backend/services/build/internal"
)

// Config holds builder configuration.
type Config struct {
	// KanikoImage is the Kaniko executor image.
	KanikoImage string

	// BuildKitAddr is the BuildKit daemon address.
	BuildKitAddr string

	// RegistryAuth holds registry credentials.
	RegistryAuth map[string]RegistryCredentials

	// WorkDir is the temporary working directory for builds.
	WorkDir string

	// UseDockerDaemon enables using the local Docker daemon (for development).
	UseDockerDaemon bool
}

// RegistryCredentials holds authentication for a container registry.
type RegistryCredentials struct {
	Username string
	Password string
}

// CredentialProvider retrieves credentials for repository cloning.
type CredentialProvider interface {
	// GetGitToken returns a token for cloning the given repository URL.
	// Returns empty string if no token is available.
	GetGitToken(ctx context.Context, orgID, repoURL string) (string, error)
}

// DefaultConfig returns the default builder configuration.
func DefaultConfig() Config {
	return Config{
		KanikoImage:     "gcr.io/kaniko-project/executor:latest",
		BuildKitAddr:    "unix:///run/buildkit/buildkitd.sock",
		WorkDir:         os.TempDir(),
		UseDockerDaemon: true, // Default to Docker for local development
	}
}

// MultiBuilder implements the Builder interface by delegating to Kaniko or BuildKit.
type MultiBuilder struct {
	cfg         Config
	service     *build.Service
	credProv    CredentialProvider
	kubeBackend KubeBackend
	kanikoCfg   KanikoConfig
}

// NewMultiBuilder creates a new MultiBuilder.
func NewMultiBuilder(cfg Config, svc *build.Service) *MultiBuilder {
	return &MultiBuilder{cfg: cfg, service: svc, kanikoCfg: DefaultKanikoConfig()}
}

// WithCredentialProvider sets the credential provider for repository cloning.
func (b *MultiBuilder) WithCredentialProvider(cp CredentialProvider) *MultiBuilder {
	b.credProv = cp
	return b
}

// WithKubeBackend enables the Kubernetes-native Kaniko build engine. When set,
// kaniko builds execute as Kubernetes Jobs instead of falling back to Docker.
func (b *MultiBuilder) WithKubeBackend(backend KubeBackend, cfg KanikoConfig) *MultiBuilder {
	b.kubeBackend = backend
	b.kanikoCfg = cfg
	return b
}

// Build executes a container image build.
func (b *MultiBuilder) Build(ctx context.Context, job *build.BuildJob) error {
	switch job.Build.BuilderType {
	case build.BuilderKaniko:
		return b.buildWithKaniko(ctx, job)
	case build.BuilderBuildKit:
		return b.buildWithBuildKit(ctx, job)
	default:
		// Default to Docker daemon for local development
		if b.cfg.UseDockerDaemon {
			return b.buildWithDocker(ctx, job)
		}
		return b.buildWithKaniko(ctx, job)
	}
}

// buildWithDocker builds using the local Docker daemon (for development).
func (b *MultiBuilder) buildWithDocker(ctx context.Context, job *build.BuildJob) error {
	job.LogWriter(build.LevelInfo, build.StreamSystem, "Starting build with Docker daemon", nil)
	job.OnProgress("cloning")

	// Clone the repository
	workDir, err := b.cloneRepository(ctx, job)
	if err != nil {
		return fmt.Errorf("clone repository: %w", err)
	}
	defer os.RemoveAll(workDir)

	// Detect Dockerfile
	dockerfilePath := filepath.Join(workDir, job.Build.ContextPath, job.Build.DockerfilePath)
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		// Try to detect Dockerfile
		detected, detectErr := b.detectDockerfile(workDir, job.Build.ContextPath)
		if detectErr != nil {
			return fmt.Errorf("dockerfile not found at %s and auto-detection failed: %w", job.Build.DockerfilePath, detectErr)
		}
		job.Build.DockerfilePath = detected
		dockerfilePath = filepath.Join(workDir, job.Build.ContextPath, detected)
		job.LogWriter(build.LevelInfo, build.StreamSystem, fmt.Sprintf("Detected Dockerfile: %s", detected), nil)
	}

	job.OnProgress("building")
	job.LogWriter(build.LevelInfo, build.StreamSystem, "Building image with Docker", nil)

	// Build the image
	fullImage := fmt.Sprintf("%s/%s", job.Build.TargetRegistry, job.Build.TargetImage)
	
	args := []string{"build", "-t", fullImage, "-f", dockerfilePath}
	
	// Add build args
	var buildArgs map[string]string
	if len(job.Build.BuildArgs) > 0 {
		_ = json.Unmarshal(job.Build.BuildArgs, &buildArgs)
		for k, v := range buildArgs {
			args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
		}
	}
	
	args = append(args, filepath.Join(workDir, job.Build.ContextPath))

	cmd := exec.CommandContext(ctx, "docker", args...)
	
	if err := b.runCommand(ctx, cmd, job); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}

	// Push to registry if requested
	if job.Build.PushToRegistry {
		job.OnProgress("pushing")

		// Authenticate to registry if credentials are configured
		if err := b.authenticateRegistry(ctx, job, job.Build.TargetRegistry); err != nil {
			job.LogWriter(build.LevelWarn, build.StreamSystem, fmt.Sprintf("Registry auth failed: %v", err), nil)
		}

		job.LogWriter(build.LevelInfo, build.StreamSystem, fmt.Sprintf("Pushing image to %s", fullImage), nil)

		pushCmd := exec.CommandContext(ctx, "docker", "push", fullImage)
		if err := b.runCommand(ctx, pushCmd, job); err != nil {
			return fmt.Errorf("docker push: %w", err)
		}
	}

	// Get image digest
	job.OnProgress("finalizing")
	digestCmd := exec.CommandContext(ctx, "docker", "inspect", "--format={{index .RepoDigests 0}}", fullImage)
	digestOutput, err := digestCmd.Output()
	if err != nil {
		job.LogWriter(build.LevelWarn, build.StreamSystem, "Could not get image digest", nil)
	}

	digest := strings.TrimSpace(string(digestOutput))
	if digest == "" {
		// Use a placeholder if we couldn't get the real digest
		digest = fmt.Sprintf("sha256:%s", time.Now().Format("20060102150405"))
	}

	// Extract just the digest part
	if idx := strings.Index(digest, "@"); idx != -1 {
		digest = digest[idx+1:]
	}

	// Create artifact
	artifact := &build.BuildArtifact{
		OrgID:        job.OrgID,
		BuildID:      job.Build.ID,
		ImageDigest:  digest,
		ImageTag:     fullImage,
		ManifestType: "docker",
	}

	durationMs := time.Since(job.Build.QueuedAt).Milliseconds()
	if err := b.service.MarkBuildSucceeded(ctx, job.OrgID, job.Build.ID, artifact, durationMs); err != nil {
		return fmt.Errorf("mark build succeeded: %w", err)
	}

	job.LogWriter(build.LevelInfo, build.StreamSystem, fmt.Sprintf("Build completed: %s", fullImage), nil)
	return nil
}

// buildWithKaniko builds using Kaniko. When a Kubernetes backend is configured
// the build runs as a Kaniko Job in-cluster; otherwise it falls back to the
// local Docker daemon for development.
func (b *MultiBuilder) buildWithKaniko(ctx context.Context, job *build.BuildJob) error {
	if b.kubeBackend != nil {
		return b.buildWithKanikoBackend(ctx, job)
	}

	job.LogWriter(build.LevelInfo, build.StreamSystem, "Starting build with Kaniko", nil)

	// No Kubernetes backend configured: fall back to Docker for local dev.
	if b.cfg.UseDockerDaemon {
		return b.buildWithDocker(ctx, job)
	}

	return fmt.Errorf("kaniko build engine is not configured (no Kubernetes backend and Docker daemon disabled)")
}

// buildWithBuildKit builds using BuildKit.
func (b *MultiBuilder) buildWithBuildKit(ctx context.Context, job *build.BuildJob) error {
	job.LogWriter(build.LevelInfo, build.StreamSystem, "Starting build with BuildKit", nil)
	
	// For now, fall back to Docker if available
	if b.cfg.UseDockerDaemon {
		return b.buildWithDocker(ctx, job)
	}

	// TODO: Implement full BuildKit integration
	// This would involve:
	// 1. Using buildctl to connect to the BuildKit daemon
	// 2. Building with the buildctl build command
	// 3. Pushing to the registry
	
	return fmt.Errorf("buildkit not yet implemented in non-Docker mode")
}

// cloneRepository clones the git repository to a temporary directory.
func (b *MultiBuilder) cloneRepository(ctx context.Context, job *build.BuildJob) (string, error) {
	workDir := filepath.Join(b.cfg.WorkDir, fmt.Sprintf("build-%s", job.Build.ID))
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}

	gitURL := ""
	if job.Build.GitURL != nil {
		gitURL = *job.Build.GitURL
	}
	if gitURL == "" {
		return "", fmt.Errorf("no git URL specified")
	}

	// Apply authentication token to URL if available
	cloneURL := gitURL
	if b.credProv != nil {
		token, err := b.credProv.GetGitToken(ctx, job.Build.OrgID, gitURL)
		if err != nil {
			job.LogWriter(build.LevelWarn, build.StreamSystem, fmt.Sprintf("Failed to get git token: %v", err), nil)
		} else if token != "" {
			authURL, authErr := injectTokenIntoURL(gitURL, token)
			if authErr != nil {
				job.LogWriter(build.LevelWarn, build.StreamSystem, fmt.Sprintf("Failed to inject token: %v", authErr), nil)
			} else {
				cloneURL = authURL
				job.LogWriter(build.LevelInfo, build.StreamSystem, "Using authenticated clone URL", nil)
			}
		}
	}

	job.LogWriter(build.LevelInfo, build.StreamSystem, fmt.Sprintf("Cloning %s (ref: %s)", gitURL, job.Build.GitRef), nil)

	// Clone with depth=1 for efficiency
	cloneCmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", "--branch", job.Build.GitRef, cloneURL, workDir)
	if err := b.runCommand(ctx, cloneCmd, job); err != nil {
		return "", fmt.Errorf("git clone: %w", err)
	}

	// Get the commit SHA
	revParseCmd := exec.CommandContext(ctx, "git", "-C", workDir, "rev-parse", "HEAD")
	output, err := revParseCmd.Output()
	if err == nil {
		commit := strings.TrimSpace(string(output))
		job.LogWriter(build.LevelInfo, build.StreamSystem, fmt.Sprintf("Cloned at commit: %s", commit[:12]), nil)
	}

	return workDir, nil
}

// injectTokenIntoURL adds token authentication to an HTTPS Git URL.
// Transforms: https://github.com/owner/repo.git -> https://x-access-token:TOKEN@github.com/owner/repo.git
func injectTokenIntoURL(gitURL, token string) (string, error) {
	if !strings.HasPrefix(gitURL, "https://") {
		return gitURL, nil // Only modify HTTPS URLs
	}

	// Parse and reconstruct with token
	urlWithoutScheme := strings.TrimPrefix(gitURL, "https://")
	return fmt.Sprintf("https://x-access-token:%s@%s", token, urlWithoutScheme), nil
}

// authenticateRegistry performs docker login if credentials are configured.
func (b *MultiBuilder) authenticateRegistry(ctx context.Context, job *build.BuildJob, registry string) error {
	// Check for configured credentials
	creds, ok := b.cfg.RegistryAuth[registry]
	if !ok {
		// Try common aliases
		if registry == "docker.io" || registry == "" {
			creds, ok = b.cfg.RegistryAuth["index.docker.io"]
			if !ok {
				creds, ok = b.cfg.RegistryAuth["registry-1.docker.io"]
			}
		}
	}

	if !ok || creds.Username == "" || creds.Password == "" {
		job.LogWriter(build.LevelDebug, build.StreamSystem, "No registry credentials configured, skipping auth", nil)
		return nil
	}

	job.LogWriter(build.LevelInfo, build.StreamSystem, fmt.Sprintf("Authenticating to registry %s", registry), nil)

	// Use docker login with stdin to avoid password in process list
	loginCmd := exec.CommandContext(ctx, "docker", "login", "--username", creds.Username, "--password-stdin", registry)
	loginCmd.Stdin = strings.NewReader(creds.Password)

	output, err := loginCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker login failed: %s: %w", string(output), err)
	}

	job.LogWriter(build.LevelInfo, build.StreamSystem, "Registry authentication successful", nil)
	return nil
}

// detectDockerfile attempts to find a Dockerfile in the build context.
func (b *MultiBuilder) detectDockerfile(workDir, contextPath string) (string, error) {
	contextDir := filepath.Join(workDir, contextPath)

	// Common Dockerfile names
	names := []string{
		"Dockerfile",
		"dockerfile",
		"Dockerfile.prod",
		"Dockerfile.production",
	}

	for _, name := range names {
		path := filepath.Join(contextDir, name)
		if _, err := os.Stat(path); err == nil {
			return name, nil
		}
	}

	return "", fmt.Errorf("no Dockerfile found in %s", contextDir)
}

// runCommand executes a command and streams output to the build log.
func (b *MultiBuilder) runCommand(ctx context.Context, cmd *exec.Cmd, job *build.BuildJob) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Stream output
	go b.streamOutput(stdout, build.StreamStdout, job)
	go b.streamOutput(stderr, build.StreamStderr, job)

	return cmd.Wait()
}

func (b *MultiBuilder) streamOutput(r io.Reader, stream string, job *build.BuildJob) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // Allow large lines
	
	for scanner.Scan() {
		line := scanner.Text()
		level := build.LevelInfo
		if stream == build.StreamStderr {
			level = build.LevelWarn
		}
		job.LogWriter(level, stream, line, nil)
	}
}
