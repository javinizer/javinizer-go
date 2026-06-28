package core

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/update"
)

// startUpdateChecker wires the background update checker into the API server
// lifecycle. It is gated on cfg.System.VersionCheckEnabled: when disabled, no
// goroutine is started and no one-shot check fires, so a disabled config never
// touches the network or the on-disk cache.
//
// Both the periodic ticker (StartBackgroundCheck) and the one-shot cache-warming
// check (BackgroundCheck) are bound to rt.ServerCtx(), which rt.Shutdown()
// cancels — so background work stops cleanly on server shutdown rather than
// leaking past process lifetime.
//
// opts allows tests to inject a stub Checker (no real network) and a temp-dir
// StatePath (no real filesystem writes); production callers pass a zero-value
// ServiceOptions, which reproduces NewService behavior exactly.
func startUpdateChecker(rt *APIRuntime, cfg *config.Config, opts update.ServiceOptions) {
	if cfg == nil || !cfg.System.VersionCheckEnabled {
		return
	}

	svc := update.NewServiceWithOptions(update.UpdateConfig{
		Enabled:                   cfg.System.VersionCheckEnabled,
		VersionCheckIntervalHours: cfg.System.VersionCheckIntervalHours,
		StableOnly:                cfg.System.VersionCheckStableOnly,
	}, opts)

	ctx := rt.ServerCtx()

	// One-shot check warms the cache SYNCHRONOUSLY before the first GetStatus
	// read so the nil-state skip in GetStatus stays benign for fresh installs.
	// This runs blocking (not `go`) by design: dispatching it as a goroutine would
	// let GetStatus race ahead of the cache write and return UpdateSourceNone on
	// the very first read — defeating the warm-up. BackgroundCheck bounds itself
	// with a 30s context timeout (and the GitHub HTTP client has a 10s timeout),
	// so the worst-case startup delay on a network failure is bounded; the common
	// case (GitHub reachable) completes in well under a second. Errors are logged
	// inside BackgroundCheck and never propagate to the caller, so a transient
	// network failure delays — but cannot break — startup. Gated on
	// VersionCheckEnabled above, so a disabled config never blocks.
	svc.BackgroundCheck(ctx)

	// Periodic ticker; stops when ctx (rt.ServerCtx) is cancelled on shutdown.
	svc.StartBackgroundCheck(ctx, svc.Interval())
	logging.Infof("Background update checker started (interval: %s)", svc.Interval())
}
