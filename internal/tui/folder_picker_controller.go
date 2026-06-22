package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// folderPickerController owns the folderPickerModal and exposes the
// operations the Model needs: open, close, and Bubble Tea delegation.
// This replaces the former direct field access + delegate methods on Model,
// reducing the Model's method count and grouping related concerns.
type folderPickerController struct {
	modal folderPickerModal
}

// newFolderPickerController creates a controller wired to the given deps.
func newFolderPickerController(deps folderPickerDeps) folderPickerController {
	return folderPickerController{
		modal: folderPickerModal{
			showing: false,
			deps:    deps,
		},
	}
}

// Open opens the folder picker at startPath in the given mode ("source" or "dest").
func (fc *folderPickerController) Open(startPath, mode string) {
	fc.modal.open(startPath, mode)
}

// Close closes the folder picker modal.
func (fc *folderPickerController) Close() {
	fc.modal.close()
}

// Showing returns whether the folder picker modal is currently displayed.
func (fc *folderPickerController) Showing() bool {
	return fc.modal.showing
}

// Update delegates a Bubble Tea message to the modal and returns the updated state.
func (fc *folderPickerController) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return fc.modal.Update(msg)
}

// View renders the folder picker modal overlay.
func (fc *folderPickerController) View() string {
	return fc.modal.View()
}
