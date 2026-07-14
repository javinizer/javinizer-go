package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/tui/localization"
)

// dashboard component
type dashboard struct {
	stats       jobStats
	elapsedTime time.Duration
	width       int
	height      int
	localizer   *localization.Localizer
}

func newDashboard() *dashboard {
	return &dashboard{}
}

func (d *dashboard) SetSize(width, height int) {
	d.width = width
	d.height = height
}

func (d *dashboard) SetLocalizer(l *localization.Localizer) {
	d.localizer = l
}

//nolint:unparam // variadic for API consistency with other components
func (d *dashboard) loc(id string, template ...map[string]any) string {
	if d.localizer == nil {
		return id
	}
	return d.localizer.Localize(id, template...)
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
	view := title(d.loc("TUIDashboardTitle")) + "\n\n"

	view += fmt.Sprintf("%-10s %d\n", d.loc("TUIDashTotal"), d.stats.Total)
	view += fmt.Sprintf("%-10s %s\n", d.loc("TUIDashRunning"), runningBadge.Render(fmt.Sprintf("%d", d.stats.Running)))
	view += fmt.Sprintf("%-10s %s\n", d.loc("TUIDashSuccess"), success(fmt.Sprintf("%d", d.stats.success)))
	view += fmt.Sprintf("%-10s %s\n", d.loc("TUIDashFailed"), errorStyled(fmt.Sprintf("%d", d.stats.Failed)))
	view += fmt.Sprintf("\n%-10s %.1f%%\n", d.loc("TUIDashProgress"), d.stats.OverallProgress*100)
	view += fmt.Sprintf("%-10s %v\n", d.loc("TUIDashElapsed"), d.elapsedTime.Round(time.Second))

	return view
}
