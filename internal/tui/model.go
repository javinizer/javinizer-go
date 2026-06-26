package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// viewMode represents the current view
type viewMode int

const (
	viewBrowser viewMode = iota
	viewDashboard
	viewLogs
	viewSettings
	viewHelp

	// viewModeCount is the total number of view modes (including help).
	viewModeCount = iota
)

// TUIModelConfig carries the narrow set of config fields the TUI Model reads.
type TUIModelConfig struct {
	DownloadExtrafanart bool
	MaxWorkers          int
	MoveFiles           bool
}

// settingsSnapshot holds all mutable processing options as a single struct.
// This replaces individual bool fields on Model (dryRun, forceUpdate, etc.)
// so that adding a new toggle only requires adding a field here and updating
// handleSettingsKeys — no new field or Set* method needed on Model.
type settingsSnapshot struct {
	DryRun              bool
	ForceUpdate         bool
	ForceRefresh        bool
	MoveFiles           bool
	ScrapeEnabled       bool
	DownloadEnabled     bool
	DownloadExtrafanart bool
	OrganizeEnabled     bool
	NFOEnabled          bool
	UpdateMode          bool
}

// manualSearchDeps holds the narrow interface the manual search modal needs
// from the parent Model, replacing a full *Model back-reference.
// References the SortService directly for pipeline operations
// and the settingsManager for reading current toggle state.
type manualSearchDeps struct {
	AddLog          func(level, message string)
	SortSvc         SortService // Direct reference — no *Model back-ref needed
	OrganizeEnabled func() bool
	NFOEnabled      func() bool
	SetCurrentView  func(view viewMode)
	Width           func() int
	Height          func() int
}

// manualSearchModal holds all state for the manual search modal.
type manualSearchModal struct {
	showing           bool
	input             textinput.Model
	scraperCheckboxes map[string]bool
	scraperList       []string // Cached sorted list of scraper names for stable ordering
	cursor            int
	focusOnInput      bool
	deps              manualSearchDeps
}

// actressMergeDeps holds the narrow interface the actress merge modal needs
// from the parent Model, replacing a full *Model back-reference.
type actressMergeDeps struct {
	AddLog      func(level, message string)
	ActressRepo func() database.ActressRepositoryInterface
	Width       func() int
	Height      func() int
}

// actressMergeModal holds all state for the actress merge modal.
type actressMergeModal struct {
	showing        bool
	targetInput    textinput.Model
	sourceInput    textinput.Model
	focus          int // 0: target, 1: source
	step           string
	preview        *database.ActressMergePreview
	resolutions    map[string]string
	conflictCursor int
	result         *database.ActressMergeResult
	err            string
	// mergeReqToken identifies the in-flight async merge/preview request so
	// out-of-order or stale responses (e.g. from a second Enter press) can be
	// ignored. Zero means no request in flight.
	mergeReqToken uint64
	deps          actressMergeDeps //nolint:all
}

// folderPickerDeps holds the narrow interface the folder picker modal needs
// from the parent Model, replacing a full *Model back-reference.
type folderPickerDeps struct {
	AddLog        func(level, message string)
	SetDestPath   func(path string)
	SetSourcePath func(path string)
	Width         func() int
	Height        func() int
}

// folderPickerModal holds all state for the folder picker modal.
type folderPickerModal struct {
	showing bool
	items   []folderItem
	cursor  int
	path    string
	mode    string // "source" or "dest"
	deps    folderPickerDeps
}

// browserState holds all file browser state: the file list, selection set,
// match results, source/destination paths, and the path text-input.
// Extracted from Model to reduce field count and group related concerns.
type browserState struct {
	files         []fileItem
	selectedFiles map[string]bool
	matchResults  map[string]models.FileMatchInfo
	sourcePath    string
	destPath      string // Destination path for organized files
	pathInput     textinput.Model
}

// newBrowserState creates a browserState with sensible defaults.
func newBrowserState() browserState {
	ti := textinput.New()
	ti.Placeholder = "Enter folder path..."
	ti.CharLimit = 256
	ti.Width = 50
	return browserState{
		files:         make([]fileItem, 0),
		selectedFiles: make(map[string]bool),
		matchResults:  make(map[string]models.FileMatchInfo),
		pathInput:     ti,
	}
}

// setSourcePath updates the source path and the path text-input.
func (bs *browserState) setSourcePath(path string) {
	bs.sourcePath = path
	bs.pathInput.SetValue(path)
}

// setDestPath updates the destination path.
func (bs *browserState) setDestPath(path string) {
	bs.destPath = path
}

// selectedCount returns the number of selected files.
func (bs *browserState) selectedCount() int {
	return len(bs.selectedFiles)
}

// getSelectedFiles returns the list of selected file paths.
func (bs *browserState) getSelectedFiles() []string {
	selected := make([]string, 0, len(bs.selectedFiles))
	for path := range bs.selectedFiles {
		selected = append(selected, path)
	}
	return selected
}

// toggleFileSelection toggles selection of a file path.
func (bs *browserState) toggleFileSelection(path string) {
	if bs.selectedFiles[path] {
		delete(bs.selectedFiles, path)
	} else {
		bs.selectedFiles[path] = true
	}
	for i := range bs.files {
		if bs.files[i].Path == path {
			bs.files[i].Selected = !bs.files[i].Selected
			break
		}
	}
}

// selectAll marks every file item as selected.
func (bs *browserState) selectAll() {
	for i := range bs.files {
		bs.files[i].Selected = true
		bs.selectedFiles[bs.files[i].Path] = true
	}
}

// deselectAll clears all file selections.
func (bs *browserState) deselectAll() {
	for i := range bs.files {
		bs.files[i].Selected = false
	}
	bs.selectedFiles = make(map[string]bool)
}

// taskTracker holds all task-tracking state: the task map, insertion order,
// event subscriber, and processing/completion flags.
// Extracted from Model to reduce field count and group related concerns.
type taskTracker struct {
	tasks              map[string]*taskState
	taskOrder          []string // Maintain insertion order
	eventSub           SortEventSubscriber
	isProcessing       atomic.Bool
	processingComplete atomic.Bool // True when processing has finished
	completionTime     time.Time   // When processing completed
	totalFilesCount    int         // Total number of files processed
}

// newTaskTracker creates a taskTracker with sensible defaults.
func newTaskTracker() taskTracker {
	return taskTracker{
		tasks:     make(map[string]*taskState),
		taskOrder: make([]string, 0),
	}
}

// updateProgress updates task progress from a JobEvent.
func (tt *taskTracker) updateProgress(event SortEvent) {
	taskID := event.MovieID
	if taskID == "" {
		return
	}

	// Track new tasks for ordering
	if _, exists := tt.tasks[taskID]; !exists {
		tt.taskOrder = append(tt.taskOrder, taskID)
	}

	// Delegate to handler for business logic (immutable task map update)
	tt.tasks = handleSortEvent(tt.tasks, event)
}

// startProcessing marks processing as active and resets completion state.
func (tt *taskTracker) startProcessing(totalFiles int) {
	tt.isProcessing.Store(true)
	tt.processingComplete.Store(false)
	tt.totalFilesCount = totalFiles
}

// finishProcessing marks processing as complete and records the time.
func (tt *taskTracker) finishProcessing() {
	tt.isProcessing.Store(false)
	tt.processingComplete.Store(true)
	tt.completionTime = time.Now()
}

// logState holds all log-related state: the log entries, max capacity,
// scroll position, and auto-scroll flag.
// Extracted from Model to reduce field count and group related concerns.
type logState struct {
	logs       []logEntry
	maxLogs    int
	autoScroll bool
	logScroll  int
}

// newLogState creates a logState with sensible defaults.
func newLogState() logState {
	return logState{
		logs:       make([]logEntry, 0),
		maxLogs:    1000,
		autoScroll: true,
	}
}

// add appends a log entry, trims to maxLogs, and adjusts scroll.
func (ls *logState) add(entry logEntry) {
	ls.logs = append(ls.logs, entry)

	// Trim if exceeds max
	if len(ls.logs) > ls.maxLogs {
		ls.logs = ls.logs[len(ls.logs)-ls.maxLogs:]
	}

	// Auto-scroll: keep scroll pinned to bottom
	if ls.autoScroll {
		ls.logScroll = len(ls.logs) - 1
	}
}

// scrollUp decrements the log scroll position (clamped to 0).
func (ls *logState) scrollUp() {
	if ls.logScroll > 0 {
		ls.logScroll--
	}
}

// scrollDown increments the log scroll position (clamped to max).
func (ls *logState) scrollDown() {
	maxScroll := len(ls.logs) - 10
	if maxScroll < 0 {
		maxScroll = 0
	}
	if ls.logScroll < maxScroll {
		ls.logScroll++
	}
}

// scrollToTop sets scroll to the first log entry.
func (ls *logState) scrollToTop() {
	ls.logScroll = 0
}

// scrollToBottom sets scroll to the last log entry.
func (ls *logState) scrollToBottom() {
	maxScroll := len(ls.logs) - 10
	if maxScroll < 0 {
		maxScroll = 0
	}
	ls.logScroll = maxScroll
}

// toggleAutoScroll toggles the auto-scroll flag.
func (ls *logState) toggleAutoScroll() {
	ls.autoScroll = !ls.autoScroll
}

// Model represents the TUI application state
type Model struct {
	// Configuration
	modelCfg TUIModelConfig

	// Business state (extracted - see state.go for testable functions)
	// NOTE: This field represents the MVP pattern separation (Story 9.2)
	// Pure state management functions are in state.go for unit testing
	state *state

	// View management (extracted - see view_manager.go)
	viewMgr viewManager

	// Settings management (extracted - see settings_manager.go)
	settingsMgr settingsManager

	// Link mode (set once at startup from CLI flags, not a toggleable setting)
	linkMode organizer.LinkMode

	// View state (Bubble Tea specific - remains here)
	width  int
	height int

	// File browser state (extracted sub-struct)
	browserState browserState

	// Sub-controllers (extracted from Model — P-1 decomposition)
	processingCtl processingController
	processor     *processingCoordinator
	browserCtl    browserController
	eventSub      eventSubscriber

	// Workflow for rescanning
	scanSvc     ScanService
	recursive   bool
	actressRepo database.ActressRepositoryInterface

	// Modal state (extracted into focused structs and controllers)
	manualSearch    manualSearchModal
	actressMergeCtl actressMergeController
	folderPickCtl   folderPickerController

	// Task state (extracted sub-struct)
	taskTracker taskTracker

	// Statistics
	startTime time.Time

	// Logs (extracted sub-struct)
	logState logState

	// UI state
	ready    bool
	quitting bool
	err      error

	// Components (will be initialized with actual components)
	header       *header
	browser      *browser
	taskList     *taskList
	console      *console
	dashboard    *dashboard
	logViewer    *logViewer
	settingsView *settingsView
	helpView     *helpView

	configPath string // path to config.yaml for persisting TUI settings
}

// fileItem represents a file in the browser
type fileItem struct {
	Path     string
	Name     string
	Size     int64
	IsDir    bool
	Selected bool
	Matched  bool
	ID       string // JAV ID if matched
	Depth    int    // Indentation depth for tree display
	Parent   string // Parent directory path
}

// folderItem represents a folder in the folder picker
type folderItem struct {
	Path  string
	Name  string
	IsDir bool
}

// logEntry represents a log message
type logEntry struct {
	Level     string
	Message   string
	Timestamp time.Time
}

// taskState represents the progress state of a single task in the TUI.
// Derived from SortEvent.
type taskState struct {
	ID        string
	Phase     SortEventPhase
	Step      SortEventStep
	Progress  float64 // 0.0 to 1.0
	Message   string
	UpdatedAt time.Time
}

// jobStats holds aggregate statistics about all tracked tasks.
// Computed locally from taskState map.
type jobStats struct {
	Total           int
	Pending         int
	Running         int
	success         int
	Failed          int
	OverallProgress float64
}

// New creates a new TUI model
func New(cfg TUIModelConfig) *Model {
	m := &Model{
		modelCfg:     cfg,
		state:        newState(), // Initialize business state (Story 9.2)
		viewMgr:      newViewManager(),
		settingsMgr:  newSettingsManager(settingsManagerDeps{}, cfg.DownloadExtrafanart, cfg.MoveFiles),
		browserState: newBrowserState(),
		taskTracker:  newTaskTracker(),
		logState:     newLogState(),
		startTime:    time.Now(),
	}

	wireModel(m)

	return m
}

// Init initializes the TUI
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}
	if m.taskTracker.eventSub != nil {
		cmds = append(cmds, waitForSortEvent(m.taskTracker.eventSub))
	}
	return tea.Batch(cmds...)
}

// SetSize sets the window size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.ready = true

	// Update component sizes
	// browser view layout: browser (left) | tasks (right-top) + console (right-bottom)
	rightPanelHeight := (height - 6) / 2 // Split right side vertically

	if m.header != nil {
		m.header.SetWidth(width)
	}
	if m.browser != nil {
		m.browser.SetSize(width/2, height-6)
	}
	if m.taskList != nil {
		m.taskList.SetSize(width/2, rightPanelHeight)
	}
	if m.console != nil {
		m.console.SetSize(width/2, rightPanelHeight)
	}
	if m.dashboard != nil {
		m.dashboard.SetSize(width, height-4)
	}
	if m.logViewer != nil {
		m.logViewer.SetSize(width, height-4)
	}
	if m.settingsView != nil {
		m.settingsView.SetSize(width, height-4)
	}
	if m.helpView != nil {
		m.helpView.SetSize(width, height-4)
	}
}

// SetFiles sets the files to display in the browser
func (m *Model) SetFiles(files []fileItem) {
	m.browserCtl.SetFiles(files)
}

// SetSourcePath sets the source path being scanned
func (m *Model) SetSourcePath(path string) {
	m.browserCtl.SetSourcePath(path)
}

// SetDestPath sets the destination path for organized files
func (m *Model) SetDestPath(path string) {
	m.browserCtl.SetDestPath(path)
}

// GetDestPath returns the destination path
func (m *Model) GetDestPath() string {
	return m.browserCtl.GetDestPath()
}

// AddConsoleOutput adds output to the console
func (m *Model) AddConsoleOutput(output string) {
	if m.console != nil {
		m.console.AddEntry(output)
	}
}

// AddLog adds a log entry
func (m *Model) AddLog(level, message string) {
	entry := logEntry{
		Level:     level,
		Message:   message,
		Timestamp: time.Now(),
	}

	m.logState.add(entry)

	// Also write to the actual log file
	switch level {
	case "debug":
		logging.Debug(message)
	case "info":
		logging.Info(message)
	case "warn":
		logging.Warn(message)
	case "error":
		logging.Error(message)
	default:
		logging.Info(message)
	}
}

// UpdateProgress updates task progress from a JobEvent
func (m *Model) UpdateProgress(event SortEvent) {
	m.eventSub.UpdateProgress(event)
}

// UpdateStats updates statistics
func (m *Model) UpdateStats(stats jobStats) {
	m.eventSub.UpdateStats(stats)
}

// ToggleFileSelection toggles selection of a file
func (m *Model) ToggleFileSelection(path string) {
	m.browserCtl.ToggleFileSelection(path)
}

// GetSelectedFiles returns the list of selected files
func (m *Model) GetSelectedFiles() []string {
	return m.browserCtl.GetSelectedFiles()
}

// SetProcessor sets the processing coordinator.
// Settings are synced via settingsMgr.deps.apply, not directly on the processor.
func (m *Model) SetProcessor(processor *processingCoordinator) {
	m.processor = processor
}

// SetSortService sets the sort service and wires the settingsManager
// deps to push settings changes to the sort service.
func (m *Model) SetSortService(svc SortService) {
	m.eventSub.SetSortService(svc)
	m.processingCtl.setSortService(svc)
}

// SetEventSubscriber sets the JobEvent subscriber for progress updates
func (m *Model) SetEventSubscriber(sub SortEventSubscriber) {
	m.eventSub.SetEventSubscriber(sub)
}

// pushSettingsToSortService pushes the full settings snapshot to the sort service.
// This replaces individual Set* methods so that adding a new toggle
// only requires updating settingsSnapshot and settingsManager.toggle.
// Delegates to eventSubscriber which owns the sort service reference.
func (m *Model) pushSettingsToSortService(s settingsSnapshot) {
	m.eventSub.pushSettingsToSortService(s)
}

// SetDryRun sets the dry-run mode and pushes the full snapshot to the processor.
func (m *Model) SetDryRun(dryRun bool) {
	m.settingsMgr.setDryRun(dryRun)
}

// SetMoveFiles sets the move-files mode and pushes the snapshot.
func (m *Model) SetMoveFiles(moveFiles bool) {
	m.settingsMgr.setMoveFiles(moveFiles)
}

// SetLinkMode records the link mode so the runtime move-files toggle can guard
// against the move+link combination rejected at startup (ValidateMoveLinkMode).
// Link mode is set at startup via --link-mode and is not toggled at runtime.
func (m *Model) SetLinkMode(mode organizer.LinkMode) {
	m.linkMode = mode
}

// canEnableMoveMode reports whether move mode can be enabled at runtime. Move and
// link modes are mutually exclusive (ValidateMoveLinkMode); if link mode is active,
// enabling move is refused to preserve the startup invariant.
func (m *Model) canEnableMoveMode() bool {
	return m.linkMode == organizer.LinkModeNone
}

// ResolveMoveMode determines the effective move mode for the TUI: an explicit
// --move flag overrides config.yaml's move_files; otherwise the config value
// is used (issue #36).
func ResolveMoveMode(configMoveFiles, moveFlagSet, moveFlagValue bool) bool {
	if moveFlagSet {
		return moveFlagValue
	}
	return configMoveFiles
}

// ValidateMoveLinkMode returns an error if move mode and link mode are both
// enabled, since they are mutually exclusive. effectiveMove may originate from
// config's move_files or the --move flag.
func ValidateMoveLinkMode(effectiveMove bool, linkMode organizer.LinkMode) error {
	if effectiveMove && linkMode != organizer.LinkModeNone {
		return fmt.Errorf("--link-mode can only be used when move mode is disabled (move_files is false and --move is not set)")
	}
	return nil
}

// SetConfigPath records the config.yaml path so TUI settings can be persisted.
func (m *Model) SetConfigPath(path string) {
	m.configPath = path
}

// saveConfig persists the Move Files setting to config.yaml. It uses config.Update
// (atomic read-modify-write under the file lock) so only move_files is written and
// session-only CLI/env overrides applied to the in-memory cfg (e.g. --extrafanart,
// the TUI-mode logging.output rewrite, --scraper-priority, LOG_LEVEL) are NOT
// leaked to disk, and a concurrent writer's changes cannot be silently reverted
// (issue #36).
func (m *Model) saveConfig() {
	if m.configPath == "" {
		return
	}
	err := config.Update(m.configPath, func(c *config.Config) {
		c.Output.Operation.MoveFiles = m.settingsMgr.snapshot.MoveFiles
	})
	if err != nil {
		m.AddLog("error", fmt.Sprintf("Failed to save Move Files setting to config: %v", err))
		return
	}
	m.AddLog("info", "Move Files setting saved to config")
}

// SetUpdateMode sets update mode and pushes the full snapshot to the processor.
func (m *Model) SetUpdateMode(updateMode bool) {
	m.settingsMgr.setUpdateMode(updateMode)
}

// SetMatchResults sets the match results for files
func (m *Model) SetMatchResults(matches map[string]models.FileMatchInfo) {
	m.browserCtl.SetMatchResults(matches)
}

// expandSelectedFiles returns the expanded list of file items to process.
// If a directory is selected, all its child files are included.
// This is a pure data transformation with no side effects.
func expandSelectedFiles(files []fileItem) []fileItem {
	selectedDirs := make(map[string]bool)
	for i := range files {
		if files[i].Selected && files[i].IsDir {
			selectedDirs[files[i].Path] = true
		}
	}

	expanded := make([]fileItem, 0, len(files))
	for i := range files {
		if files[i].Selected {
			expanded = append(expanded, files[i])
		} else if !files[i].IsDir {
			for dirPath := range selectedDirs {
				if strings.HasPrefix(files[i].Path, dirPath+string(filepath.Separator)) {
					expanded = append(expanded, files[i])
					break
				}
			}
		}
	}
	return expanded
}

// StartProcessing begins processing selected files.
// The flow is: validate → expand selected files → delegate to sortSvc.ProcessFiles.
// ProcessFiles is non-blocking (it submits work via runner.Go internally),
// so a thin goroutine handles the blocking Wait() call.
// Delegates to processingController which owns the processing lifecycle.
func (m *Model) StartProcessing(ctx context.Context) error {
	return m.processingCtl.StartProcessing(ctx)
}

// Error returns any error that occurred
func (m *Model) Error() error {
	return m.err
}

// Helper commands

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func waitForSortEvent(sub SortEventSubscriber) tea.Cmd {
	return func() tea.Msg {
		ch := sub.Events()
		if cs, ok := sub.(*channelSortEventSubscriber); ok {
			select {
			case event, ok := <-ch:
				if !ok {
					return nil
				}
				return sortEventMsg{Event: event}
			case <-cs.Done():
				return nil
			}
		}
		event, ok := <-ch
		if !ok {
			return nil
		}
		return sortEventMsg{Event: event}
	}
}

// SetScanService sets the scan service for rescanning files
func (m *Model) SetScanService(svc ScanService, recursive bool) {
	m.scanSvc = svc
	m.recursive = recursive
}

// Rescan performs a rescan of the source path and updates the file list.
// The rescanController owns the scan→parse pipeline; this method is a thin
// handler that calls the controller and applies results to UI state.
func (m *Model) Rescan(path string) {
	ctrl := newRescanController(m.scanSvc, m.recursive)

	if m.scanSvc == nil {
		m.AddLog("error", "Scan service not initialized")
		m.AddConsoleOutput("❌ Scan service not initialized")
		return
	}

	m.AddLog("info", fmt.Sprintf("Scanning %s...", path))
	m.AddConsoleOutput(fmt.Sprintf("🔄 Refreshing file list from %s...", path))

	result := ctrl.Run(path)

	if result.Err != nil {
		m.AddLog("error", result.Err.Error())
		m.AddConsoleOutput(fmt.Sprintf("❌ %s", result.Err.Error()))
		return
	}

	m.AddLog("info", fmt.Sprintf("Found %d video files", result.TotalFiles))
	m.AddConsoleOutput(fmt.Sprintf("📁 Found %d video files", result.TotalFiles))
	m.AddLog("info", fmt.Sprintf("Matched %d JAV IDs", result.MatchedCount))
	m.AddConsoleOutput(fmt.Sprintf("🎯 Matched %d JAV IDs", result.MatchedCount))

	// Update model from structured result
	m.SetFiles(result.FileItems)
	m.SetMatchResults(result.Files)

	// Clear selection since files changed
	m.browserState.selectedFiles = make(map[string]bool)
	m.state.Cursor = 0

	// Log results
	if result.Skipped > 0 {
		m.AddLog("warn", fmt.Sprintf("Skipped %d files", result.Skipped))
		m.AddConsoleOutput(fmt.Sprintf("⚠️  Skipped %d files", result.Skipped))
	}

	m.AddLog("info", "Rescan complete")
	m.AddConsoleOutput("✅ Refresh complete!")
}
