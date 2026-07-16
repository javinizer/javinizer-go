// Package progress provides context-scoped progress reporting for long-running
// operations (scrape, apply). Progress is telemetry threaded through the
// call stack via context.Context, mirroring the structured-logging pattern
// (logr, zap-in-context): callers inject a ProgressReporter with
// WithReporter and emitters retrieve it with FromContext. When no reporter
// is set, FromContext returns NoopProgress, preserving the silent no-op
// behavior of the previous nil-guarded prog() helper.
//
// This package owns all progress reporting vocabulary (ProgressStep constants
// and the ProgressReporter interface) so that both the Scrape Pipeline
// (internal/scrape) and the File Processor (internal/workflow) import from a
// neutral peer package instead of one importing the other.
//
// The package depends only on the standard library (context) — it MUST NOT
// import any internal/ package.
package progress

import "context"

// ProgressStep represents a step name in the progress reporting pipeline.
// The values match worker.JobEventStep string values so that the worker can
// safely convert via JobEventStep(string(step)).
type ProgressStep string

// Progress step constants. Callers should use these instead of bare string
// literals to prevent silent breakage if the step values ever change.
const (
	ProgressStepScrape   ProgressStep = "scrape"
	ProgressStepOrganize ProgressStep = "organize"
	ProgressStepDownload ProgressStep = "download"
	ProgressStepNFO      ProgressStep = "nfo"
	ProgressStepApply    ProgressStep = "apply"
)

// ProgressReporter is the contract for emitting progress updates during a
// long-running operation. Emitters retrieve the active reporter via
// FromContext(ctx) and call Report with the step, completion fraction (0-1),
// and a human-readable message.
type ProgressReporter interface {
	Report(step ProgressStep, pct float64, msg string)
}

// ReporterFunc is a function adapter that satisfies ProgressReporter. It is
// the single function adapter type for the package — callers that want a
// closure-based reporter wrap their function in ReporterFunc.
type ReporterFunc func(step ProgressStep, pct float64, msg string)

// Report calls the underlying function.
func (f ReporterFunc) Report(step ProgressStep, pct float64, msg string) {
	f(step, pct, msg)
}

// noopProgress is the singleton returned by FromContext when no reporter is
// set in the context. Its Report method is a no-op, matching today's
// previous nil-guarded prog() behavior.
type noopProgress struct{}

// Report is a no-op.
func (noopProgress) Report(ProgressStep, float64, string) {}

// NoopProgress is the default ProgressReporter returned by FromContext when
// no reporter has been injected. It silently discards all reports.
var NoopProgress ProgressReporter = noopProgress{}

type reporterKey struct{}

// WithReporter returns a new context carrying the given ProgressReporter. If
// r is nil the original ctx is returned unchanged (so FromContext falls back
// to NoopProgress). Both WithReporter and FromContext require a non-nil
// context (same precondition as context.WithValue).
func WithReporter(ctx context.Context, r ProgressReporter) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterKey{}, r)
}

// FromContext returns the ProgressReporter stored in ctx, or NoopProgress if
// none was set. Requires a non-nil ctx (same precondition as
// context.WithValue). Callers may invoke Report on the result unconditionally
// — NoopProgress handles the no-reporter case.
func FromContext(ctx context.Context) ProgressReporter {
	if r, ok := ctx.Value(reporterKey{}).(ProgressReporter); ok {
		return r
	}
	return NoopProgress
}
