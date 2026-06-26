package tui

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// SortService is the narrow seam between the TUI and the worker/workflow layer.
// The TUI depends only on this interface — it never imports worker, workflow,
// or database types directly. Construction of worker.BatchJobFactory,
// workflow.ResolveSeamStrings, etc. lives inside the concrete implementation.
//
// This follows the project's interface-near-consumer convention: the TUI
// package defines the interface it needs, and the implementation wraps the
// concrete dependencies.
type SortService interface {
	// ProcessFiles starts asynchronous processing of matched files.
	// The caller receives progress events through the forward channel returned
	// by NewSortService (consumed via a SortEventSubscriber wired in eventSubscriber).
	ProcessFiles(ctx context.Context, files []fileItem, matches map[string]models.FileMatchInfo) error

	// Wait blocks until all submitted tasks have completed.
	Wait() error

	// Stop cancels all running tasks and waits for them to finish.
	Stop()

	// --- Processing options ---

	// SetOptions atomically replaces all processing options. For partial updates,
	// call LoadOptions, mutate the returned struct, and pass it to SetOptions.
	SetOptions(opts ProcessorOptions)
	// LoadOptions returns the current processing options snapshot.
	LoadOptions() ProcessorOptions

	// --- Runtime config ---

	// SetConfig applies the full TUIProcessorConfig snapshot.
	SetConfig(cfg TUIProcessorConfig)

	// --- Custom scraper support (for manual search) ---

	SetCustomScrapers(scrapers []string)
	GetCustomScrapers() []string

	// Registry returns the scraper registry for enumerating available scrapers.
	Registry() scrape.ScraperInstanceResolver
}

// ScanService is the narrow seam for directory scanning operations.
// The TUI uses this instead of importing workflow.WorkflowInterface directly.
type ScanService interface {
	// ScanAndMatch scans a directory for video files and matches JAV IDs.
	// Returns match info for each discovered file.
	ScanAndMatch(ctx context.Context, directory string, recursive bool) (*ScanResult, error)
}

// ScanResult holds the result of a scan-and-match operation.
// This is the TUI's own type, decoupled from workflow.ScanAndMatchResult.
type ScanResult struct {
	Files   []models.FileMatchInfo
	Skipped int
}
