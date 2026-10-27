package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
)

// This reader owns no mutation service, confirmation ticket, or background worker.
type offlineScrapeEvidenceState struct {
	offlineEvidenceOnce   sync.Once
	offlineEvidenceReader *scrapeevidence.Service
}

// ErrOfflineEvidenceShuttingDown reports closed server-task admission.
var ErrOfflineEvidenceShuttingDown = errors.New("offline evidence operation unavailable during shutdown")

// OfflineScrapeEvidence pins startup settings without artifact or database I/O.
// Bootstrap calls it before serving; minimally wired test runtimes initialize lazily.
func (r *APIRuntime) OfflineScrapeEvidence() *scrapeevidence.Service {
	if r == nil {
		return nil
	}
	r.offlineEvidenceOnce.Do(func() {
		if r.ServerCtx().Err() != nil || r.deps == nil || r.deps.CoreDeps == nil || !r.deps.CoreDeps.HasConfig() {
			return
		}
		cfg := r.deps.CoreDeps.GetConfig().Metadata.OfflineScrapeEvidence
		if config.ValidateOfflineScrapeEvidence(cfg) != nil {
			return
		}
		cfg = cfg.Effective()
		r.offlineEvidenceReader, _ = scrapeevidence.New(scrapeevidence.Options{
			Enabled: cfg.Enabled, ArtifactDirectory: cfg.ArtifactDirectory,
			MaxAge: time.Duration(cfg.MaxAgeHours * float64(time.Hour)),
		})
	})
	return r.offlineEvidenceReader
}

// AdmitOfflineEvidenceOperation must precede opted-in dependency or job access.
// Default-off callers preserve the existing lifecycle. Release is idempotent.
func (r *APIRuntime) AdmitOfflineEvidenceOperation(requested bool) (func(), error) {
	if !requested {
		return func() {}, nil
	}
	if r == nil {
		return nil, ErrOfflineEvidenceShuttingDown
	}
	done := r.TrackServerTask()
	if done == nil {
		return nil, ErrOfflineEvidenceShuttingDown
	}
	var once sync.Once
	return func() { once.Do(done) }, nil
}

// LinkOfflineEvidenceOperationContext is used after early admission, including
// discovery before a batch's final multipart count is known. It never loads data.
func (r *APIRuntime) LinkOfflineEvidenceOperationContext(ctx context.Context, requested bool) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !requested {
		return ctx, func() {}
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ServerCtx(), cancel)
	if r.ServerCtx().Err() != nil {
		cancel()
	}
	return linked, func() { stop(); cancel() }
}

// BeginOfflineScrapeEvidence admits a single synchronous read-only operation.
func (r *APIRuntime) BeginOfflineScrapeEvidence(ctx context.Context, requested bool) (context.Context, func(), error) {
	return r.BeginOfflineScrapeEvidenceForFiles(ctx, requested, 1)
}

// BeginOfflineScrapeEvidenceForFiles pins one generation before any fanout.
// The caller releases only after every admitted reader/worker has joined.
func (r *APIRuntime) BeginOfflineScrapeEvidenceForFiles(ctx context.Context, requested bool, count int) (context.Context, func(), error) {
	if !requested {
		var disabled *scrapeevidence.Service
		clean, release := disabled.Begin(ctx, false)
		return clean, release, nil
	}
	done, err := r.AdmitOfflineEvidenceOperation(true)
	if err != nil {
		return ctx, nil, err
	}
	linked, cancel := r.LinkOfflineEvidenceOperationContext(ctx, true)
	ready := false
	defer func() {
		if !ready {
			cancel()
			done()
		}
	}()
	pinned, release := r.OfflineScrapeEvidence().BeginBatch(linked, true, count)
	var once sync.Once
	ready = true
	return pinned, func() { once.Do(func() { cancel(); release(); done() }) }, nil
}
