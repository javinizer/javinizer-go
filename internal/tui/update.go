package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/worker"
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

	case ProgressMsg:
		m.UpdateProgress(worker.ProgressUpdate{
			TaskID:    msg.TaskID,
			Type:      msg.Type,
			Status:    msg.Status,
			Progress:  msg.Progress,
			Message:   msg.Message,
			BytesDone: msg.BytesDone,
			Error:     msg.Error,
			Timestamp: msg.Timestamp,
		})
		// Continue waiting for more progress updates
		cmds = append(cmds, waitForProgress(m.progressChan))
		return m, tea.Batch(cmds...)

	case TickMsg:
		// Update elapsed time and stats
		if m.workerPool != nil {
			stats := m.workerPool.Stats()
			m.UpdateStats(worker.ProgressStats{
				Total:           stats.TotalTasks,
				Pending:         stats.Pending,
				Running:         stats.Running,
				Success:         stats.Success,
				Failed:          stats.Failed,
				Canceled:        stats.Canceled,
				OverallProgress: stats.OverallProgress,
			})
		}
		// Schedule next tick
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case LogMsg:
		m.AddLog(msg.Level, msg.Message)
		return m, nil

	case ErrorMsg:
		m.err = msg.Error
		m.AddLog("error", msg.Error.Error())
		return m, nil

	case QuitMsg:
		m.quitting = true
		if m.workerPool != nil {
			m.workerPool.Stop()
		}
		return m, tea.Quit

	case RescanMsg:
		// Update source path and rescan
		m.SetSourcePath(msg.Path)
		m.Rescan(msg.Path)
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

	// If folder picker is open, handle its keys first
	if m.showingFolderPicker {
		return m.handleFolderPickerKeys(msg)
	}

	// Global keybindings
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		if m.workerPool != nil {
			m.workerPool.Stop()
		}
		return m, tea.Quit

	case "?":
		// Toggle help view
		if m.currentView == ViewHelp {
			m.currentView = ViewBrowser
		} else {
			m.currentView = ViewHelp
		}
		return m, nil

	case "1":
		m.currentView = ViewBrowser
		return m, nil

	case "2":
		m.currentView = ViewDashboard
		return m, nil

	case "3":
		m.currentView = ViewLogs
		return m, nil

	case "4":
		m.currentView = ViewSettings
		return m, nil

	case "tab":
		// Cycle through views (Browser -> Dashboard -> Logs -> Settings -> Browser)
		m.currentView = (m.currentView + 1) % 5
		if m.currentView == ViewHelp {
			m.currentView = ViewBrowser
		}
		return m, nil
	}

	// View-specific keybindings
	switch m.currentView {
	case ViewBrowser:
		return m.handleBrowserKeys(msg)

	case ViewDashboard:
		return m.handleDashboardKeys(msg)

	case ViewLogs:
		return m.handleLogsKeys(msg)

	case ViewSettings:
		return m.handleSettingsKeys(msg)
	}

	return m, tea.Batch(cmds...)
}

// handleBrowserKeys handles browser view keybindings
func (m *Model) handleBrowserKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// If editing path, handle text input
	if m.editingPath {
		switch msg.String() {
		case "enter":
			// Confirm path change
			m.editingPath = false
			newPath := m.pathInput.Value()
			if newPath != "" && newPath != m.sourcePath {
				m.AddLog("info", "Path changed to: "+newPath)
				// Trigger rescan by sending a RescanMsg
				return m, func() tea.Msg {
					return RescanMsg{Path: newPath}
				}
			}
			return m, nil

		case "esc":
			// Cancel editing
			m.editingPath = false
			m.pathInput.SetValue(m.sourcePath)
			return m, nil

		default:
			// Update text input
			m.pathInput, cmd = m.pathInput.Update(msg)
			return m, cmd
		}
	}

	// Normal browser navigation
	switch msg.String() {
	case "f":
		// Open folder picker for source
		m.OpenFolderPicker(m.sourcePath, "source")
		return m, nil

	case "o":
		// Open folder picker for output destination
		destPath := m.destPath
		if destPath == "" {
			destPath = m.sourcePath
		}
		m.OpenFolderPicker(destPath, "dest")
		return m, nil

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		if m.browser != nil {
			m.browser.CursorUp()
		}

	case "down", "j":
		if m.cursor < len(m.files)-1 {
			m.cursor++
		}
		if m.browser != nil {
			m.browser.CursorDown()
		}

	case " ", "space":
		// Toggle selection of current file
		if m.cursor < len(m.files) {
			m.ToggleFileSelection(m.files[m.cursor].Path)
		}

	case "a":
		// Select all
		for i := range m.files {
			m.files[i].Selected = true
			m.selectedFiles[m.files[i].Path] = true
		}
		if m.browser != nil {
			m.browser.SelectAll()
		}

	case "A":
		// Deselect all
		for i := range m.files {
			m.files[i].Selected = false
		}
		m.selectedFiles = make(map[string]bool)
		if m.browser != nil {
			m.browser.DeselectAll()
		}

	case "enter":
		// Start processing
		if len(m.selectedFiles) == 0 {
			m.AddLog("warn", "No files selected. Use space to select files first.")
		} else if m.isProcessing {
			m.AddLog("warn", "Processing already in progress")
		} else {
			m.AddLog("info", "Enter key pressed, starting processing...")
			ctx := context.Background()
			if err := m.StartProcessing(ctx); err != nil {
				m.AddLog("error", "Failed to start processing: "+err.Error())
			}
		}

	case "p":
		// Pause/resume processing
		if m.isProcessing {
			m.isPaused = !m.isPaused
			if m.isPaused {
				m.AddLog("info", "Processing paused")
			} else {
				m.AddLog("info", "Processing resumed")
			}
		}

	case "r":
		// Refresh/rescan the current source path
		if m.sourcePath != "" {
			m.AddLog("info", "Refreshing file list...")
			return m, func() tea.Msg {
				return RescanMsg{Path: m.sourcePath}
			}
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
		if m.logScroll > 0 {
			m.logScroll--
		}
		if m.logViewer != nil {
			m.logViewer.ScrollUp()
		}

	case "down", "j":
		maxScroll := len(m.logs) - 10
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.logScroll < maxScroll {
			m.logScroll++
		}
		if m.logViewer != nil {
			m.logViewer.ScrollDown()
		}

	case "g":
		// Go to top
		m.logScroll = 0
		if m.logViewer != nil {
			m.logViewer.ScrollToTop()
		}

	case "G":
		// Go to bottom
		maxScroll := len(m.logs) - 10
		if maxScroll < 0 {
			maxScroll = 0
		}
		m.logScroll = maxScroll
		if m.logViewer != nil {
			m.logViewer.ScrollToBottom()
		}

	case "a":
		// Toggle auto-scroll
		m.autoScroll = !m.autoScroll
		if m.logViewer != nil {
			m.logViewer.ToggleAutoScroll()
		}
	}

	return m, nil
}

// handleFolderPickerKeys handles folder picker keybindings
func (m *Model) handleFolderPickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Close folder picker without selecting
		m.CloseFolderPicker()
		return m, nil

	case "up", "k":
		// Move cursor up
		if m.folderPickerCursor > 0 {
			m.folderPickerCursor--
		}
		return m, nil

	case "down", "j":
		// Move cursor down
		if m.folderPickerCursor < len(m.folderPickerItems)-1 {
			m.folderPickerCursor++
		}
		return m, nil

	case "enter":
		// Navigate into selected folder or go to parent
		if m.folderPickerCursor < len(m.folderPickerItems) {
			selectedItem := m.folderPickerItems[m.folderPickerCursor]
			m.NavigateToFolder(selectedItem.Path)
		}
		return m, nil

	case " ", "space":
		// Select current folder and close picker
		mode := m.folderPickerMode
		m.SelectCurrentFolder()
		// Only trigger rescan if changing source folder
		if mode == "source" {
			return m, func() tea.Msg {
				return RescanMsg{Path: m.folderPickerPath}
			}
		}
		return m, nil
	}

	return m, nil
}

// handleSettingsKeys handles settings view keybindings
func (m *Model) handleSettingsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxSettings := 9 // 0-9: 10 total settings

	switch msg.String() {
	case "up", "k":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
		if m.settingsView != nil {
			// Settings view doesn't need explicit cursor update
		}

	case "down", "j":
		if m.settingsCursor < maxSettings {
			m.settingsCursor++
		}
		if m.settingsView != nil {
			// Settings view doesn't need explicit cursor update
		}

	case " ", "space":
		// Toggle the selected setting
		switch m.settingsCursor {
		case 0:
			m.dryRun = !m.dryRun
			if m.processor != nil {
				m.processor.SetDryRun(m.dryRun)
			}
			if m.dryRun {
				m.AddLog("info", "Dry run mode enabled")
			} else {
				m.AddLog("info", "Dry run mode disabled")
			}

		case 1:
			m.forceUpdate = !m.forceUpdate
			if m.processor != nil {
				m.processor.SetForceUpdate(m.forceUpdate)
			}
			if m.forceUpdate {
				m.AddLog("info", "Force update enabled - will replace existing files")
			} else {
				m.AddLog("info", "Force update disabled")
			}

		case 2:
			m.forceRefresh = !m.forceRefresh
			if m.processor != nil {
				m.processor.SetForceRefresh(m.forceRefresh)
			}
			if m.forceRefresh {
				m.AddLog("info", "Force refresh enabled - will clear DB and rescrape")
			} else {
				m.AddLog("info", "Force refresh disabled")
			}

		case 3:
			m.moveFiles = !m.moveFiles
			if m.processor != nil {
				m.processor.SetMoveFiles(m.moveFiles)
			}
			if m.moveFiles {
				m.AddLog("info", "Move mode enabled - files will be moved instead of copied")
			} else {
				m.AddLog("info", "Copy mode enabled - files will be copied")
			}

		case 4:
			m.scrapeEnabled = !m.scrapeEnabled
			if m.processor != nil {
				m.processor.SetScrapeEnabled(m.scrapeEnabled)
			}
			if m.scrapeEnabled {
				m.AddLog("info", "Metadata scraping enabled")
			} else {
				m.AddLog("info", "Metadata scraping disabled")
			}

		case 5:
			m.downloadEnabled = !m.downloadEnabled
			if m.processor != nil {
				m.processor.SetDownloadEnabled(m.downloadEnabled)
			}
			if m.downloadEnabled {
				m.AddLog("info", "Media downloads enabled")
			} else {
				m.AddLog("info", "Media downloads disabled")
			}

		case 6:
			m.downloadExtrafanart = !m.downloadExtrafanart
			if m.processor != nil {
				m.processor.SetDownloadExtrafanart(m.downloadExtrafanart)
			}
			if m.downloadExtrafanart {
				m.AddLog("info", "Extrafanart downloads enabled")
			} else {
				m.AddLog("info", "Extrafanart downloads disabled")
			}

		case 7:
			m.organizeEnabled = !m.organizeEnabled
			if m.processor != nil {
				m.processor.SetOrganizeEnabled(m.organizeEnabled)
			}
			if m.organizeEnabled {
				m.AddLog("info", "File organization enabled")
			} else {
				m.AddLog("info", "File organization disabled")
			}

		case 8:
			m.nfoEnabled = !m.nfoEnabled
			if m.processor != nil {
				m.processor.SetNFOEnabled(m.nfoEnabled)
			}
			if m.nfoEnabled {
				m.AddLog("info", "NFO generation enabled")
			} else {
				m.AddLog("info", "NFO generation disabled")
			}

		case 9:
			m.updateMode = !m.updateMode
			// When update mode is enabled, disable file organization
			// When update mode is disabled, re-enable file organization
			if m.updateMode {
				m.organizeEnabled = false
				if m.processor != nil {
					m.processor.SetOrganizeEnabled(false)
				}
				m.AddLog("info", "Update mode enabled - files will remain in place, only metadata updated")
			} else {
				m.organizeEnabled = true
				if m.processor != nil {
					m.processor.SetOrganizeEnabled(true)
				}
				m.AddLog("info", "Update mode disabled - file organization re-enabled")
			}
		}
	}

	return m, nil
}
