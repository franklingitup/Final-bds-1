package build

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// WorkerConfig configures the build worker.
type WorkerConfig struct {
	WorkerID          string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	StaleClaimTimeout time.Duration
	MaxConcurrent     int
}

// DefaultWorkerConfig returns the default worker configuration.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		WorkerID:          uuid.NewString()[:8],
		PollInterval:      5 * time.Second,
		HeartbeatInterval: 30 * time.Second,
		StaleClaimTimeout: 5 * time.Minute,
		MaxConcurrent:     2,
	}
}

// Builder is the interface that build executors implement.
type Builder interface {
	Build(ctx context.Context, job *BuildJob) error
}

// BuildJob contains all information needed to execute a build.
type BuildJob struct {
	Build      *Build
	OrgID      string
	WorkerID   string
	LogWriter  func(level, stream, message string, metadata map[string]any)
	OnProgress func(stage string)
}

// Worker polls for builds and executes them.
type Worker struct {
	svc       *Service
	builder   Builder
	cfg       WorkerConfig
	log       *slog.Logger
	
	running   map[string]context.CancelFunc
	runningMu sync.Mutex
}

// NewWorker creates a new build worker.
func NewWorker(svc *Service, builder Builder, cfg WorkerConfig, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.Default()
	}
	return &Worker{
		svc:     svc,
		builder: builder,
		cfg:     cfg,
		log:     log.With("component", "build-worker", "workerId", cfg.WorkerID),
		running: make(map[string]context.CancelFunc),
	}
}

// Run starts the worker and blocks until the context is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("starting build worker",
		"pollInterval", w.cfg.PollInterval,
		"heartbeatInterval", w.cfg.HeartbeatInterval,
		"maxConcurrent", w.cfg.MaxConcurrent)

	pollTicker := time.NewTicker(w.cfg.PollInterval)
	heartbeatTicker := time.NewTicker(w.cfg.HeartbeatInterval)
	cleanupTicker := time.NewTicker(w.cfg.StaleClaimTimeout / 2)
	
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("shutting down build worker")
			w.cancelAll()
			return ctx.Err()
			
		case <-pollTicker.C:
			w.poll(ctx)
			
		case <-heartbeatTicker.C:
			w.heartbeatAll(ctx)
			
		case <-cleanupTicker.C:
			w.cleanupStaleClaims(ctx)
		}
	}
}

func (w *Worker) poll(ctx context.Context) {
	w.runningMu.Lock()
	currentCount := len(w.running)
	w.runningMu.Unlock()

	if currentCount >= w.cfg.MaxConcurrent {
		return
	}

	// Try to claim a build
	build, err := w.svc.ClaimBuild(ctx, w.cfg.WorkerID)
	if err != nil {
		w.log.Error("failed to claim build", "error", err)
		return
	}
	if build == nil {
		return // No work available
	}

	w.log.Info("claimed build", "buildId", build.ID, "image", build.TargetImage)
	
	// Start the build in a goroutine
	buildCtx, cancel := context.WithTimeout(ctx, time.Duration(build.TimeoutSeconds)*time.Second)
	
	w.runningMu.Lock()
	w.running[build.ID] = cancel
	w.runningMu.Unlock()

	go func() {
		defer func() {
			cancel()
			w.runningMu.Lock()
			delete(w.running, build.ID)
			w.runningMu.Unlock()
		}()
		
		w.executeBuild(buildCtx, build)
	}()
}

func (w *Worker) executeBuild(ctx context.Context, build *Build) {
	startTime := time.Now()

	// logCtx is used exclusively for AppendBuildLog calls. It is intentionally
	// detached from the build's cancellable context so that failure log entries
	// are always written even when:
	//   • the build times out (ctx is context.DeadlineExceeded), or
	//   • the worker is shutting down (ctx is context.Canceled), or
	//   • the deferred cancel() in the caller goroutine fires before the final
	//     log entry is committed (see Bug B in the runCommand comment).
	//
	// A 30-second hard deadline ensures we don't block forever if the database
	// is unreachable.
	logCtx, stopLogCtx := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer stopLogCtx()

	logEntry := func(level, stream, message string) {
		if err := w.svc.AppendBuildLog(logCtx, build.OrgID, build.ID, level, stream, message, nil); err != nil {
			// Escalate to the worker logger so operators can see it even when
			// the build log itself is unavailable.
			w.log.Error("failed to write build log entry",
				"buildId", build.ID,
				"level", level,
				"stream", stream,
				"message", message,
				"error", err)
		}
	}

	job := &BuildJob{
		Build:    build,
		OrgID:    build.OrgID,
		WorkerID: w.cfg.WorkerID,
		LogWriter: func(level, stream, message string, metadata map[string]any) {
			logEntry(level, stream, message)
		},
		OnProgress: func(stage string) {
			w.log.Debug("build progress", "buildId", build.ID, "stage", stage)
		},
	}

	// Mark build as started. If this fails the build is stuck in StatusQueued
	// forever, so transition it to failed immediately.
	if err := w.svc.MarkBuildStarted(ctx, build.OrgID, build.ID, w.cfg.WorkerID, nil); err != nil {
		w.log.Error("failed to mark build started", "buildId", build.ID, "error", err)
		// Persist the failure so the build doesn't stay in "queued" permanently.
		logEntry(LevelError, StreamSystem, fmt.Sprintf("Worker failed to start build: %v", err))
		if markErr := w.svc.MarkBuildFailed(ctx, build.OrgID, build.ID, err.Error(), "start"); markErr != nil {
			w.log.Error("failed to mark build failed after start error", "buildId", build.ID, "error", markErr)
		}
		return
	}

	// Log the start
	logEntry(LevelInfo, StreamSystem, fmt.Sprintf("Build started by worker %s", w.cfg.WorkerID))

	// Execute the build.
	// runCommand (inside the builder) now uses sync.WaitGroup to ensure all
	// real-time log entries are committed before Build() returns, so by the
	// time we reach the error-handling block below every streamed Docker/git
	// line is already in the database.
	err := w.builder.Build(ctx, job)
	durationMs := time.Since(startTime).Milliseconds()

	if err != nil {
		stage := "building"
		var logMsg string

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logMsg = fmt.Sprintf("Build cancelled or timed out after %dms: %v", durationMs, err)
			stage = "timeout"
		} else {
			// err.Error() now includes any captured stderr from runCommand, so
			// this single line gives operators the full picture: exit code +
			// the actual command output that explains why the build failed.
			logMsg = fmt.Sprintf("Build failed after %dms: %v", durationMs, err)
		}

		// UNCONDITIONAL: always write the failure entry, regardless of whether
		// the build context was cancelled. logCtx is detached from ctx, so this
		// write succeeds even after ctx is done.
		logEntry(LevelError, StreamSystem, logMsg)
		w.log.Error("build failed",
			"buildId", build.ID,
			"durationMs", durationMs,
			"stage", stage,
			"error", err)

		if markErr := w.svc.MarkBuildFailed(ctx, build.OrgID, build.ID, err.Error(), stage); markErr != nil {
			w.log.Error("failed to mark build failed", "buildId", build.ID, "error", markErr)
		}
		return
	}

	// Build succeeded - artifact should have been created by the builder.
	logEntry(LevelInfo, StreamSystem, fmt.Sprintf("Build completed in %dms", durationMs))
	w.log.Info("build succeeded", "buildId", build.ID, "durationMs", durationMs)
}

func (w *Worker) heartbeatAll(ctx context.Context) {
	w.runningMu.Lock()
	buildIDs := make([]string, 0, len(w.running))
	for id := range w.running {
		buildIDs = append(buildIDs, id)
	}
	w.runningMu.Unlock()

	for _, id := range buildIDs {
		if err := w.svc.HeartbeatBuild(ctx, id, w.cfg.WorkerID); err != nil {
			w.log.Warn("failed to heartbeat build", "buildId", id, "error", err)
		}
	}
}

func (w *Worker) cleanupStaleClaims(ctx context.Context) {
	if err := w.svc.ReleaseStaleClaims(ctx, w.cfg.StaleClaimTimeout); err != nil {
		w.log.Warn("failed to release stale claims", "error", err)
	}
}

func (w *Worker) cancelAll() {
	w.runningMu.Lock()
	defer w.runningMu.Unlock()
	
	for id, cancel := range w.running {
		w.log.Info("cancelling build due to shutdown", "buildId", id)
		cancel()
	}
}
