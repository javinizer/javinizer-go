package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// ViewMode represents the current view
type ViewMode int

const (
	ViewBrowser ViewMode = iota
	ViewDashboard
	ViewLogs
	ViewHelp
)

// Model represents the TUI application state
type Model struct {
	// Configuration
	config *config.Config

	// View state
	currentView ViewMode
	width       int
	height      int

	// File browser state
	files         []FileItem
	cursor        int
	selectedFiles map[string]bool
	matchResults  map[string]matcher.MatchResult
	sourcePath    string
	pathInput     textinput.Model
	editingPath   bool

	// Task state
	tasks         map[string]*worker.TaskProgress
	taskOrder     []string // Maintain insertion order
	workerPool    *worker.Pool
	progressChan  chan worker.ProgressUpdate
	processor     *ProcessingCoordinator
	isProcessing  bool
	isPaused      bool

	// Statistics
	stats         worker.ProgressStats
	startTime     time.Time
	elapsedTime   time.Duration

	// Logs
	logs        []LogEntry
	maxLogs     int
	autoScroll  bool
	logScroll   int

	// UI state
	ready       bool
	quitting    bool
	err         error

	// Components (will be initialized with actual components)
	header      *Header
	browser     *Browser
	taskList    *TaskList
	dashboard   *Dashboard
	logViewer   *LogViewer
	helpView    *HelpView
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
}

// LogEntry represents a log message
type LogEntry struct {
	Level     string
	Message   string
	Timestamp time.Time
}

// New creates a new TUI model
func New(cfg *config.Config) *Model {
	progressChan := make(chan worker.ProgressUpdate, cfg.Performance.BufferSize)

	progressTracker := worker.NewProgressTracker(progressChan)
	workerPool := worker.NewPool(
		cfg.Performance.MaxWorkers,
		time.Duration(cfg.Performance.WorkerTimeout)*time.Second,
		progressTracker,
	)

	m := &Model{
		config:        cfg,
		currentView:   ViewBrowser,
		files:         make([]FileItem, 0),
		selectedFiles: make(map[string]bool),
		matchResults:  make(map[string]matcher.MatchResult),
		tasks:         make(map[string]*worker.TaskProgress),
		taskOrder:     make([]string, 0),
		workerPool:    workerPool,
		progressChan:  progressChan,
		logs:          make([]LogEntry, 0),
		maxLogs:       1000,
		autoScroll:    true,
		startTime:     time.Now(),
	}

	// Initialize text input for path editing
	ti := textinput.New()
	ti.Placeholder = "Enter folder path..."
	ti.CharLimit = 256
	ti.Width = 50
	m.pathInput = ti

	// Initialize components
	m.header = NewHeader()
	m.browser = NewBrowser()
	m.taskList = NewTaskList()
	m.dashboard = NewDashboard()
	m.logViewer = NewLogViewer()
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
	if m.header != nil {
		m.header.SetWidth(width)
	}
	if m.browser != nil {
		m.browser.SetSize(width/2, height-6)
	}
	if m.taskList != nil {
		m.taskList.SetSize(width/2, height-6)
	}
	if m.dashboard != nil {
		m.dashboard.SetSize(width, height-4)
	}
	if m.logViewer != nil {
		m.logViewer.SetSize(width, height-4)
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
}

// UpdateProgress updates task progress
func (m *Model) UpdateProgress(update worker.ProgressUpdate) {
	// Update or create task progress
	if _, exists := m.tasks[update.TaskID]; !exists {
		m.taskOrder = append(m.taskOrder, update.TaskID)
	}

	m.tasks[update.TaskID] = &worker.TaskProgress{
		ID:        update.TaskID,
		Type:      update.Type,
		Status:    update.Status,
		Progress:  update.Progress,
		Message:   update.Message,
		BytesDone: update.BytesDone,
		UpdatedAt: update.Timestamp,
		Error:     update.Error,
	}

	// Update task list component
	if m.taskList != nil {
		m.taskList.UpdateTask(update)
	}

	// Log progress if significant
	if update.Status == worker.TaskStatusSuccess {
		m.AddLog("info", update.Message)
	} else if update.Status == worker.TaskStatusFailed {
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
}

// SetMatchResults sets the match results for files
func (m *Model) SetMatchResults(matches map[string]matcher.MatchResult) {
	m.matchResults = matches
}

// StartProcessing begins processing selected files
func (m *Model) StartProcessing(ctx context.Context) error {
	if m.processor == nil {
		return nil
	}

	if m.isProcessing {
		return nil
	}

	selectedCount := len(m.selectedFiles)
	if selectedCount == 0 {
		m.AddLog("warn", "No files selected for processing")
		return nil
	}

	m.isProcessing = true
	m.startTime = time.Now()

	// Filter to get selected file items
	selectedItems := make([]FileItem, 0, selectedCount)
	for i := range m.files {
		if m.files[i].Selected {
			selectedItems = append(selectedItems, m.files[i])
		}
	}

	m.AddLog("info", "Starting processing...")

	// Start processing in background
	go func() {
		if err := m.processor.ProcessFiles(ctx, selectedItems, m.matchResults); err != nil {
			m.AddLog("error", err.Error())
		}
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
