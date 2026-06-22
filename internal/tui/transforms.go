package tui

// Task step constants — TUI-local values matching worker.JobEventStep values.
// Defined here so the TUI package does not import the worker package
// for these two constants.
const (
	taskStepComplete = sortStepComplete
	taskStepFailed   = sortStepFailed
)
