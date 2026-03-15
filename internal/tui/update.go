package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
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
		// Delegate to handler for business logic
		progressUpdate := worker.ProgressUpdate{
			TaskID:    msg.TaskID,
			Type:      msg.Type,
			Status:    msg.Status,
			Progress:  msg.Progress,
			Message:   msg.Message,
			BytesDone: msg.BytesDone,
			Error:     msg.Error,
			Timestamp: msg.Timestamp,
		}
		m.UpdateProgress(progressUpdate)
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

	// If manual search modal is open, handle its keys first
	if m.showingManualSearch {
		return handleManualSearchInput(m, msg)
	}

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
		// Toggle help view - delegate to state.go
		previousView := m.currentView
		if m.currentView == ViewHelp {
			previousView = ViewBrowser
		}
		newState := ToggleHelp(State{CurrentView: m.currentView}, previousView)
		m.currentView = newState.CurrentView
		return m, nil

	case "1", "b":
		// 'b' for browser (also works as dismiss for completion banner)  - delegate to state.go
		newState := SwitchToView(State{CurrentView: m.currentView}, ViewBrowser)
		m.currentView = newState.CurrentView
		return m, nil

	case "2":
		// Delegate to state.go
		newState := SwitchToView(State{CurrentView: m.currentView}, ViewDashboard)
		m.currentView = newState.CurrentView
		return m, nil

	case "3":
		// Delegate to state.go
		newState := SwitchToView(State{CurrentView: m.currentView}, ViewLogs)
		m.currentView = newState.CurrentView
		return m, nil

	case "4":
		// Delegate to state.go
		newState := SwitchToView(State{CurrentView: m.currentView}, ViewSettings)
		m.currentView = newState.CurrentView
		return m, nil

	case "d":
		// 'd' to dismiss completion banner (stay on current view)
		if m.processingComplete {
			m.processingComplete = false
		}
		return m, nil

	case "tab":
		// Cycle through views (Browser -> Dashboard -> Logs -> Settings -> Browser) - delegate to state.go
		newState := CycleView(State{CurrentView: m.currentView})
		m.currentView = newState.CurrentView
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
	case "m":
		// Open manual search modal
		m.showingManualSearch = true
		m.focusOnInput = true
		m.manualSearchInput.Focus()
		m.manualSearchInput.SetValue("")

		// Build stable sorted list of scrapers (cache to prevent reshuffling)
		m.scraperList = make([]string, 0)
		m.scraperCheckboxes = make(map[string]bool)
		if m.processor != nil && m.processor.registry != nil {
			for _, scraper := range m.processor.registry.GetAll() {
				scraperName := scraper.Name()
				m.scraperList = append(m.scraperList, scraperName)
				m.scraperCheckboxes[scraperName] = false
			}
			// Sort for stable ordering
			sort.Strings(m.scraperList)
		}
		m.manualSearchCursor = 0
		return m, nil

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
			m.SetUpdateMode(!m.updateMode)
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

// handleManualSearchInput handles keyboard input for the manual search modal
func handleManualSearchInput(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.showingManualSearch = false
		m.manualSearchInput.Blur()
		return m, nil

	case "tab":
		m.focusOnInput = !m.focusOnInput
		if m.focusOnInput {
			m.manualSearchInput.Focus()
		} else {
			m.manualSearchInput.Blur()
		}
		return m, nil

	case "up":
		if !m.focusOnInput && m.manualSearchCursor > 0 {
			m.manualSearchCursor--
		}
		return m, nil

	case "down":
		if !m.focusOnInput && len(m.scraperList) > 0 {
			maxCursor := len(m.scraperList) - 1
			if m.manualSearchCursor < maxCursor {
				m.manualSearchCursor++
			}
		}
		return m, nil

	case " ":
		if !m.focusOnInput && len(m.scraperList) > 0 {
			// Toggle checkbox using cached list
			if m.manualSearchCursor < len(m.scraperList) {
				scraperName := m.scraperList[m.manualSearchCursor]
				m.scraperCheckboxes[scraperName] = !m.scraperCheckboxes[scraperName]
			}
		}
		return m, nil

	case "enter":
		return executeManualSearch(m)
	}

	// Update input if focused
	if m.focusOnInput {
		var cmd tea.Cmd
		m.manualSearchInput, cmd = m.manualSearchInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

// executeManualSearch performs the manual search with selected scrapers
func executeManualSearch(m *Model) (*Model, tea.Cmd) {
	input := strings.TrimSpace(m.manualSearchInput.Value())
	if input == "" {
		// Validation: no empty search
		return m, nil
	}

	// Get selected scrapers
	selectedScrapers := []string{}
	for scraper, checked := range m.scraperCheckboxes {
		if checked {
			selectedScrapers = append(selectedScrapers, scraper)
		}
	}

	if len(selectedScrapers) == 0 {
		// Validation: at least one scraper must be selected
		return m, nil
	}

	// Parse input (URL or ID)
	parsed, err := matcher.ParseInput(input)
	if err != nil {
		// Show error in logs
		m.AddLog("error", fmt.Sprintf("Invalid input: %v", err))
		return m, nil
	}

	// If URL with scraper hint, prioritize that scraper
	if parsed.IsURL && parsed.ScraperHint != "" {
		selectedScrapers = reorderWithPriority(selectedScrapers, parsed.ScraperHint)
	}

	// Set custom scrapers
	m.processor.SetCustomScrapers(selectedScrapers)

	// For manual search, we only want to scrape metadata and download media
	// Disable organize and NFO since there's no actual video file to work with
	originalOrganize := m.organizeEnabled
	originalNFO := m.nfoEnabled
	m.processor.SetOrganizeEnabled(false)
	m.processor.SetNFOEnabled(false)

	// Create fake match result for manual search
	// We need to create a minimal FileInfo for the MatchResult
	fakeFileInfo := scanner.FileInfo{
		Path: "manual-search",
		Name: parsed.ID,
	}

	manualMatch := matcher.MatchResult{
		File:        fakeFileInfo,
		ID:          parsed.ID,
		PartNumber:  0,
		PartSuffix:  "",
		IsMultiPart: false,
		MatchedBy:   "manual",
	}

	// Submit to processor
	ctx := context.Background()
	files := []FileItem{{
		Path:    "manual-search",
		Name:    parsed.ID,
		Matched: true,
		ID:      parsed.ID,
	}}
	matches := map[string]matcher.MatchResult{
		"manual-search": manualMatch,
	}

	if err := m.processor.ProcessFiles(ctx, files, matches); err != nil {
		m.AddLog("error", fmt.Sprintf("Failed to start manual search: %v", err))
	} else {
		m.AddLog("info", fmt.Sprintf("Started manual search for %s with scrapers: %v (metadata + downloads only)", parsed.ID, selectedScrapers))
	}

	// Close modal and reset
	m.showingManualSearch = false
	m.manualSearchInput.SetValue("")
	m.manualSearchInput.Blur()
	m.processor.SetCustomScrapers(nil) // Reset after submission

	// Restore original organize/NFO settings
	m.processor.SetOrganizeEnabled(originalOrganize)
	m.processor.SetNFOEnabled(originalNFO)

	// Switch to dashboard view to see progress
	m.currentView = ViewDashboard

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
