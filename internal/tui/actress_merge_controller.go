package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// actressMergeController owns the actressMergeModal and exposes the
// operations the Model needs: open, loadPreview, and Bubble Tea delegation.
// This replaces the former direct field access + delegate methods on Model,
// reducing the Model's method count and grouping related concerns.
type actressMergeController struct {
	modal actressMergeModal
}

// newActressMergeController creates a controller with the given modal state.
func newActressMergeController(modal actressMergeModal) actressMergeController {
	return actressMergeController{modal: modal}
}

// Open opens the actress merge modal.
func (ac *actressMergeController) Open() {
	ac.modal.open()
}

// LoadPreview loads the merge preview for the current target/source inputs.
func (ac *actressMergeController) LoadPreview() error {
	return ac.modal.loadPreview()
}

// Showing returns whether the actress merge modal is currently displayed.
func (ac *actressMergeController) Showing() bool {
	return ac.modal.showing
}

// Update delegates a Bubble Tea message to the modal and returns the updated state.
func (ac *actressMergeController) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return ac.modal.Update(msg)
}

// View renders the actress merge modal overlay.
func (ac *actressMergeController) View() string {
	return ac.modal.View()
}
