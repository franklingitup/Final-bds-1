package builder

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	build "github.com/bdsplatform/platform/backend/services/build/internal"
)

// KanikoConfig configures the Kubernetes-native Kaniko build engine.
type KanikoConfig struct {
	// Namespace is where Kaniko Jobs and their pull/push secrets are created.
	Namespace string
	// Image is the Kaniko executor image reference.
	Image string
	// ServiceAccount, when set, is assigned to the Kaniko pod (e.g. for IRSA /
	// Workload Identity registry auth).
	ServiceAccount string
	// CPURequest/MemoryRequest/CPULimit/MemoryLimit are the default pod
	// resources; a build may override the limits via its own CPULimit/MemoryLimit.
	CPURequest    string
	MemoryRequest string
	CPULimit      string
	MemoryLimit   string
	// JobTTLSeconds sets ttlSecondsAfterFinished so completed Jobs are reaped by
	// Kubernetes even if explicit cleanup is missed.
	JobTTLSeconds int32
}

// DefaultKanikoConfig returns production-sensible defaults.
func DefaultKanikoConfig() KanikoConfig {
	return KanikoConfig{
		Namespace:     "bdsplatform-builds",
		Image:         "gcr.io/kaniko-project/executor:v1.23.2",
		CPURequest:    "500m",
		MemoryRequest: "1Gi",
		CPULimit:      "2",
		MemoryLimit:   "4Gi",
		JobTTLSeconds: 3600,
	}
}

// JobSpec is the fully-resolved description of a single Kaniko build Job. It is
// backend-agnostic so the orchestration and the Kubernetes adapter can be tested
// independently.
type JobSpec struct {
	// JobName is the deterministic Job/Pod name (derived from the build ID) so
	// re-claims after a worker crash adopt the existing Job instead of starting
	// a duplicate build.
	JobName string
	// SecretName is the deterministic name of the dockerconfigjson secret, or ""
	// when no registry credentials are mounted.
	SecretName string
	// Namespace is the target namespace.
	Namespace string
	// Image is the Kaniko executor image.
	Image string
	// ServiceAccount is the pod service account (may be empty).
	ServiceAccount string
	// Args are the Kaniko executor arguments.
	Args []string
	// Env are additional environment variables (e.g. git credentials).
	Env map[string]string
	// DockerConfigJSON is the .dockerconfigjson secret content, or nil when no
	// registry credentials are configured.
	DockerConfigJSON []byte
	// Labels are applied to the Job and pod (ownership + correlation).
	Labels map[string]string
	// Resource requests/limits and deadline.
	CPURequest, MemoryRequest, CPULimit, MemoryLimit string
	ActiveDeadlineSeconds                            int64
	TTLSecondsAfterFinished                          int32
}

// JobResult is the terminal outcome of a Kaniko Job.
type JobResult struct {
	Succeeded bool
	// Digest is the pushed image digest (sha256:...), extracted from the
	// container termination message. Empty if unavailable.
	Digest string
	// FailureReason is a short human-readable reason when Succeeded is false.
	FailureReason string
}

// KubeBackend abstracts the Kubernetes operations required to run a Kaniko Job.
// The production implementation is *kubeClient (kaniko_client.go); tests use a
// fake. Run owns the full lifecycle: it creates the secret+Job (idempotently),
// streams pod logs to logSink as they arrive, waits for completion (honouring
// ctx cancellation/timeout by deleting the Job), and cleans up.
type KubeBackend interface {
	Run(ctx context.Context, spec JobSpec, logSink func(stream, line string)) (JobResult, error)
}

// Ownership labels applied to every Kaniko Job so orphaned builds are
// identifiable and never collide with unrelated workloads.
const (
	labelManagedBy = "app.kubernetes.io/managed-by"
	managedByValue = "bdsplatform-build"
	labelBuildID   = "bdsplatform.io/build-id"
	labelOrgID     = "bdsplatform.io/org-id"

	// digestFile points Kaniko at the container termination log so Kubernetes
	// surfaces the digest via ContainerStatus.State.Terminated.Message without a
	// shared volume.
	digestFile = "/dev/termination-log"

	// dockerConfigMount is where the registry secret is mounted; Kaniko reads
	// $DOCKER_CONFIG/config.json.
	dockerConfigMount = "/kaniko/.docker"
)

var tracer = otel.Tracer("build.kaniko")

// buildWithKanikoBackend orchestrates a Kubernetes-native Kaniko build. It is
// invoked by MultiBuilder when a KubeBackend is configured.
func (b *MultiBuilder) buildWithKanikoBackend(ctx context.Context, job *build.BuildJob) error {
	ctx, span := tracer.Start(ctx, "kaniko.build",
		trace.WithAttributes(
			attribute.String("build.id", job.Build.ID),
			attribute.String("org.id", job.OrgID),
			attribute.String("target.image", job.Build.TargetImage),
		),
	)
	defer span.End()

	start := time.Now()
	metricJobsStarted.Inc()

	spec, err := b.buildKanikoJobSpec(ctx, job)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "build job spec")
		metricJobsFailed.WithLabelValues("spec").Inc()
		return fmt.Errorf("build kaniko job spec: %w", err)
	}

	job.OnProgress("building")
	job.LogWriter(build.LevelInfo, build.StreamSystem,
		fmt.Sprintf("Scheduling Kaniko build job %s in namespace %s", spec.JobName, spec.Namespace), nil)

	logSink := func(stream, line string) {
		level := build.LevelInfo
		if stream == build.StreamStderr {
			level = build.LevelWarn
		}
		job.LogWriter(level, stream, line, nil)
	}

	result, err := b.kubeBackend.Run(ctx, spec, logSink)
	durationMs := time.Since(start).Milliseconds()
	metricJobDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "run kaniko job")
		metricJobsFailed.WithLabelValues("run").Inc()
		return fmt.Errorf("run kaniko job: %w", err)
	}

	if !result.Succeeded {
		metricJobsFailed.WithLabelValues("build").Inc()
		reason := result.FailureReason
		if reason == "" {
			reason = "kaniko build failed"
		}
		span.SetStatus(codes.Error, reason)
		return fmt.Errorf("kaniko build failed: %s", reason)
	}

	job.OnProgress("finalizing")

	fullImage := composeImageRef(job.Build.TargetRegistry, job.Build.TargetImage)
	digest := result.Digest
	if digest == "" {
		// A successful push without a digest is unexpected; surface it rather
		// than persisting an empty artifact digest.
		metricJobsFailed.WithLabelValues("digest").Inc()
		return fmt.Errorf("kaniko build succeeded but produced no image digest")
	}

	artifact := &build.BuildArtifact{
		OrgID:        job.OrgID,
		BuildID:      job.Build.ID,
		ImageDigest:  digest,
		ImageTag:     fullImage,
		ManifestType: "oci",
	}

	if err := b.service.MarkBuildSucceeded(ctx, job.OrgID, job.Build.ID, artifact, durationMs); err != nil {
		return fmt.Errorf("mark build succeeded: %w", err)
	}

	metricJobsSucceeded.Inc()
	span.SetAttributes(attribute.String("image.digest", digest))
	job.LogWriter(build.LevelInfo, build.StreamSystem,
		fmt.Sprintf("Build completed: %s@%s", fullImage, digest), nil)
	return nil
}

// buildKanikoJobSpec resolves a BuildJob into a JobSpec: Kaniko args, git/registry
// credentials, resources, and deterministic Job/Secret names.
func (b *MultiBuilder) buildKanikoJobSpec(ctx context.Context, job *build.BuildJob) (JobSpec, error) {
	bd := job.Build

	gitURL := ""
	if bd.GitURL != nil {
		gitURL = *bd.GitURL
	}
	if strings.TrimSpace(gitURL) == "" {
		return JobSpec{}, fmt.Errorf("no git URL specified")
	}

	args, err := buildKanikoArgs(bd)
	if err != nil {
		return JobSpec{}, err
	}

	spec := JobSpec{
		JobName:               kanikoJobName(bd.ID),
		Namespace:             b.kanikoCfg.Namespace,
		Image:                 b.kanikoCfg.Image,
		ServiceAccount:        b.kanikoCfg.ServiceAccount,
		Args:                  args,
		Env:                   map[string]string{},
		Labels:                jobLabels(bd.ID, job.OrgID),
		CPURequest:            b.kanikoCfg.CPURequest,
		MemoryRequest:         b.kanikoCfg.MemoryRequest,
		CPULimit:              firstNonEmpty(ptrVal(bd.CPULimit), b.kanikoCfg.CPULimit),
		MemoryLimit:           firstNonEmpty(ptrVal(bd.MemoryLimit), b.kanikoCfg.MemoryLimit),
		ActiveDeadlineSeconds: int64(bd.TimeoutSeconds),
		TTLSecondsAfterFinished: b.kanikoCfg.JobTTLSeconds,
	}

	// Git credentials: Kaniko's git context reads GIT_USERNAME/GIT_PASSWORD for
	// HTTPS auth. Reuse the existing credential provider (GitHub service).
	if b.credProv != nil && strings.HasPrefix(gitURL, "https://") {
		token, err := b.credProv.GetGitToken(ctx, job.OrgID, gitURL)
		if err != nil {
			job.LogWriter(build.LevelWarn, build.StreamSystem,
				fmt.Sprintf("Failed to get git token: %v", err), nil)
		} else if token != "" {
			spec.Env["GIT_USERNAME"] = "x-access-token"
			spec.Env["GIT_PASSWORD"] = token
		}
	}

	// Registry credentials: build a .dockerconfigjson mounted at DOCKER_CONFIG so
	// Kaniko can push. Only when pushing and credentials are configured.
	if bd.PushToRegistry {
		if dockerCfg, ok := b.dockerConfigForRegistry(bd.TargetRegistry); ok {
			spec.DockerConfigJSON = dockerCfg
			spec.SecretName = kanikoSecretName(bd.ID)
			spec.Env["DOCKER_CONFIG"] = dockerConfigMount
		}
	}

	return spec, nil
}

// buildKanikoArgs assembles the Kaniko executor argument list from a Build. The
// git context, subpath, dockerfile, destination/no-push, build-args and
// digest-file are all derived here so the logic is unit-testable in isolation.
func buildKanikoArgs(bd *build.Build) ([]string, error) {
	gitURL := ""
	if bd.GitURL != nil {
		gitURL = *bd.GitURL
	}
	context, err := gitContext(gitURL, bd.GitRef, bd.GitCommit)
	if err != nil {
		return nil, err
	}

	args := []string{
		"--context=" + context,
		"--dockerfile=" + defaultString(bd.DockerfilePath, "Dockerfile"),
		"--digest-file=" + digestFile,
		// Deterministic, reproducible layers; avoids leaking build timestamps.
		"--reproducible=false",
		"--verbosity=info",
	}

	if sub := strings.TrimSpace(bd.ContextPath); sub != "" && sub != "." {
		args = append(args, "--context-sub-path="+strings.TrimPrefix(sub, "./"))
	}

	if bd.PushToRegistry {
		args = append(args, "--destination="+composeImageRef(bd.TargetRegistry, bd.TargetImage))
	} else {
		args = append(args, "--no-push")
	}

	// Build args, sorted for deterministic ordering (stable Job specs across
	// reconciliations and tests).
	var buildArgs map[string]string
	if len(bd.BuildArgs) > 0 {
		_ = json.Unmarshal(bd.BuildArgs, &buildArgs)
	}
	keys := make([]string, 0, len(buildArgs))
	for k := range buildArgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, fmt.Sprintf("--build-arg=%s=%s", k, buildArgs[k]))
	}

	return args, nil
}

// gitContext builds a Kaniko git context URI: git://host/path#ref[#commit].
func gitContext(gitURL, ref string, commit *string) (string, error) {
	if !strings.HasPrefix(gitURL, "https://") && !strings.HasPrefix(gitURL, "http://") {
		return "", fmt.Errorf("kaniko git context requires an http(s) git URL, got %q", gitURL)
	}
	scheme := "https://"
	if strings.HasPrefix(gitURL, "http://") {
		scheme = "http://"
	}
	hostPath := strings.TrimPrefix(strings.TrimPrefix(gitURL, "https://"), "http://")
	_ = scheme

	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "main"
	}
	// Fully-qualify a bare branch name so Kaniko resolves it deterministically.
	refPart := ref
	if !strings.HasPrefix(ref, "refs/") {
		refPart = "refs/heads/" + ref
	}

	context := "git://" + hostPath + "#" + refPart
	if commit != nil && strings.TrimSpace(*commit) != "" {
		context += "#" + strings.TrimSpace(*commit)
	}
	return context, nil
}

// dockerConfigForRegistry returns the .dockerconfigjson bytes for the target
// registry, or ok=false when no credentials are configured for it.
func (b *MultiBuilder) dockerConfigForRegistry(registry string) ([]byte, bool) {
	creds, ok := b.cfg.RegistryAuth[registry]
	if !ok {
		if registry == "docker.io" || registry == "" {
			if creds, ok = b.cfg.RegistryAuth["index.docker.io"]; !ok {
				creds, ok = b.cfg.RegistryAuth["registry-1.docker.io"]
			}
		}
	}
	if !ok || creds.Username == "" || creds.Password == "" {
		return nil, false
	}
	cfg, err := dockerConfigJSON(registry, creds.Username, creds.Password)
	if err != nil {
		return nil, false
	}
	return cfg, true
}

// dockerConfigJSON produces a Docker/OCI auth config for a single registry.
func dockerConfigJSON(registry, username, password string) ([]byte, error) {
	host := registry
	if host == "" || host == "docker.io" {
		host = "https://index.docker.io/v1/"
	}
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	cfg := map[string]any{
		"auths": map[string]any{
			host: map[string]any{
				"username": username,
				"password": password,
				"auth":     auth,
			},
		},
	}
	return json.Marshal(cfg)
}

// composeImageRef joins registry and image into a single reference, avoiding a
// duplicated registry prefix if the image already contains it.
func composeImageRef(registry, image string) string {
	registry = strings.TrimSpace(registry)
	image = strings.TrimSpace(image)
	if registry == "" || strings.HasPrefix(image, registry+"/") {
		return image
	}
	return registry + "/" + image
}

var dns1123Invalid = regexp.MustCompile(`[^a-z0-9-]`)

// kanikoJobName derives a deterministic, DNS-1123-safe Job name from a build ID.
func kanikoJobName(buildID string) string {
	name := "bdsplatform-build-" + strings.ToLower(buildID)
	name = dns1123Invalid.ReplaceAllString(name, "-")
	return truncateName(name)
}

// kanikoSecretName derives a deterministic secret name from a build ID.
func kanikoSecretName(buildID string) string {
	name := "bdsplatform-build-reg-" + strings.ToLower(buildID)
	name = dns1123Invalid.ReplaceAllString(name, "-")
	return truncateName(name)
}

// truncateName keeps a name within the 63-char DNS-1123 limit, appending a short
// content hash when truncation is needed to preserve uniqueness.
func truncateName(name string) string {
	if len(name) <= 63 {
		return strings.Trim(name, "-")
	}
	sum := sha256.Sum256([]byte(name))
	suffix := "-" + fmt.Sprintf("%x", sum[:4])
	return strings.Trim(name[:63-len(suffix)]+suffix, "-")
}

func jobLabels(buildID, orgID string) map[string]string {
	return map[string]string{
		labelManagedBy: managedByValue,
		labelBuildID:   sanitizeLabel(buildID),
		labelOrgID:     sanitizeLabel(orgID),
	}
}

// sanitizeLabel makes a value safe for a Kubernetes label (<=63 chars, alnum/-._).
func sanitizeLabel(v string) string {
	v = strings.ToLower(v)
	v = regexp.MustCompile(`[^a-z0-9_.-]`).ReplaceAllString(v, "-")
	if len(v) > 63 {
		v = v[:63]
	}
	return strings.Trim(v, "-._")
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func ptrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
