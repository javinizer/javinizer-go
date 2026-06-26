package tui

// settingsManager manages the TUI settings snapshot and cursor.
// Extracted from Model to localize all settings-related state and mutations.
// Holds a narrow deps struct for pushing settings to the processingCoordinator
// instead of a *Model back-reference.
type settingsManager struct {
	snapshot settingsSnapshot
	cursor   int

	// Narrow deps — set once during construction
	deps settingsManagerDeps
}

// settingsManagerDeps holds the narrow interface the settingsManager needs
// to push settings changes to the processing pipeline and log changes.
// Replaces a full *Model back-reference.
type settingsManagerDeps struct {
	// apply pushes the full settings snapshot to the processor.
	apply func(snapshot settingsSnapshot)
	// log records a user-visible message about a settings change.
	log func(level, message string)
}

// newSettingsManager creates a settingsManager with default settings.
func newSettingsManager(deps settingsManagerDeps, extrafanartCfg bool, moveFilesCfg bool) settingsManager {
	return settingsManager{
		snapshot: settingsSnapshot{
			ScrapeEnabled:       true,
			DownloadEnabled:     true,
			DownloadExtrafanart: extrafanartCfg,
			OrganizeEnabled:     true,
			NFOEnabled:          true,
			MoveFiles:           moveFilesCfg,
		},
		cursor: 0,
		deps:   deps,
	}
}

// get returns the current settings snapshot (value copy).
func (sm *settingsManager) get() settingsSnapshot {
	return sm.snapshot
}

// cursorPos returns the current cursor position in the settings view.
func (sm *settingsManager) cursorPos() int {
	return sm.cursor
}

// moveCursor moves the settings cursor up or down, clamped to [0, maxSettings].
func (sm *settingsManager) moveCursor(delta int) {
	maxSettings := 9 // 0-9: 10 total settings
	sm.cursor += delta
	if sm.cursor < 0 {
		sm.cursor = 0
	} else if sm.cursor > maxSettings {
		sm.cursor = maxSettings
	}
}

// toggle flips the setting at the current cursor position and pushes
// the full snapshot to the processor. Returns a human-readable description
// of the change for logging.
func (sm *settingsManager) toggle() string {
	var desc string

	switch sm.cursor {
	case 0:
		sm.snapshot.DryRun = !sm.snapshot.DryRun
		if sm.snapshot.DryRun {
			desc = "Dry run mode enabled"
		} else {
			desc = "Dry run mode disabled"
		}

	case 1:
		sm.snapshot.ForceUpdate = !sm.snapshot.ForceUpdate
		if sm.snapshot.ForceUpdate {
			desc = "Force update enabled - will replace existing files"
		} else {
			desc = "Force update disabled"
		}

	case 2:
		sm.snapshot.ForceRefresh = !sm.snapshot.ForceRefresh
		if sm.snapshot.ForceRefresh {
			desc = "Force refresh enabled - will clear DB and rescrape"
		} else {
			desc = "Force refresh disabled"
		}

	case 3:
		sm.snapshot.MoveFiles = !sm.snapshot.MoveFiles
		if sm.snapshot.MoveFiles {
			desc = "Move mode enabled - files will be moved instead of copied"
		} else {
			desc = "Copy mode enabled - files will be copied"
		}

	case 4:
		sm.snapshot.ScrapeEnabled = !sm.snapshot.ScrapeEnabled
		if sm.snapshot.ScrapeEnabled {
			desc = "Metadata scraping enabled"
		} else {
			desc = "Metadata scraping disabled"
		}

	case 5:
		sm.snapshot.DownloadEnabled = !sm.snapshot.DownloadEnabled
		if sm.snapshot.DownloadEnabled {
			desc = "Media downloads enabled"
		} else {
			desc = "Media downloads disabled"
		}

	case 6:
		sm.snapshot.DownloadExtrafanart = !sm.snapshot.DownloadExtrafanart
		if sm.snapshot.DownloadExtrafanart {
			desc = "Extrafanart downloads enabled"
		} else {
			desc = "Extrafanart downloads disabled"
		}

	case 7:
		sm.snapshot.OrganizeEnabled = !sm.snapshot.OrganizeEnabled
		if sm.snapshot.OrganizeEnabled {
			desc = "File organization enabled"
		} else {
			desc = "File organization disabled"
		}

	case 8:
		sm.snapshot.NFOEnabled = !sm.snapshot.NFOEnabled
		if sm.snapshot.NFOEnabled {
			desc = "NFO generation enabled"
		} else {
			desc = "NFO generation disabled"
		}

	case 9:
		sm.snapshot.UpdateMode = !sm.snapshot.UpdateMode
		if sm.snapshot.UpdateMode {
			sm.snapshot.OrganizeEnabled = false
			desc = "Update mode enabled - files will remain in place, only metadata updated"
		} else {
			sm.snapshot.OrganizeEnabled = true
			desc = "Update mode disabled - file organization re-enabled"
		}
	}

	// Push the full snapshot to the processor
	sm.apply()

	return desc
}

// setDryRun sets the dry-run mode and pushes the snapshot.
func (sm *settingsManager) setDryRun(dryRun bool) {
	sm.snapshot.DryRun = dryRun
	sm.apply()
	if dryRun && sm.deps.log != nil {
		sm.deps.log("info", "DRY RUN mode enabled - no changes will be made")
	}
}

// setMoveFiles sets the move-files mode and pushes the snapshot.
func (sm *settingsManager) setMoveFiles(moveFiles bool) {
	sm.snapshot.MoveFiles = moveFiles
	sm.apply()
}

// setUpdateMode sets update mode and pushes the snapshot.
// When update mode is enabled, file organization is automatically disabled.
// When update mode is disabled, file organization is automatically re-enabled.
func (sm *settingsManager) setUpdateMode(updateMode bool) {
	sm.snapshot.UpdateMode = updateMode
	if updateMode {
		sm.snapshot.OrganizeEnabled = false
	} else {
		sm.snapshot.OrganizeEnabled = true
	}
	sm.apply()
}

// apply pushes the full snapshot to the processor via deps.
// This is the centralized push method — callers should use this after
// any direct mutation of sm.snapshot (e.g., from manualSearchDeps).
func (sm *settingsManager) apply() {
	if sm.deps.apply != nil {
		sm.deps.apply(sm.snapshot)
	}
}
