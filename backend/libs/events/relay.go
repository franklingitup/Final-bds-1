package events

import (
	"context"
	"log/slog"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Relay drains the transactional outbox to the broker. It runs as a background
// loop: each tick it reads a batch of unpublished events inside a transaction,
// publishes them, and marks them published in the same transaction. Publishing
// inside the transaction is safe because the broker deduplicates on EventID, so
// a crash between publish and commit results in at-most a harmless re-publish.
type Relay struct {
	db       *database.DB
	store    Outbox
	pub      Publisher
	log      *slog.Logger
	interval time.Duration
	batch    int
}

// RelayOptions configures a Relay. Zero values fall back to defaults.
type RelayOptions struct {
	Interval time.Duration // poll interval; default 1s
	Batch    int           // rows per tick; default 100
}

// NewRelay constructs a Relay.
func NewRelay(db *database.DB, store Outbox, pub Publisher, log *slog.Logger, opts RelayOptions) *Relay {
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	if opts.Batch <= 0 {
		opts.Batch = 100
	}
	if log == nil {
		log = slog.Default()
	}
	return &Relay{
		db:       db,
		store:    store,
		pub:      pub,
		log:      log,
		interval: opts.Interval,
		batch:    opts.Batch,
	}
}

// Run polls the outbox until ctx is cancelled. It drains repeatedly while a tick
// yields a full batch so backlogs clear quickly.
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for {
				n, err := r.drain(ctx)
				if err != nil {
					r.log.Error("outbox relay drain failed", "error", err)
					break
				}
				if n < r.batch {
					break // backlog cleared for now
				}
			}
		}
	}
}

// drain publishes one batch of pending events in a single transaction and
// reports how many were processed.
func (r *Relay) drain(ctx context.Context) (int, error) {
	var processed int
	err := r.db.Tx(ctx, func(txCtx context.Context) error {
		records, err := r.store.FetchUnpublished(txCtx, r.batch)
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}

		ids := make([]string, 0, len(records))
		for _, rec := range records {
			if err := r.pub.Publish(txCtx, rec.Envelope); err != nil {
				return err // rolls back; locked rows are released and retried next tick
			}
			ids = append(ids, rec.ID)
		}
		processed = len(ids)
		return r.store.MarkPublished(txCtx, ids)
	})
	return processed, err
}
