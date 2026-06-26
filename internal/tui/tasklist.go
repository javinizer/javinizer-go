package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// taskList component — stateless renderer. Data is owned by Model and
// pushed via SetTasks before each render.
type taskList struct {
	// Render snapshot — set by Model.SetTasks before each View call.
	// These fields must NOT be read or mutated outside of SetTasks/View;
	// the Model is the single source of truth.
	tasks  map[string]*taskState
	order  []string
	width  int
	height int
}

func newTaskList() *taskList {
	return &taskList{}
}

func (t *taskList) SetSize(width, height int) {
	t.width = width
	t.height = height
}

// SetTasks replaces the task data snapshot used for rendering.
// The Model owns the canonical task state and pushes it before each View call.
func (t *taskList) SetTasks(tasks map[string]*taskState, order []string) {
	t.tasks = tasks
	t.order = order
}

func (t *taskList) Init() tea.Cmd {
	return nil
}

func (t *taskList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return t, nil
}

func (t *taskList) View() string {
	view := title("Tasks") + "\n\n"

	if len(t.tasks) == 0 {
		return view + dimmed("No active tasks")
	}

	// Show last N tasks
	start := len(t.order) - (t.height - 4)
	if start < 0 {
		start = 0
	}

	for i := start; i < len(t.order); i++ {
		taskID := t.order[i]
		task := t.tasks[taskID]
		// Guard against order/tasks skew (stale order entry, concurrent
		// reorder) so a missing task skips rendering instead of nil-deref
		// panicking the whole TUI render loop.
		if task == nil {
			continue
		}

		status := ""
		switch task.Step {
		case taskStepComplete:
			status = successBadge.Render("OK")
		case taskStepFailed:
			status = errorBadge.Render("ERR")
		default:
			if task.Progress > 0 {
				status = runningBadge.Render("RUN")
			} else {
				status = infoBadge.Render("...")
			}
		}

		progress := renderProgressBar(task.Progress, 20)
		view += fmt.Sprintf("%s %s %s\n", status, progress, task.ID)
	}

	return view
}

// renderProgressBar renders a text-based progress bar.
func renderProgressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled

	bar := progressBarStyle.Render(strings.Repeat("█", filled))
	bar += progressEmptyStyle.Render(strings.Repeat("░", empty))

	return fmt.Sprintf("[%s] %3.0f%%", bar, progress*100)
}
