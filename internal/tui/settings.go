package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// settingsView component
type settingsView struct {
	width    int
	height   int
	cursor   int
	settings settingsSnapshot
}

func newSettingsView() *settingsView {
	return &settingsView{
		settings: settingsSnapshot{
			ScrapeEnabled:   true,
			DownloadEnabled: true,
			OrganizeEnabled: true,
			NFOEnabled:      true,
		},
	}
}

func (s *settingsView) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func (s *settingsView) Init() tea.Cmd {
	return nil
}

func (s *settingsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return s, nil
}

func (s *settingsView) UpdateSettings(cursor int, settings settingsSnapshot) {
	s.cursor = cursor
	s.settings = settings
}

func (s *settingsView) View() string {
	view := title("Settings") + " " + dimmed("(space to toggle)") + "\n\n"

	settings := []struct {
		index   int
		name    string
		desc    string
		enabled bool
	}{
		{0, "Dry Run", "Preview mode - don't make actual changes", s.settings.DryRun},
		{1, "Force Update", "Replace existing files (images, NFO)", s.settings.ForceUpdate},
		{2, "Force Refresh", "Clear DB cache and rescrape metadata", s.settings.ForceRefresh},
		{3, "Move Files", "Move instead of copy (default: copy)", s.settings.MoveFiles},
		{4, "Scrape Metadata", "Fetch metadata from JAV sources", s.settings.ScrapeEnabled},
		{5, "Download Media", "Download covers, screenshots, trailers", s.settings.DownloadEnabled},
		{6, "Download Extrafanart", "Download extrafanart/screenshots to subfolder", s.settings.DownloadExtrafanart},
		{7, "Organize Files", "Move/copy files to organized structure", s.settings.OrganizeEnabled},
		{8, "Generate NFO", "Create NFO files for media centers", s.settings.NFOEnabled},
		{9, "Update Mode", "Only create/update metadata, don't move files", s.settings.UpdateMode},
	}

	for _, setting := range settings {
		cursorStr := "  "
		if s.cursor == setting.index {
			cursorStr = "> "
		}

		checkbox := "☐"
		if setting.enabled {
			checkbox = success("☑")
		}

		view += fmt.Sprintf("%s%s %s\n", cursorStr, checkbox, helpKeyStyle.Render(setting.name))
		view += fmt.Sprintf("   %s\n\n", dimmed(setting.desc))
	}

	view += "\n" + dimmed("Changes take effect on next processing run")

	return view
}
