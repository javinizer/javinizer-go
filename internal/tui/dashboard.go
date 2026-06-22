package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// dashboard component
type dashboard struct {
	stats       jobStats
	elapsedTime time.Duration
	width       int
	height      int
}

func newDashboard() *dashboard {
	return &dashboard{}
}

func (d *dashboard) SetSize(width, height int) {
	d.width = width
	d.height = height
}

func (d *dashboard) UpdateStats(stats jobStats, elapsed time.Duration) {
	d.stats = stats
	d.elapsedTime = elapsed
}

func (d *dashboard) Init() tea.Cmd {
	return nil
}

func (d *dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return d, nil
}

func (d *dashboard) View() string {
	view := title("dashboard") + "\n\n"

	view += fmt.Sprintf("Total:     %d\n", d.stats.Total)
	view += fmt.Sprintf("Running:   %s\n", runningBadge.Render(fmt.Sprintf("%d", d.stats.Running)))
	view += fmt.Sprintf("success:   %s\n", success(fmt.Sprintf("%d", d.stats.success)))
	view += fmt.Sprintf("Failed:    %s\n", errorStyled(fmt.Sprintf("%d", d.stats.Failed)))
	view += fmt.Sprintf("\nProgress:  %.1f%%\n", d.stats.OverallProgress*100)
	view += fmt.Sprintf("Elapsed:   %v\n", d.elapsedTime.Round(time.Second))

	return view
}
