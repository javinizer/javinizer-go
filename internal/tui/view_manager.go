package tui

// viewManager manages the current TUI view and view-switching logic.
// Extracted from Model to localize all view-related state and transitions.
// Pure functions (switchToView, cycleView, toggleHelp) are now methods,
// making the view concern independently testable without a *Model back-reference.
type viewManager struct {
	current  viewMode
	previous viewMode // View before entering Help, for toggle-back
}

// newViewManager creates a viewManager defaulting to the browser view.
func newViewManager() viewManager {
	return viewManager{
		current:  viewBrowser,
		previous: viewBrowser,
	}
}

// currentView returns the active view mode.
func (vm *viewManager) currentView() viewMode {
	return vm.current
}

// switchTo switches to the specified view if it is valid and different.
// Resets cursor on successful switch (callers should reset their own cursor state).
func (vm *viewManager) switchTo(view viewMode) {
	if view == vm.current {
		return
	}
	if !isValidView(view) {
		return
	}
	vm.previous = vm.current
	vm.current = view
}

// cycle advances to the next view in sequence (browser → dashboard → logs → settings → browser),
// skipping the Help view (accessed via toggleHelp only).
func (vm *viewManager) cycle() {
	next := (vm.current + 1) % viewModeCount
	if next == viewHelp {
		next = viewBrowser
	}
	vm.previous = vm.current
	vm.current = next
}

// toggleHelp toggles between the current view and the Help view.
// When entering Help, the previous view is remembered so toggleHelp can return.
func (vm *viewManager) toggleHelp() {
	if vm.current == viewHelp {
		vm.current = vm.previous
	} else {
		vm.previous = vm.current
		vm.current = viewHelp
	}
}
