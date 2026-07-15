package tui

import (
	"fmt"
	"time"
)

// eventSubscriber owns the SortService and SortEventSubscriber wiring,
// settings pushing, and progress/stats updates. Extracted from Model to
// isolate the event-subscription concern from the Bubble Tea shell.
type eventSubscriber struct {
	sortSvc SortService

	// Shared state pointers — set during construction
	taskTracker *taskTracker
	taskList    *taskList
	dashboard   *dashboard
	header      *header

	// Statistics
	stats jobStats

	// Narrow deps — set during construction, not *Model back-reference
	deps eventSubscriberDeps
}

// eventSubscriberDeps holds the narrow interface the eventSubscriber
// needs from the parent Model, replacing a full *Model back-reference.
type eventSubscriberDeps struct {
	// addLog records a user-visible log message.
	addLog func(level, message string)
	// addConsoleOutput appends a line to the console panel.
	addConsoleOutput func(output string)
	// settingsMgrApply pushes the current settings snapshot to the sort service.
	settingsMgrApply func()
	// setManualSearchSortSvc wires the sort service into the manual search modal.
	setManualSearchSortSvc func(svc SortService)
	// setSettingsMgrDeps wires the settingsManager deps (apply + log).
	setSettingsMgrDeps func(deps settingsManagerDeps)
	// pushSettings is the Model method that pushes a settingsSnapshot to the sort service.
	pushSettings func(s settingsSnapshot)
	// getElapsed returns the elapsed time since processing started.
	getElapsed func() time.Duration
	// setStartTime records when processing started.
	setStartTime func(time.Time)
	// loc translates a message id (with optional template data) for the current
	// TUI locale. It is nil-safe so the subscriber cannot panic if the
	// localizer failed to construct at startup.
	loc func(id string, template ...map[string]any) string
}

// newEventSubscriber creates a subscriber wired to the given deps.
func newEventSubscriber(deps eventSubscriberDeps) eventSubscriber {
	return eventSubscriber{
		deps: deps,
	}
}

// setTaskTracker wires the shared taskTracker pointer.
func (es *eventSubscriber) setTaskTracker(tt *taskTracker) {
	es.taskTracker = tt
}

// setTaskList wires the taskList component for UI updates.
func (es *eventSubscriber) setTaskList(tl *taskList) {
	es.taskList = tl
}

// setDashboard wires the dashboard component for UI updates.
func (es *eventSubscriber) setDashboard(d *dashboard) {
	es.dashboard = d
}

// setHeader wires the header component for UI updates.
func (es *eventSubscriber) setHeader(h *header) {
	es.header = h
}

// SetSortService sets the sort service and wires the settingsManager
// deps to push settings changes to the sort service.
func (es *eventSubscriber) SetSortService(svc SortService) {
	es.sortSvc = svc
	// Wire settingsManager deps now that sort service is available
	es.deps.setSettingsMgrDeps(settingsManagerDeps{
		apply: func(s settingsSnapshot) { es.pushSettingsToSortService(s) },
		log:   es.deps.addLog,
	})
	// Push the full settings snapshot to the sort service
	es.deps.settingsMgrApply()
	// Wire the sort service into manualSearchDeps so the modal can reach it directly
	es.deps.setManualSearchSortSvc(svc)
}

// SetEventSubscriber sets the JobEvent subscriber for progress updates.
func (es *eventSubscriber) SetEventSubscriber(sub SortEventSubscriber) {
	es.taskTracker.eventSub = sub
}

// pushSettingsToSortService pushes the full settings snapshot to the sort service.
// This replaces individual Set* methods so that adding a new toggle
// only requires updating settingsSnapshot and settingsManager.toggle.
func (es *eventSubscriber) pushSettingsToSortService(s settingsSnapshot) {
	if es.sortSvc == nil {
		return
	}
	opts := es.sortSvc.LoadOptions()
	opts.DryRun = s.DryRun
	opts.ForceUpdate = s.ForceUpdate
	opts.ForceRefresh = s.ForceRefresh
	opts.MoveFiles = s.MoveFiles
	opts.ScrapeEnabled = s.ScrapeEnabled
	opts.DownloadEnabled = s.DownloadEnabled
	opts.DownloadExtrafanartOverride = s.DownloadExtrafanart
	opts.OrganizeEnabled = s.OrganizeEnabled
	opts.NFOEnabled = s.NFOEnabled
	opts.UpdateMode = s.UpdateMode
	es.sortSvc.SetOptions(opts)
}

// UpdateProgress updates task progress from a JobEvent.
func (es *eventSubscriber) UpdateProgress(event SortEvent) {
	taskID := event.MovieID
	if taskID == "" {
		return
	}

	es.taskTracker.updateProgress(es.resolveEvent(event))

	// Push updated task data to taskList (single source of truth: taskTracker.tasks)
	if es.taskList != nil {
		es.taskList.SetTasks(es.taskTracker.tasks, es.taskTracker.taskOrder)
	}

	// Add to console output
	if event.Message != "" {
		displayMsg := es.eventMessage(event)
		consoleMsg := fmt.Sprintf("[%s] %s", taskID, displayMsg)
		es.deps.addConsoleOutput(consoleMsg)
	}

	// Log progress if significant
	switch event.Step {
	case taskStepComplete:
		es.deps.addLog("info", es.eventMessage(event))
	case taskStepFailed:
		es.deps.addLog("error", es.eventMessage(event))
	}
}

// eventMessage resolves the user-facing text for a sort event. When the event
// carries a structured Code, it is translated via the localizer (unknown codes
// or a nil localizer fall back to the English Message). When Code is empty, the
// raw Message is returned unchanged.
func (es *eventSubscriber) eventMessage(event SortEvent) string {
	if event.Code != "" && es.deps.loc != nil {
		return es.deps.loc(event.Code, event.Args)
	}
	return event.Message
}

// resolveEvent returns a copy of event with its Message replaced by the
// localized display text and Code cleared, so downstream pure handlers
// (handleSortEvent) store the resolved message without needing a localizer.
func (es *eventSubscriber) resolveEvent(event SortEvent) SortEvent {
	if event.Code == "" || es.deps.loc == nil {
		return event
	}
	resolved := event
	resolved.Message = es.deps.loc(event.Code, event.Args)
	resolved.Code = ""
	return resolved
}

// UpdateStats updates statistics.
func (es *eventSubscriber) UpdateStats(stats jobStats) {
	es.stats = stats

	if es.dashboard != nil {
		es.dashboard.UpdateStats(stats, es.deps.getElapsed())
	}
	if es.header != nil {
		es.header.UpdateStats(stats)
	}
}

// Stats returns the current job statistics.
func (es *eventSubscriber) Stats() jobStats {
	return es.stats
}
