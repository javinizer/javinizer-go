package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderManualSearchModal renders the manual search modal overlay
func RenderManualSearchModal(m *Model) string {
	// Modal styling
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(60).
		Height(20)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("62")).
		MarginBottom(1)

	// Build scraper checkbox list using cached sorted list
	scraperList := []string{}

	for i, scraperName := range m.scraperList {
		checked := m.scraperCheckboxes[scraperName]

		cursor := "  "
		if !m.focusOnInput && i == m.manualSearchCursor {
			cursor = "▸ "
		}

		checkbox := "[ ]"
		if checked {
			checkbox = "[✓]"
		}

		scraperList = append(scraperList,
			fmt.Sprintf("%s%s %s", cursor, checkbox, scraperName))
	}

	// Build input field display
	inputLabel := "Search: "
	if m.focusOnInput {
		inputLabel = "▸ Search: "
	}

	// Instructions
	instructions := "Tab: Switch Focus  Space: Toggle  Enter: Search  Esc: Cancel"

	// Build modal content
	content := strings.Join([]string{
		titleStyle.Render("Manual Search"),
		"",
		inputLabel + m.manualSearchInput.View(),
		"",
		"Select Scrapers:",
		strings.Join(scraperList, "\n"),
		"",
		lipgloss.NewStyle().Faint(true).Render(instructions),
	}, "\n")

	// Center the modal on screen
	modal := modalStyle.Render(content)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}
