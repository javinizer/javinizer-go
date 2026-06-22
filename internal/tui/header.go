package tui

import tea "github.com/charmbracelet/bubbletea"

// header component
type header struct {
	width int
	stats jobStats
}

func newHeader() *header {
	return &header{}
}

func (h *header) SetWidth(width int) {
	h.width = width
}

func (h *header) UpdateStats(stats jobStats) {
	h.stats = stats
}

func (h *header) View() string {
	// header content is rendered in view.go; this component keeps a minimal fallback.
	return headerStyle.Render("Javinizer TUI")
}

// Ensure header satisfies tea.Model at compile time.
var _ tea.Model = (*header)(nil)

func (h *header) Init() tea.Cmd                           { return nil }
func (h *header) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return h, nil }
