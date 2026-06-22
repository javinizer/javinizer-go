package tui

import "github.com/javinizer/javinizer-go/internal/models"

// browserController owns file browser state and operations: setting files,
// toggling selection, retrieving selected files, expanding directory selections,
// and building the file tree. Extracted from Model to isolate browser
// concerns from the Bubble Tea shell.
type browserController struct {
	state   *browserState
	browser *browser

	// Narrow deps — set during construction, not *Model back-reference
	deps browserControllerDeps
}

// browserControllerDeps holds the narrow interface the browserController
// needs from the parent Model, replacing a full *Model back-reference.
type browserControllerDeps struct {
	// addLog records a user-visible log message.
	addLog func(level, message string)
	// setSortSvcDestPath pushes the destination path to the sort service.
	setSortSvcDestPath func(path string)
}

// newBrowserController creates a controller wired to the given deps.
func newBrowserController(deps browserControllerDeps) browserController {
	return browserController{
		deps: deps,
	}
}

// setBrowserState wires the shared browserState pointer.
func (bc *browserController) setBrowserState(bs *browserState) {
	bc.state = bs
}

// setBrowser wires the browser component for UI updates.
func (bc *browserController) setBrowser(b *browser) {
	bc.browser = b
}

// SetFiles sets the files to display in the browser.
func (bc *browserController) SetFiles(files []fileItem) {
	bc.state.files = files
	if bc.browser != nil {
		bc.browser.SetItems(files)
	}
}

// SetSourcePath sets the source path being scanned.
func (bc *browserController) SetSourcePath(path string) {
	bc.state.setSourcePath(path)
	if bc.browser != nil {
		bc.browser.SetSourcePath(path)
	}
}

// SetDestPath sets the destination path for organized files.
func (bc *browserController) SetDestPath(path string) {
	bc.state.setDestPath(path)
	bc.deps.setSortSvcDestPath(path)
	if bc.browser != nil {
		bc.browser.SetDestPath(path)
	}
}

// GetDestPath returns the destination path.
func (bc *browserController) GetDestPath() string {
	return bc.state.destPath
}

// ToggleFileSelection toggles selection of a file.
func (bc *browserController) ToggleFileSelection(path string) {
	bc.state.toggleFileSelection(path)
	if bc.browser != nil {
		bc.browser.ToggleSelection(path)
	}
}

// GetSelectedFiles returns the list of selected files.
func (bc *browserController) GetSelectedFiles() []string {
	return bc.state.getSelectedFiles()
}

// SetMatchResults sets the match results for files.
func (bc *browserController) SetMatchResults(matches map[string]models.FileMatchInfo) {
	bc.state.matchResults = matches
}
