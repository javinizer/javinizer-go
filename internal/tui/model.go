package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// ViewMode represents the current view
type ViewMode int

const (
	ViewBrowser ViewMode = iota
	ViewDashboard
	ViewLogs
	ViewSettings
	ViewHelp
)

// Model represents the TUI application state
type Model struct {
	// Configuration
	config *config.Config

	// Business state (extracted - see state.go for testable functions)
	// NOTE: This field represents the MVP pattern separation (Story 9.2)
	// Pure state management functions are in state.go for unit testing
	state *State

	// View state (Bubble Tea specific - remains here)
	currentView ViewMode
	width       int
	height      int

	// File browser state
	files         []FileItem
	cursor        int
	selectedFiles map[string]bool
	matchResults  map[string]matcher.MatchResult
	sourcePath    string
	destPath      string // Destination path for organized files
	pathInput     textinput.Model
	editingPath   bool

	// Scanner/matcher for rescanning
	scanner   *scanner.Scanner
	matcher   *matcher.Matcher
	recursive bool

	// Folder picker state
	showingFolderPicker bool
	folderPickerItems   []FolderItem
	folderPickerCursor  int
	folderPickerPath    string
	folderPickerMode    string // "source" or "dest"

	// Manual search modal state
	showingManualSearch bool
	manualSearchInput   textinput.Model
	scraperCheckboxes   map[string]bool
	scraperList         []string // Cached sorted list of scraper names for stable ordering
	manualSearchCursor  int
	focusOnInput        bool

	// Task state
	tasks        map[string]*worker.TaskProgress
	taskOrder    []string // Maintain insertion order
	workerPool   *worker.Pool
	progressChan chan worker.ProgressUpdate
	processor    *ProcessingCoordinator
	isProcessing bool
	isPaused     bool
	dryRun       bool // Preview mode - don't make actual changes

	// Completion state
	processingComplete bool      // True when processing has finished
	completionTime     time.Time // When processing completed
	totalFilesCount    int       // Total number of files processed

	// Runtime settings (can be toggled in Settings view)
	forceUpdate         bool // Replace existing files (images, NFO)
	forceRefresh        bool // Clear DB cache and rescrape metadata
	moveFiles           bool // Move instead of copy
	scrapeEnabled       bool // Enable metadata scraping
	downloadEnabled     bool // Enable media downloads
	downloadExtrafanart bool // Enable extrafanart (screenshots) downloads
	organizeEnabled     bool // Enable file organization
	nfoEnabled          bool // Enable NFO generation
	updateMode          bool // Update mode: only create/update metadata without moving files
	settingsCursor      int  // Cursor position in settings view

	// Statistics
	stats       worker.ProgressStats
	startTime   time.Time
	elapsedTime time.Duration

	// Logs
	logs       []LogEntry
	maxLogs    int
	autoScroll bool
	logScroll  int

	// UI state
	ready    bool
	quitting bool
	err      error

	// Components (will be initialized with actual components)
	header       *Header
	browser      *Browser
	taskList     *TaskList
	console      *Console
	dashboard    *Dashboard
	logViewer    *LogViewer
	settingsView *SettingsView
	helpView     *HelpView
}

// FileItem represents a file in the browser
type FileItem struct {
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

// FolderItem represents a folder in the folder picker
type FolderItem struct {
	Path  string
	Name  string
	IsDir bool
}

// LogEntry represents a log message
type LogEntry struct {
	Level     string
	Message   string
	Timestamp time.Time
}

// New creates a new TUI model
func New(cfg *config.Config) *Model {
	m := &Model{
		config:        cfg,
		state:         NewState(), // Initialize business state (Story 9.2)
		currentView:   ViewBrowser,
		files:         make([]FileItem, 0),
		selectedFiles: make(map[string]bool),
		matchResults:  make(map[string]matcher.MatchResult),
		tasks:         make(map[string]*worker.TaskProgress),
		taskOrder:     make([]string, 0),
		workerPool:    nil, // Will be set later
		progressChan:  nil, // Will be set later
		logs:          make([]LogEntry, 0),
		maxLogs:       1000,
		autoScroll:    true,
		startTime:     time.Now(),

		// Runtime settings defaults
		forceUpdate:         false,
		forceRefresh:        false,
		moveFiles:           false,
		scrapeEnabled:       true,
		downloadEnabled:     true,
		downloadExtrafanart: cfg.Output.DownloadExtrafanart, // Initialize from config
		organizeEnabled:     true,
		nfoEnabled:          true,
		settingsCursor:      0,
	}

	// Initialize text input for path editing
	ti := textinput.New()
	ti.Placeholder = "Enter folder path..."
	ti.CharLimit = 256
	ti.Width = 50
	m.pathInput = ti

	// Initialize manual search input
	manualSearchInput := textinput.New()
	manualSearchInput.Placeholder = "Enter JAV ID or URL"
	manualSearchInput.CharLimit = 200
	manualSearchInput.Width = 50
	m.manualSearchInput = manualSearchInput
	m.scraperCheckboxes = make(map[string]bool)
	m.manualSearchCursor = 0
	m.focusOnInput = true
	m.showingManualSearch = false

	// Initialize components
	m.header = NewHeader()
	m.browser = NewBrowser()
	m.taskList = NewTaskList()
	m.console = NewConsole()
	m.dashboard = NewDashboard()
	m.logViewer = NewLogViewer()
	m.settingsView = NewSettingsView()
	m.helpView = NewHelpView()

	return m
}

// Init initializes the TUI
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		waitForProgress(m.progressChan),
	)
}

// SetSize sets the window size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.ready = true

	// Update component sizes
	// Browser view layout: browser (left) | tasks (right-top) + console (right-bottom)
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
func (m *Model) SetFiles(files []FileItem) {
	m.files = files
	if m.browser != nil {
		m.browser.SetItems(files)
	}
}

// SetSourcePath sets the source path being scanned
func (m *Model) SetSourcePath(path string) {
	m.sourcePath = path
	m.pathInput.SetValue(path)
	if m.browser != nil {
		m.browser.SetSourcePath(path)
	}
}

// SetDestPath sets the destination path for organized files
func (m *Model) SetDestPath(path string) {
	m.destPath = path
	if m.processor != nil {
		m.processor.SetDestPath(path)
	}
	if m.browser != nil {
		m.browser.SetDestPath(path)
	}
}

// GetDestPath returns the destination path
func (m *Model) GetDestPath() string {
	return m.destPath
}

// AddConsoleOutput adds output to the console
func (m *Model) AddConsoleOutput(output string) {
	if m.console != nil {
		m.console.AddEntry(output)
	}
}

// AddLog adds a log entry
func (m *Model) AddLog(level, message string) {
	entry := LogEntry{
		Level:     level,
		Message:   message,
		Timestamp: time.Now(),
	}

	m.logs = append(m.logs, entry)

	// Trim if exceeds max
	if len(m.logs) > m.maxLogs {
		m.logs = m.logs[len(m.logs)-m.maxLogs:]
	}

	if m.logViewer != nil {
		m.logViewer.AddLog(entry)
	}

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

// UpdateProgress updates task progress
func (m *Model) UpdateProgress(update worker.ProgressUpdate) {
	// Track new tasks for ordering
	if _, exists := m.tasks[update.TaskID]; !exists {
		m.taskOrder = append(m.taskOrder, update.TaskID)
	}

	// Delegate to handler for business logic (immutable task map update)
	m.tasks = HandleProgressUpdate(m.tasks, update)

	// Update task list component
	if m.taskList != nil {
		m.taskList.UpdateTask(update)
	}

	// Add to console output
	if update.Message != "" {
		// Format the console output with task type and status
		consoleMsg := fmt.Sprintf("[%s] %s", update.TaskID, update.Message)
		m.AddConsoleOutput(consoleMsg)
	}

	// Log progress if significant
	switch update.Status {
	case worker.TaskStatusSuccess:
		m.AddLog("info", update.Message)
	case worker.TaskStatusFailed:
		m.AddLog("error", update.Message)
	}
}

// UpdateStats updates statistics
func (m *Model) UpdateStats(stats worker.ProgressStats) {
	m.stats = stats
	m.elapsedTime = time.Since(m.startTime)

	if m.dashboard != nil {
		m.dashboard.UpdateStats(stats, m.elapsedTime)
	}
	if m.header != nil {
		m.header.UpdateStats(stats)
	}
}

// ToggleFileSelection toggles selection of a file
func (m *Model) ToggleFileSelection(path string) {
	if m.selectedFiles[path] {
		delete(m.selectedFiles, path)
	} else {
		m.selectedFiles[path] = true
	}

	// Update file item
	for i := range m.files {
		if m.files[i].Path == path {
			m.files[i].Selected = !m.files[i].Selected
			break
		}
	}

	if m.browser != nil {
		m.browser.ToggleSelection(path)
	}
}

// GetSelectedFiles returns the list of selected files
func (m *Model) GetSelectedFiles() []string {
	selected := make([]string, 0, len(m.selectedFiles))
	for path := range m.selectedFiles {
		selected = append(selected, path)
	}
	return selected
}

// SetProcessor sets the processing coordinator
func (m *Model) SetProcessor(processor *ProcessingCoordinator) {
	m.processor = processor
	// Sync dry-run state to processor
	if m.processor != nil {
		m.processor.SetDryRun(m.dryRun)
		m.processor.SetUpdateMode(m.updateMode)
	}
}

// SetWorkerPool sets the worker pool and progress channel
func (m *Model) SetWorkerPool(pool *worker.Pool, progressChan chan worker.ProgressUpdate) {
	m.workerPool = pool
	m.progressChan = progressChan
}

// SetDryRun sets the dry-run mode
func (m *Model) SetDryRun(dryRun bool) {
	m.dryRun = dryRun
	// Sync to processor if set
	if m.processor != nil {
		m.processor.SetDryRun(dryRun)
	}
	if dryRun {
		m.AddLog("info", "DRY RUN mode enabled - no changes will be made")
	}
}

// SetUpdateMode sets update mode and syncs it to processor.
func (m *Model) SetUpdateMode(updateMode bool) {
	m.updateMode = updateMode
	if updateMode {
		m.organizeEnabled = false
	}
	if m.processor != nil {
		m.processor.SetUpdateMode(updateMode)
		if updateMode {
			m.processor.SetOrganizeEnabled(false)
		}
	}
}

// SetMatchResults sets the match results for files
func (m *Model) SetMatchResults(matches map[string]matcher.MatchResult) {
	m.matchResults = matches
}

// StartProcessing begins processing selected files
func (m *Model) StartProcessing(ctx context.Context) error {
	if m.processor == nil {
		m.AddLog("error", "Processor not initialized")
		return fmt.Errorf("processor not initialized")
	}

	if m.isProcessing {
		m.AddLog("warn", "Already processing")
		return nil
	}

	selectedCount := len(m.selectedFiles)
	if selectedCount == 0 {
		m.AddLog("warn", "No files selected for processing")
		return nil
	}

	m.isProcessing = true
	m.processingComplete = false // Reset completion state
	m.startTime = time.Now()
	m.totalFilesCount = selectedCount // Track total files being processed

	// Filter to get selected file items
	// If a directory is selected, include all its child files
	selectedItems := make([]FileItem, 0, selectedCount)
	selectedDirs := make(map[string]bool) // Track selected directories

	// First pass: collect selected directories
	for i := range m.files {
		if m.files[i].Selected && m.files[i].IsDir {
			selectedDirs[m.files[i].Path] = true
		}
	}

	// Second pass: collect selected files and children of selected directories
	for i := range m.files {
		if m.files[i].Selected {
			logging.Debugf("Selected item: %s, IsDir: %v, Matched: %v, ID: %s", m.files[i].Path, m.files[i].IsDir, m.files[i].Matched, m.files[i].ID)
			selectedItems = append(selectedItems, m.files[i])
		} else if !m.files[i].IsDir {
			// Check if this file is a child of any selected directory
			for dirPath := range selectedDirs {
				if strings.HasPrefix(m.files[i].Path, dirPath+string(filepath.Separator)) {
					logging.Debugf("Including child of selected dir: %s (parent: %s)", m.files[i].Path, dirPath)
					selectedItems = append(selectedItems, m.files[i])
					break
				}
			}
		}
	}

	m.AddLog("info", fmt.Sprintf("Starting processing of %d files...", len(selectedItems)))
	logging.Debugf("Selected %d items (including children of directories) out of %d files", len(selectedItems), len(m.files))

	// Start processing in background
	go func() {
		if err := m.processor.ProcessFiles(ctx, selectedItems, m.matchResults); err != nil {
			m.AddLog("error", "Processing error: "+err.Error())
		} else {
			m.AddLog("info", "All tasks submitted successfully")
		}

		// Wait for all tasks to complete
		if err := m.processor.Wait(); err != nil {
			m.AddLog("error", "Some tasks failed: "+err.Error())
		} else {
			m.AddLog("info", "All tasks completed successfully")
		}

		m.isProcessing = false
		m.processingComplete = true
		m.completionTime = time.Now()
		// Stay on dashboard to allow user to review results
		// User can press '1' or 'b' to return to browser when ready
	}()

	return nil
}

// Error returns any error that occurred
func (m *Model) Error() error {
	return m.err
}

// Helper commands

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func waitForProgress(progressChan <-chan worker.ProgressUpdate) tea.Cmd {
	return func() tea.Msg {
		update := <-progressChan
		return ProgressMsg{
			TaskID:    update.TaskID,
			Type:      update.Type,
			Status:    update.Status,
			Progress:  update.Progress,
			Message:   update.Message,
			BytesDone: update.BytesDone,
			Error:     update.Error,
			Timestamp: update.Timestamp,
		}
	}
}

// Folder picker methods

// OpenFolderPicker opens the folder picker at the given path
func (m *Model) OpenFolderPicker(startPath, mode string) {
	if startPath == "" {
		startPath = "."
	}
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		absPath = startPath
	}

	m.showingFolderPicker = true
	m.folderPickerPath = absPath
	m.folderPickerCursor = 0
	m.folderPickerMode = mode
	m.loadFolderContents(absPath)
}

// CloseFolderPicker closes the folder picker
func (m *Model) CloseFolderPicker() {
	m.showingFolderPicker = false
	m.folderPickerItems = nil
	m.folderPickerCursor = 0
}

// loadFolderContents loads the contents of a directory for the folder picker
func (m *Model) loadFolderContents(path string) {
	items := []FolderItem{}

	// Add parent directory option if not at root
	if path != "/" && path != "." {
		parent := filepath.Dir(path)
		items = append(items, FolderItem{
			Path:  parent,
			Name:  "..",
			IsDir: true,
		})
	}

	// Read directory contents
	entries, err := os.ReadDir(path)
	if err != nil {
		m.AddLog("error", "Failed to read directory: "+err.Error())
		return
	}

	// Filter to only directories and sort
	for _, entry := range entries {
		if entry.IsDir() {
			// Skip hidden directories
			if entry.Name()[0] == '.' {
				continue
			}

			items = append(items, FolderItem{
				Path:  filepath.Join(path, entry.Name()),
				Name:  entry.Name(),
				IsDir: true,
			})
		}
	}

	// Sort alphabetically
	sort.Slice(items, func(i, j int) bool {
		// Keep ".." at top
		if items[i].Name == ".." {
			return true
		}
		if items[j].Name == ".." {
			return false
		}
		return items[i].Name < items[j].Name
	})

	m.folderPickerItems = items

	// Reset cursor if it's out of bounds
	if m.folderPickerCursor >= len(items) {
		m.folderPickerCursor = 0
	}
}

// NavigateToFolder navigates to a folder in the picker
func (m *Model) NavigateToFolder(path string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		m.AddLog("error", "Invalid path: "+err.Error())
		return
	}

	m.folderPickerPath = absPath
	m.folderPickerCursor = 0
	m.loadFolderContents(absPath)
}

// SelectCurrentFolder selects the current folder and closes the picker
func (m *Model) SelectCurrentFolder() {
	if m.folderPickerPath != "" {
		if m.folderPickerMode == "dest" {
			m.SetDestPath(m.folderPickerPath)
			m.AddLog("info", fmt.Sprintf("Output directory set to: %s", m.folderPickerPath))
		} else {
			m.SetSourcePath(m.folderPickerPath)
			m.AddLog("info", fmt.Sprintf("Source directory set to: %s", m.folderPickerPath))
		}
		m.CloseFolderPicker()
		return
	}
}

// SetScanner sets the scanner for rescanning files
func (m *Model) SetScanner(s *scanner.Scanner, mat *matcher.Matcher, recursive bool) {
	m.scanner = s
	m.matcher = mat
	m.recursive = recursive
}

// Rescan performs a rescan of the source path and updates the file list
func (m *Model) Rescan(path string) {
	if m.scanner == nil || m.matcher == nil {
		m.AddLog("error", "Scanner not initialized")
		m.AddConsoleOutput("❌ Scanner not initialized")
		return
	}

	m.AddLog("info", fmt.Sprintf("Scanning %s...", path))
	m.AddConsoleOutput(fmt.Sprintf("🔄 Refreshing file list from %s...", path))

	// Perform scan
	var scanResult *scanner.ScanResult
	var err error

	if m.recursive {
		scanResult, err = m.scanner.Scan(path)
	} else {
		scanResult, err = m.scanner.ScanSingle(path)
	}

	if err != nil {
		m.AddLog("error", fmt.Sprintf("Scan failed: %v", err))
		m.AddConsoleOutput(fmt.Sprintf("❌ Scan failed: %v", err))
		return
	}

	m.AddLog("info", fmt.Sprintf("Found %d video files", len(scanResult.Files)))
	m.AddConsoleOutput(fmt.Sprintf("📁 Found %d video files", len(scanResult.Files)))

	// Match JAV IDs
	matches := m.matcher.Match(scanResult.Files)

	// Validate letter-based multipart patterns using directory context
	// This prevents false positives like ABW-121-C.mp4 (Chinese subtitles) being marked as multipart
	matches = matcher.ValidateMultipartInDirectory(matches)

	m.AddLog("info", fmt.Sprintf("Matched %d JAV IDs", len(matches)))
	m.AddConsoleOutput(fmt.Sprintf("🎯 Matched %d JAV IDs", len(matches)))

	// Convert to match map
	matchMap := make(map[string]matcher.MatchResult)
	for _, match := range matches {
		matchMap[match.File.Path] = match
	}

	// Build file tree
	fileItems := buildFileTree(path, scanResult.Files, matchMap)

	// Update model
	m.SetFiles(fileItems)
	m.SetMatchResults(matchMap)

	// Clear selection since files changed
	m.selectedFiles = make(map[string]bool)
	m.cursor = 0

	// Log results
	if len(scanResult.Skipped) > 0 {
		m.AddLog("warn", fmt.Sprintf("Skipped %d files", len(scanResult.Skipped)))
		m.AddConsoleOutput(fmt.Sprintf("⚠️  Skipped %d files", len(scanResult.Skipped)))
	}
	if len(scanResult.Errors) > 0 {
		m.AddLog("error", fmt.Sprintf("%d errors during scan", len(scanResult.Errors)))
		m.AddConsoleOutput(fmt.Sprintf("❌ %d errors during scan", len(scanResult.Errors)))
	}

	m.AddLog("info", "Rescan complete")
	m.AddConsoleOutput("✅ Refresh complete!")
}

// buildFileTree constructs a tree structure of files and directories
func buildFileTree(basePath string, files []scanner.FileInfo, matchMap map[string]matcher.MatchResult) []FileItem {
	// Normalize base path
	absBasePath, err := filepath.Abs(basePath)
	if err != nil {
		absBasePath = basePath
	}

	// Group files by their immediate parent directory
	dirFiles := make(map[string][]scanner.FileInfo)
	allDirs := make(map[string]bool)

	for _, file := range files {
		dir := filepath.Dir(file.Path)
		dirFiles[dir] = append(dirFiles[dir], file)

		// Track all directories between file and base path
		current := dir
		for current != absBasePath && current != "." && current != "/" {
			rel, err := filepath.Rel(absBasePath, current)
			if err != nil || strings.HasPrefix(rel, "..") {
				break
			}
			allDirs[current] = true
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	// Build sorted list of directories
	dirs := make([]string, 0, len(allDirs))
	for dir := range allDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	// Calculate relative depth
	getDepth := func(path string) int {
		rel, err := filepath.Rel(absBasePath, path)
		if err != nil || rel == "." {
			return 0
		}
		return len(filepath.SplitList(rel))
	}

	result := []FileItem{}

	// Process each directory
	for _, dir := range dirs {
		depth := getDepth(dir) - 1
		if depth < 0 {
			depth = 0
		}

		// Add directory item
		result = append(result, FileItem{
			Path:     dir,
			Name:     filepath.Base(dir),
			Size:     0,
			IsDir:    true,
			Selected: false,
			Matched:  false,
			Depth:    depth,
			Parent:   filepath.Dir(dir),
		})

		// Add files in this directory
		if fileList, ok := dirFiles[dir]; ok {
			sort.Slice(fileList, func(i, j int) bool {
				return fileList[i].Name < fileList[j].Name
			})

			for _, file := range fileList {
				item := FileItem{
					Path:     file.Path,
					Name:     file.Name,
					Size:     file.Size,
					IsDir:    false,
					Selected: false,
					Matched:  false,
					Depth:    depth + 1,
					Parent:   dir,
				}

				if match, found := matchMap[file.Path]; found {
					item.Matched = true
					item.ID = match.ID
				}

				result = append(result, item)
			}
		}
	}

	// Add files in the base directory itself
	if baseFiles, ok := dirFiles[absBasePath]; ok {
		sort.Slice(baseFiles, func(i, j int) bool {
			return baseFiles[i].Name < baseFiles[j].Name
		})

		for _, file := range baseFiles {
			item := FileItem{
				Path:     file.Path,
				Name:     file.Name,
				Size:     file.Size,
				IsDir:    false,
				Selected: false,
				Matched:  false,
				Depth:    0,
				Parent:   absBasePath,
			}

			if match, found := matchMap[file.Path]; found {
				item.Matched = true
				item.ID = match.ID
			}

			result = append(result, item)
		}
	}

	return result
}
