package workflow

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/scrape"
)

// Workflow is the composition root for the 5-phase pipeline.
// it holds only sub-orchestrator interfaces — no raw dependencies.
// Each WorkflowInterface method is a one-line delegate to the corresponding sub-orchestrator.
type Workflow struct {
	scrape    scrapeOrchestrator
	apply     applyOrchestrator
	compare   compareOrchestrator
	preview   previewOrchestrator
	scanMatch scanAndMatchOrchestrator
}

// Scrape delegates to the internal scrapeOrchestrator which owns the 5 explicit
// Scrape steps: cache clear, scrape, DisplayTitle, persist, poster.
// Returns the scrape result, orchestration metadata, and any error.
func (w *Workflow) Scrape(ctx context.Context, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *OrchestrationMeta, error) {
	return w.scrape.Execute(ctx, cmd)
}

// Apply delegates to the internal applyOrchestrator which owns the 6-step Apply
// sequence: revert begin, organize, merge, DisplayTitle, download, NFO, revert complete.
func (w *Workflow) Apply(ctx context.Context, cmd ApplyCmd) (*ApplyResult, error) {
	return w.apply.Execute(ctx, cmd)
}

// Compare delegates to the internal compareOrchestrator which owns the compare pipeline:
// parse NFO, scrape fresh, merge.
func (w *Workflow) Compare(ctx context.Context, cmd CompareCmd) (*CompareResult, error) {
	return w.compare.Execute(ctx, cmd)
}

// Preview delegates to the internal previewOrchestrator which owns the preview
// calculation pipeline.
func (w *Workflow) Preview(ctx context.Context, cmd PreviewCmd) (*PreviewResult, error) {
	return w.preview.Execute(ctx, cmd)
}

// ScanAndMatch delegates to the internal scanAndMatchOrchestrator which owns the
// scan + match + multipart validation pipeline.
func (w *Workflow) ScanAndMatch(ctx context.Context, cmd ScanAndMatchCmd) (*ScanAndMatchResult, error) {
	return w.scanMatch.Execute(ctx, cmd)
}
