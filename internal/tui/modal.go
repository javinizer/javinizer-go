package tui

import tea "github.com/charmbracelet/bubbletea"

// Modal is the interface that all TUI modal overlays implement.
type Modal interface {
	Update(msg tea.Msg) (tea.Model, tea.Cmd)
	View() string
}
