package tui

import (
	"context"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Update handles messages and updates the model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case sortEventMsg:
		// Delegate to handler for business logic
		m.UpdateProgress(msg.Event)
		// Continue waiting for more sort events
		if m.taskTracker.eventSub != nil {
			cmds = append(cmds, waitForSortEvent(m.taskTracker.eventSub))
		}
		return m, tea.Batch(cmds...)

	case tickMsg:
		// Update elapsed time and stats
		m.UpdateStats(calculateStats(m.taskTracker.tasks))
		// Schedule next tick
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case logMsg:
		m.AddLog(msg.Level, msg.Message)
		return m, nil

	case errorMsg:
		m.err = msg.Error
		m.AddLog("error", msg.Error.Error())
		return m, nil

	case quitMsg:
		m.quitting = true
		if m.eventSub.sortSvc != nil {
			m.eventSub.sortSvc.Stop()
		}
		return m, tea.Quit

	case rescanMsg:
		// Update source path and rescan
		m.SetSourcePath(msg.Path)
		m.Rescan(msg.Path)
		return m, nil

	case actressPreviewResultMsg, actressMergeResultMsg:
		// Route async actress-merge results to the modal when it is open.
		if m.actressMergeCtl.Showing() {
			updated, cmd := m.actressMergeCtl.Update(msg)
			if am, ok := updated.(*actressMergeModal); ok {
				m.actressMergeCtl.modal = *am
			}
			return m, cmd
		}
		return m, nil
	}

	// Update active view component
	// Note: Components handle their own updates internally
	// We don't need to reassign since they're pointers

	return m, tea.Batch(cmds...)
}

// handleKeyPress handles keyboard input
func (m *Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// If actress merge modal is open, delegate to its controller
	if m.actressMergeCtl.Showing() {
		updated, cmd := m.actressMergeCtl.Update(msg)
		if am, ok := updated.(*actressMergeModal); ok {
			m.actressMergeCtl.modal = *am
		}
		return m, cmd
	}

	// If manual search modal is open, delegate to its Update method
	if m.manualSearch.showing {
		updated, cmd := m.manualSearch.Update(msg)
		if ms, ok := updated.(*manualSearchModal); ok {
			m.manualSearch = *ms
		}
		return m, cmd
	}

	// If folder picker is open, delegate to its controller
	if m.folderPickCtl.Showing() {
		updated, cmd := m.folderPickCtl.Update(msg)
		if fp, ok := updated.(*folderPickerModal); ok {
			m.folderPickCtl.modal = *fp
		}
		return m, cmd
	}

	// Global keybindings
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		if m.eventSub.sortSvc != nil {
			m.eventSub.sortSvc.Stop()
		}
		return m, tea.Quit

	case "?":
		// Toggle help view
		m.viewMgr.toggleHelp()
		return m, nil

	case "1", "b":
		// 'b' for browser (also works as dismiss for completion banner)
		m.viewMgr.switchTo(viewBrowser)
		return m, nil

	case "2":
		m.viewMgr.switchTo(viewDashboard)
		return m, nil

	case "3":
		m.viewMgr.switchTo(viewLogs)
		return m, nil

	case "4":
		m.viewMgr.switchTo(viewSettings)
		return m, nil

	case "d":
		// 'd' to dismiss completion banner (stay on current view)
		if m.taskTracker.processingComplete.Load() {
			m.taskTracker.processingComplete.Store(false)
		}
		return m, nil

	case "tab":
		// Cycle through views (browser -> dashboard -> Logs -> Settings -> browser)
		m.viewMgr.cycle()
		return m, nil
	}

	// View-specific keybindings
	switch m.viewMgr.currentView() {
	case viewBrowser:
		return m.handleBrowserKeys(msg)

	case viewDashboard:
		return m.handleDashboardKeys(msg)

	case viewLogs:
		return m.handleLogsKeys(msg)

	case viewSettings:
		return m.handleSettingsKeys(msg)
	}

	return m, tea.Batch(cmds...)
}

// handleBrowserKeys handles browser view keybindings.
// Each case is delegated to a named method; this function acts as a dispatch table.
func (m *Model) handleBrowserKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If editing path, handle text input
	if m.state.EditingPath {
		return m.handlePathEditing(msg)
	}

	// Dispatch table for browser keybindings
	switch msg.String() {
	case "m":
		return m.handleManualSearchOpen()
	case "M", "shift+m":
		return m.handleActressMergeOpen()
	case "f":
		return m.handleSourceFolderPicker()
	case "o":
		return m.handleDestFolderPicker()
	case "up", "k":
		return m.handleBrowserCursorUp()
	case "down", "j":
		return m.handleBrowserCursorDown()
	case " ", "space":
		return m.handleToggleCurrentFile()
	case "a":
		return m.handleSelectAll()
	case "A":
		return m.handleDeselectAll()
	case "enter":
		return m.handleStartProcessing()
	case "p":
		return m.handlePauseResume()
	case "r":
		return m.handleBrowserRefresh()
	}

	return m, nil
}

// handlePathEditing handles key input while the path text input is active.
func (m *Model) handlePathEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.String() {
	case "enter":
		m.state.EditingPath = false
		newPath := m.browserState.pathInput.Value()
		if newPath != "" && newPath != m.browserState.sourcePath {
			m.AddLog("info", "Path changed to: "+newPath)
			return m, func() tea.Msg {
				return rescanMsg{Path: newPath}
			}
		}
		return m, nil

	case "esc":
		m.state.EditingPath = false
		m.browserState.pathInput.SetValue(m.browserState.sourcePath)
		return m, nil

	default:
		m.browserState.pathInput, cmd = m.browserState.pathInput.Update(msg)
		return m, cmd
	}
}

// handleManualSearchOpen opens the manual search modal.
func (m *Model) handleManualSearchOpen() (tea.Model, tea.Cmd) {
	m.manualSearch.showing = true
	m.manualSearch.focusOnInput = true
	m.manualSearch.input.Focus()
	m.manualSearch.input.SetValue("")

	// Build stable sorted list of scrapers (cache to prevent reshuffling)
	m.manualSearch.scraperList = make([]string, 0)
	m.manualSearch.scraperCheckboxes = make(map[string]bool)
	if m.eventSub.sortSvc != nil && m.eventSub.sortSvc.Registry() != nil {
		for _, name := range m.eventSub.sortSvc.Registry().Names() {
			m.manualSearch.scraperList = append(m.manualSearch.scraperList, name)
			m.manualSearch.scraperCheckboxes[name] = false
		}
		// Sort for stable ordering
		sort.Strings(m.manualSearch.scraperList)
	}
	m.manualSearch.cursor = 0
	return m, nil
}

// handleActressMergeOpen opens the actress merge modal.
func (m *Model) handleActressMergeOpen() (tea.Model, tea.Cmd) {
	m.actressMergeCtl.Open()
	return m, nil
}

// handleSourceFolderPicker opens the folder picker for the source directory.
func (m *Model) handleSourceFolderPicker() (tea.Model, tea.Cmd) {
	m.folderPickCtl.Open(m.browserState.sourcePath, "source")
	return m, nil
}

// handleDestFolderPicker opens the folder picker for the output destination.
func (m *Model) handleDestFolderPicker() (tea.Model, tea.Cmd) {
	destPath := m.browserState.destPath
	if destPath == "" {
		destPath = m.browserState.sourcePath
	}
	m.folderPickCtl.Open(destPath, "dest")
	return m, nil
}

// handleBrowserCursorUp moves the cursor up in the file browser.
func (m *Model) handleBrowserCursorUp() (tea.Model, tea.Cmd) {
	if m.state.Cursor > 0 {
		m.state.Cursor--
	}
	if m.browser != nil {
		m.browser.CursorUp()
	}
	return m, nil
}

// handleBrowserCursorDown moves the cursor down in the file browser.
func (m *Model) handleBrowserCursorDown() (tea.Model, tea.Cmd) {
	if m.state.Cursor < len(m.browserState.files)-1 {
		m.state.Cursor++
	}
	if m.browser != nil {
		m.browser.CursorDown()
	}
	return m, nil
}

// handleToggleCurrentFile toggles selection of the file at the cursor.
func (m *Model) handleToggleCurrentFile() (tea.Model, tea.Cmd) {
	if m.state.Cursor < len(m.browserState.files) {
		m.ToggleFileSelection(m.browserState.files[m.state.Cursor].Path)
	}
	return m, nil
}

// handleSelectAll selects all files in the browser.
func (m *Model) handleSelectAll() (tea.Model, tea.Cmd) {
	m.browserState.selectAll()
	if m.browser != nil {
		m.browser.SelectAll()
	}
	return m, nil
}

// handleDeselectAll deselects all files in the browser.
func (m *Model) handleDeselectAll() (tea.Model, tea.Cmd) {
	m.browserState.deselectAll()
	if m.browser != nil {
		m.browser.DeselectAll()
	}
	return m, nil
}

// handleStartProcessing begins processing selected files.
func (m *Model) handleStartProcessing() (tea.Model, tea.Cmd) {
	if m.browserState.selectedCount() == 0 {
		m.AddLog("warn", "No files selected. Use space to select files first.")
	} else if m.taskTracker.isProcessing.Load() {
		m.AddLog("warn", "Processing already in progress")
	} else {
		m.AddLog("info", "Enter key pressed, starting processing...")
		// context.Background() is appropriate: TUI has no request-scoped context.
		ctx := context.Background()
		if err := m.StartProcessing(ctx); err != nil {
			m.AddLog("error", "Failed to start processing: "+err.Error())
		}
	}
	return m, nil
}

// handlePauseResume toggles pause/resume of processing.
func (m *Model) handlePauseResume() (tea.Model, tea.Cmd) {
	if m.taskTracker.isProcessing.Load() {
		m.state.IsPaused = !m.state.IsPaused
		if m.state.IsPaused {
			m.AddLog("info", "Processing paused")
		} else {
			m.AddLog("info", "Processing resumed")
		}
	}
	return m, nil
}

// handleBrowserRefresh rescans the current source path.
func (m *Model) handleBrowserRefresh() (tea.Model, tea.Cmd) {
	if m.browserState.sourcePath != "" {
		m.AddLog("info", "Refreshing file list...")
		return m, func() tea.Msg {
			return rescanMsg{Path: m.browserState.sourcePath}
		}
	}
	return m, nil
}

// handleDashboardKeys handles dashboard view keybindings
func (m *Model) handleDashboardKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		// Refresh/reset stats
		m.startTime = time.Now()
	}

	return m, nil
}

// handleLogsKeys handles logs view keybindings
func (m *Model) handleLogsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.logState.scrollUp()

	case "down", "j":
		m.logState.scrollDown()

	case "g":
		// Go to top
		m.logState.scrollToTop()

	case "G":
		// Go to bottom
		m.logState.scrollToBottom()

	case "a":
		// Toggle auto-scroll
		m.logState.toggleAutoScroll()
	}

	return m, nil
}

// handleSettingsKeys handles settings view keybindings
func (m *Model) handleSettingsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.settingsMgr.moveCursor(-1)

	case "down", "j":
		m.settingsMgr.moveCursor(1)

	case " ", "space":
		// Guard: refuse to enable move mode while link mode is active (issue #36)
		if m.settingsMgr.cursor == 3 && !m.settingsMgr.snapshot.MoveFiles && !m.canEnableMoveMode() {
			m.AddLog("warn", "Move mode cannot be enabled while link mode is active")
			return m, nil
		}
		desc := m.settingsMgr.toggle()
		m.AddLog("info", desc)
		// Persist Move Files setting to config when it's toggled (issue #36)
		if m.settingsMgr.cursor == 3 {
			m.saveConfig()
		}
	}

	return m, nil
}

// reorderWithPriority moves the priority scraper to the front of the list
func reorderWithPriority(scrapers []string, priority string) []string {
	result := []string{priority}
	for _, s := range scrapers {
		if s != priority {
			result = append(result, s)
		}
	}
	return result
}
