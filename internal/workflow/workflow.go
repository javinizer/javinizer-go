package workflow

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

// Workflow is the composition root for the 5-phase pipeline.
// Per ADR-0017: it holds only sub-orchestrator interfaces — no raw dependencies.
// Each WorkflowInterface method is a one-line delegate to the corresponding sub-orchestrator.
type Workflow struct {
	scrape    scrapeOrchestrator
	apply     applyOrchestrator
	compare   compareOrchestrator
	preview   previewOrchestrator
	scanMatch scanAndMatchOrchestrator
}

// Scrape delegates to the internal scrapeOrchestrator which owns the 5 explicit
// Scrape steps: cache clear, scrape, DisplayTitle, persist, poster (ADR-0017).
// Returns the scrape result, orchestration metadata, and any error.
func (w *Workflow) Scrape(ctx context.Context, cmd scrape.ScrapeCmd, progress scrape.ProgressFunc) (*scrape.ScrapeResult, *OrchestrationMeta, error) {
	return w.scrape.Execute(ctx, cmd, progress)
}

// Apply delegates to the internal applyOrchestrator which owns the 6-step Apply
// sequence: revert begin, organize, merge, DisplayTitle, download, NFO, revert complete (ADR-0017).
func (w *Workflow) Apply(ctx context.Context, cmd ApplyCmd, progress scrape.ProgressFunc) (*ApplyResult, error) {
	return w.apply.Execute(ctx, cmd, progress)
}

// Compare delegates to the internal compareOrchestrator which owns the compare pipeline:
// parse NFO, scrape fresh, merge (ADR-0017).
func (w *Workflow) Compare(ctx context.Context, cmd CompareCmd) (*CompareResult, error) {
	return w.compare.Execute(ctx, cmd)
}

// Preview delegates to the internal previewOrchestrator which owns the preview
// calculation pipeline (ADR-0017).
func (w *Workflow) Preview(ctx context.Context, cmd PreviewCmd) (*PreviewResult, error) {
	return w.preview.Execute(ctx, cmd)
}

// ScanAndMatch delegates to the internal scanAndMatchOrchestrator which owns the
// scan + match + multipart validation pipeline (ADR-0017).
func (w *Workflow) ScanAndMatch(ctx context.Context, cmd ScanAndMatchCmd) (*ScanAndMatchResult, error) {
	return w.scanMatch.Execute(ctx, cmd)
}
