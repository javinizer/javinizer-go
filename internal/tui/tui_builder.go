package tui

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
)

// wireModel initializes all sub-controllers, modals, and components on the Model.
// Extracted from New() so that the constructor remains a simple struct literal
// plus a single wiring call. Adding a new sub-controller or modal only requires
// changes here, not in the New() function itself.
func wireModel(m *Model) {
	// Initialize sub-controllers (P-1 decomposition)
	m.processingCtl = newProcessingController(processingControllerDeps{
		addLog:           m.AddLog,
		addConsoleOutput: m.AddConsoleOutput,
		browserState:     func() browserState { return m.browserState },
		setStartTime:     func(t time.Time) { m.startTime = t },
	})
	m.browserCtl = newBrowserController(browserControllerDeps{
		addLog: m.AddLog,
		setSortSvcDestPath: func(path string) {
			if m.eventSub.sortSvc != nil {
				opts := m.eventSub.sortSvc.LoadOptions()
				opts.DestPath = path
				m.eventSub.sortSvc.SetOptions(opts)
			}
		},
	})
	m.eventSub = newEventSubscriber(eventSubscriberDeps{
		addLog:                 m.AddLog,
		addConsoleOutput:       m.AddConsoleOutput,
		settingsMgrApply:       m.settingsMgr.apply,
		setManualSearchSortSvc: func(svc SortService) { m.manualSearch.deps.SortSvc = svc },
		setSettingsMgrDeps:     func(deps settingsManagerDeps) { m.settingsMgr.deps = deps },
		pushSettings:           m.pushSettingsToSortService,
		getElapsed:             func() time.Duration { return time.Since(m.startTime) },
		setStartTime:           func(t time.Time) { m.startTime = t },
	})

	// Wire shared state pointers into sub-controllers
	m.processingCtl.setTaskTracker(&m.taskTracker)
	m.processingCtl.setLogState(&m.logState)
	m.browserCtl.setBrowserState(&m.browserState)
	m.eventSub.setTaskTracker(&m.taskTracker)

	// Initialize modals via standalone constructors (narrow deps, not *Model)
	m.manualSearch = newManualSearchModal(manualSearchDeps{
		AddLog:          m.AddLog,
		SortSvc:         nil, // Set later via SetSortService
		OrganizeEnabled: func() bool { return m.settingsMgr.get().OrganizeEnabled },
		NFOEnabled:      func() bool { return m.settingsMgr.get().NFOEnabled },
		SetCurrentView:  func(v viewMode) { m.viewMgr.switchTo(v) },
		Width:           func() int { return m.width },
		Height:          func() int { return m.height },
	})
	m.actressMergeCtl = newActressMergeController(newActressMergeModal(actressMergeDeps{
		AddLog: m.AddLog,
		ActressRepo: func() database.ActressRepositoryInterface {
			return m.actressRepo
		},
		Width:  func() int { return m.width },
		Height: func() int { return m.height },
	}))
	m.folderPickCtl = newFolderPickerController(newFolderPickerDeps(folderPickerDeps{
		AddLog:        m.AddLog,
		SetDestPath:   m.SetDestPath,
		SetSourcePath: m.SetSourcePath,
		Width:         func() int { return m.width },
		Height:        func() int { return m.height },
	}))

	// Initialize components
	m.header = newHeader()
	m.browser = newBrowser()
	m.taskList = newTaskList()
	m.console = newConsole()
	m.dashboard = newDashboard()
	m.logViewer = newLogViewer()
	m.settingsView = newSettingsView()
	m.helpView = newHelpView()

	// Wire components into sub-controllers
	m.processingCtl.setBrowser(m.browser)
	m.processingCtl.setTaskList(m.taskList)
	m.processingCtl.setConsole(m.console)
	m.browserCtl.setBrowser(m.browser)
	m.eventSub.setTaskList(m.taskList)
	m.eventSub.setDashboard(m.dashboard)
	m.eventSub.setHeader(m.header)
}
