package notification

import (
	"context"
	"log/slog"
	"time"
)

// WorkerConfig holds worker configuration.
type WorkerConfig struct {
	Enabled          bool
	PollInterval     time.Duration
	BatchSize        int
	RetryInterval    time.Duration
}

// DefaultWorkerConfig returns default worker configuration.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Enabled:       true,
		PollInterval:  5 * time.Second,
		BatchSize:     50,
		RetryInterval: 30 * time.Second,
	}
}

// Worker processes notification deliveries.
type Worker struct {
	svc    *Service
	cfg    WorkerConfig
	log    *slog.Logger
	stopCh chan struct{}
}

// NewWorker creates a new notification worker.
func NewWorker(svc *Service, cfg WorkerConfig, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.Default()
	}
	return &Worker{
		svc:    svc,
		cfg:    cfg,
		log:    log,
		stopCh: make(chan struct{}),
	}
}

// Start starts the worker.
func (w *Worker) Start(ctx context.Context) {
	if !w.cfg.Enabled {
		w.log.Info("notification worker disabled")
		return
	}

	w.log.Info("starting notification worker",
		"poll_interval", w.cfg.PollInterval,
		"batch_size", w.cfg.BatchSize)

	go w.runDeliveryLoop(ctx)
	go w.runRetryLoop(ctx)
}

// Stop stops the worker.
func (w *Worker) Stop() {
	close(w.stopCh)
}

func (w *Worker) runDeliveryLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processPending(ctx)
		}
	}
}

func (w *Worker) runRetryLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processRetries(ctx)
		}
	}
}

func (w *Worker) processPending(ctx context.Context) {
	processed, err := w.svc.ProcessPendingDeliveries(ctx, w.cfg.BatchSize)
	if err != nil {
		w.log.Error("failed to process pending deliveries", "error", err)
		return
	}
	if processed > 0 {
		w.log.Info("processed pending deliveries", "count", processed)
	}
}

func (w *Worker) processRetries(ctx context.Context) {
	processed, err := w.svc.ProcessRetries(ctx, w.cfg.BatchSize)
	if err != nil {
		w.log.Error("failed to process retries", "error", err)
		return
	}
	if processed > 0 {
		w.log.Info("processed retries", "count", processed)
	}
}
