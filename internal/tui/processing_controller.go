package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/panicutil"
)

// processingController owns the processing lifecycle: starting a batch,
// tracking progress via taskTracker, and waiting for completion in a
// background goroutine. Extracted from Model to isolate the
// startProcessing/finishProcessing pair and the sortSvc.Wait() goroutine
// from the Bubble Tea shell.
type processingController struct {
	taskTracker *taskTracker
	sortSvc     SortService
	logState    *logState
	browser     *browser
	taskList    *taskList
	console     *console

	// Narrow deps — set during construction, not *Model back-reference
	deps processingControllerDeps
}

// processingControllerDeps holds the narrow interface the processingController
// needs from the parent Model, replacing a full *Model back-reference.
type processingControllerDeps struct {
	// addLog records a user-visible log message.
	addLog func(level, message string)
	// addConsoleOutput appends a line to the console panel.
	addConsoleOutput func(output string)
	// browserState returns the current browser state for file/selection access.
	browserState func() browserState
	// startTime records when processing started (owned by Model for elapsed-time display).
	setStartTime func(time.Time)
	// loc translates a message id (with optional template data) for the current
	// TUI locale. It is nil-safe so the controller cannot panic if the
	// localizer failed to construct at startup.
	loc func(id string, template ...map[string]any) string
	// plural translates a count-aware message id via CLDR plural rules. It is
	// nil-safe so the controller cannot panic if the localizer is unavailable.
	plural func(id string, count interface{}, template ...map[string]any) string
}

// newProcessingController creates a controller wired to the given deps.
func newProcessingController(deps processingControllerDeps) processingController {
	return processingController{
		deps: deps,
	}
}

// setTaskTracker wires the shared taskTracker pointer.
func (pc *processingController) setTaskTracker(tt *taskTracker) {
	pc.taskTracker = tt
}

// setLogState wires the shared logState pointer.
func (pc *processingController) setLogState(ls *logState) {
	pc.logState = ls
}

// setSortService wires the sort service for processing.
func (pc *processingController) setSortService(svc SortService) {
	pc.sortSvc = svc
}

// setBrowser wires the browser component for UI updates.
func (pc *processingController) setBrowser(b *browser) {
	pc.browser = b
}

// setTaskList wires the taskList component for UI updates.
func (pc *processingController) setTaskList(tl *taskList) {
	pc.taskList = tl
}

// setConsole wires the console component for UI updates.
func (pc *processingController) setConsole(c *console) {
	pc.console = c
}

// StartProcessing begins processing selected files.
// The flow is: validate → expand selected files → delegate to sortSvc.ProcessFiles.
// ProcessFiles is non-blocking (it submits work via runner.Go internally),
// so a thin goroutine handles the blocking Wait() call.
func (pc *processingController) StartProcessing(ctx context.Context) error {
	if pc.sortSvc == nil {
		pc.deps.addLog("error", pc.deps.loc("TUISortServiceNotInitialized"))
		return fmt.Errorf("sort service not initialized")
	}

	if pc.taskTracker.isProcessing.Load() {
		pc.deps.addLog("warn", pc.deps.loc("TUILogAlreadyProcessing"))
		return nil
	}

	bs := pc.deps.browserState()
	selectedCount := bs.selectedCount()
	if selectedCount == 0 {
		pc.deps.addLog("warn", pc.deps.loc("TUILogNoFilesSelected"))
		return nil
	}

	pc.taskTracker.startProcessing(selectedCount)
	pc.deps.setStartTime(time.Now())

	// Expand directory selections into their child files (pure transformation)
	selectedItems := expandSelectedFiles(bs.files)

	pc.deps.addLog("info", pc.deps.plural("TUILogStartingProcessingFiles", len(selectedItems)))
	logging.Debugf("Selected %d items (including children of directories) out of %d files", len(selectedItems), len(bs.files))

	// ProcessFiles is non-blocking (submits via runner.Go), so call it directly.
	if err := pc.sortSvc.ProcessFiles(ctx, selectedItems, bs.matchResults); err != nil {
		pc.deps.addLog("error", pc.deps.loc("TUILogProcessingError", map[string]any{"Error": err.Error()}))
		pc.taskTracker.finishProcessing()
		return err
	}
	pc.deps.addLog("info", pc.deps.loc("TUIAllTasksSubmitted"))

	// Wait blocks until all runner tasks complete; run in a goroutine
	// so it doesn't freeze the UI. finishProcessing fires when done.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicErr := panicutil.FormatRecover(r)
				pc.deps.addLog("error", panicErr.Error())
				logging.Errorf("TUI processing wait %v", panicErr)
			}
			pc.taskTracker.finishProcessing()
		}()

		if err := pc.sortSvc.Wait(); err != nil {
			pc.deps.addLog("error", pc.deps.loc("TUILogSomeTasksFailed", map[string]any{"Error": err.Error()}))
		} else {
			pc.deps.addLog("info", pc.deps.loc("TUIAllTasksCompletedSuccessfully"))
		}
	}()

	return nil
}
