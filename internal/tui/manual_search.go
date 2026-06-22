//nolint:goconst
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

// newManualSearchModal constructs the manual search modal with the provided deps.
// Accepts only the narrow deps interface instead of *Model.
func newManualSearchModal(deps manualSearchDeps) manualSearchModal {
	manualSearchInput := textinput.New()
	manualSearchInput.Placeholder = "Enter JAV ID or URL"
	manualSearchInput.CharLimit = 200
	manualSearchInput.Width = 50

	return manualSearchModal{
		input:             manualSearchInput,
		scraperCheckboxes: make(map[string]bool),
		cursor:            0,
		focusOnInput:      true,
		showing:           false,
		deps:              deps,
	}
}

// Init is a no-op; manual search modals do not issue commands on init.
func (ms *manualSearchModal) Init() tea.Cmd { return nil }

// Update processes Bubble Tea messages for the manual search modal.
func (ms *manualSearchModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return ms, nil
	}
	switch keyMsg.String() {
	case "esc":
		ms.showing = false
		ms.input.Blur()
		return ms, nil
	case "tab":
		ms.focusOnInput = !ms.focusOnInput
		if ms.focusOnInput {
			ms.input.Focus()
		} else {
			ms.input.Blur()
		}
		return ms, nil
	case "up":
		if !ms.focusOnInput && ms.cursor > 0 {
			ms.cursor--
		}
		return ms, nil
	case "down":
		if !ms.focusOnInput && len(ms.scraperList) > 0 {
			maxCursor := len(ms.scraperList) - 1
			if ms.cursor < maxCursor {
				ms.cursor++
			}
		}
		return ms, nil
	case " ":
		if !ms.focusOnInput && len(ms.scraperList) > 0 && ms.cursor < len(ms.scraperList) {
			scraperName := ms.scraperList[ms.cursor]
			ms.scraperCheckboxes[scraperName] = !ms.scraperCheckboxes[scraperName]
		}
		return ms, nil
	case "enter":
		return ms.executeManualSearch(), nil
	}
	if ms.focusOnInput {
		var cmd tea.Cmd
		ms.input, cmd = ms.input.Update(msg)
		return ms, cmd
	}
	return ms, nil
}

func (ms *manualSearchModal) executeManualSearch() *manualSearchModal {
	input := strings.TrimSpace(ms.input.Value())
	if input == "" {
		return ms
	}
	if ms.deps.SortSvc == nil {
		ms.deps.AddLog("error", "Sort service not initialized")
		return ms
	}
	selectedScrapers := []string{}
	for scraper, checked := range ms.scraperCheckboxes {
		if checked {
			selectedScrapers = append(selectedScrapers, scraper)
		}
	}
	if len(selectedScrapers) == 0 {
		return ms
	}
	parsed, err := matcher.ParseInput(input, ms.deps.SortSvc.Registry())
	if err != nil {
		ms.deps.AddLog("error", fmt.Sprintf("Invalid input: %v", err))
		return ms
	}
	if parsed.IsURL && parsed.ScraperHint != "" {
		selectedScrapers = reorderWithPriority(selectedScrapers, parsed.ScraperHint)
	}
	ms.deps.SortSvc.SetCustomScrapers(selectedScrapers)
	originalOrganize := ms.deps.OrganizeEnabled()
	originalNFO := ms.deps.NFOEnabled()
	opts := ms.deps.SortSvc.LoadOptions()
	opts.OrganizeEnabled = false
	opts.NFOEnabled = false
	ms.deps.SortSvc.SetOptions(opts)
	manualMatch := models.FileMatchInfo{Path: "manual-search", Name: parsed.ID, MovieID: parsed.ID}
	ctx := context.Background()
	files := []fileItem{{Path: "manual-search", Name: parsed.ID, Matched: true, ID: parsed.ID}}
	matches := map[string]models.FileMatchInfo{"manual-search": manualMatch}
	if err := ms.deps.SortSvc.ProcessFiles(ctx, files, matches); err != nil {
		ms.deps.AddLog("error", fmt.Sprintf("Failed to start manual search: %v", err))
	} else {
		ms.deps.AddLog("info", fmt.Sprintf("Started manual search for %s with scrapers: %v (metadata + downloads only)", parsed.ID, selectedScrapers))
	}
	ms.showing = false
	ms.input.SetValue("")
	ms.input.Blur()
	ms.deps.SortSvc.SetCustomScrapers(nil)
	opts = ms.deps.SortSvc.LoadOptions()
	opts.OrganizeEnabled = originalOrganize
	opts.NFOEnabled = originalNFO
	ms.deps.SortSvc.SetOptions(opts)
	ms.deps.SetCurrentView(viewDashboard)
	return ms
}

// View renders the manual search modal overlay.
func (ms *manualSearchModal) View() string {
	modalStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(1, 2).Width(60).Height(20)
	msTitleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62")).MarginBottom(1)
	scraperList := []string{}
	for i, scraperName := range ms.scraperList {
		checked := ms.scraperCheckboxes[scraperName]
		cursor := "  "
		if !ms.focusOnInput && i == ms.cursor {
			cursor = "▸ "
		}
		checkbox := "[ ]"
		if checked {
			checkbox = "[✓]"
		}
		scraperList = append(scraperList, fmt.Sprintf("%s%s %s", cursor, checkbox, scraperName))
	}
	inputLabel := "Search: "
	if ms.focusOnInput {
		inputLabel = "▸ Search: "
	}
	instructions := "Tab: Switch Focus  Space: Toggle  Enter: Search  Esc: Cancel"
	content := strings.Join([]string{msTitleStyle.Render("Manual Search"), "", inputLabel + ms.input.View(), "", "Select Scrapers:", strings.Join(scraperList, "\n"), "", lipgloss.NewStyle().Faint(true).Render(instructions)}, "\n")
	modal := modalStyle.Render(content)
	return lipgloss.Place(ms.deps.Width(), ms.deps.Height(), lipgloss.Center, lipgloss.Center, modal)
}
