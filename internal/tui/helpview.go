package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// helpView component
type helpView struct {
	width  int
	height int
}

func newHelpView() *helpView {
	return &helpView{}
}

func (h *helpView) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h *helpView) Init() tea.Cmd {
	return nil
}

func (h *helpView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return h, nil
}

func (h *helpView) View() string {
	help := title("Help") + "\n\n"

	help += helpKeyStyle.Render("Global Keys") + "\n"
	help += "  ? - Toggle help\n"
	help += "  q/Ctrl+C - Quit\n"
	help += "  1-3 - Switch views\n"
	help += "  Tab - Cycle views\n\n"

	help += helpKeyStyle.Render("browser View") + "\n"
	help += "  m - Manual search (select scrapers + ID/URL)\n"
	help += "  f - Change scan folder\n"
	help += "  r - Refresh/rescan current folder\n"
	help += "  ↑/k - Move up\n"
	help += "  ↓/j - Move down\n"
	help += "  Space - Toggle selection (files or entire folders)\n"
	help += "  a - Select all\n"
	help += "  A - Deselect all\n"
	help += "  Enter - Start processing\n"
	help += "  p - Pause/resume\n\n"

	help += helpKeyStyle.Render("Logs View") + "\n"
	help += "  ↑/k - Scroll up\n"
	help += "  ↓/j - Scroll down\n"
	help += "  g - Go to top\n"
	help += "  G - Go to bottom\n"
	help += "  a - Toggle auto-scroll\n\n"

	help += dimmed("📁 Folders with checkboxes select all files inside")

	return help
}
