package tui

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// RescanResult holds the structured output of a rescan operation.
// The controller returns this so the Model can update UI state without
// mixing scan logic and Bubble Tea mutations.
type RescanResult struct {
	// Files is the full match map keyed by file path.
	Files map[string]models.FileMatchInfo

	// FileRefs is the flat list of file references for tree construction.
	FileRefs []models.FileMatchInfo

	// FileItems is the pre-built tree structure ready for display.
	FileItems []fileItem

	// TotalFiles is the count of video files discovered.
	TotalFiles int

	// MatchedCount is the number of files with a matched JAV ID.
	MatchedCount int

	// Skipped is the count of files skipped during scanning.
	Skipped int

	// Err is the scan error, if any.
	Err error
}

// rescanController owns the scan→parse pipeline. It is testable without
// Bubble Tea because it has no UI mutation logic — it returns structured
// results that the caller applies.
type rescanController struct {
	scanSvc   ScanService
	recursive bool
}

// newRescanController creates a controller with the given scan service and
// recursive flag.
func newRescanController(svc ScanService, recursive bool) *rescanController {
	return &rescanController{scanSvc: svc, recursive: recursive}
}

// Run executes the scan→parse pipeline and returns structured results.
// context.Background() is appropriate here: the TUI has no request-scoped
// context, and the scan should run to completion regardless of UI state.
func (c *rescanController) Run(path string) RescanResult {
	if c.scanSvc == nil {
		return RescanResult{Err: fmt.Errorf("scan service not initialized")}
	}

	scanResult, err := c.scanSvc.ScanAndMatch(context.Background(), path, c.recursive)
	if err != nil {
		return RescanResult{Err: fmt.Errorf("scan failed: %w", err)}
	}

	matchMap, fileRefs := BuildMatchMapFromScanResult(scanResult)

	matchedCount := 0
	for _, mr := range matchMap {
		if mr.MovieID != "" {
			matchedCount++
		}
	}

	fileItems := BuildFileTree(path, fileRefs, matchMap)

	return RescanResult{
		Files:        matchMap,
		FileRefs:     fileRefs,
		FileItems:    fileItems,
		TotalFiles:   len(scanResult.Files),
		MatchedCount: matchedCount,
		Skipped:      scanResult.Skipped,
	}
}
