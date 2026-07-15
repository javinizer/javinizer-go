package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/tui/localization"
)

// settingRowType discriminates the two kinds of settings rows.
const (
	settingRowToggle settingRowType = "toggle"
	settingRowChoice settingRowType = "choice"
)

// Row index constants. Toggles occupy 0-9; the language choice row is 10.
const (
	settingLanguageRow = 10
)

type settingRowType string

// settingRow describes one rendered settings row. Toggle rows read their
// enabled state from settingsSnapshot; choice rows read their current value
// from the separate language field and cycle through Choices.
type settingRow struct {
	index   int
	nameKey string
	descKey string
	typ     settingRowType
	choices []string
}

// supportedLanguages returns the locale choices offered in the TUI. Only
// locales that ship a catalog should appear here; "auto" resolves the OS
// locale preference list at localizer construction time.
func supportedLanguages() []string {
	return []string{"auto", "en", "ja", "zh-Hans", "zh-Hant"}
}

// languageDisplayName returns the self-name shown in the selector. Explicit
// locales use their endonym (e.g. "English") so the picker stays usable when
// the active locale is wrong; "auto" is localized since it is chrome.
func (s *settingsView) languageDisplayName(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto":
		return s.loc("TUISettingsLanguageAuto")
	case "en":
		return "English"
	case "ja":
		return "日本語"
	case "zh-hans":
		return "简体中文"
	case "zh-hant":
		return "繁體中文"
	default:
		return lang
	}
}

// settingsView component
type settingsView struct {
	width     int
	height    int
	cursor    int
	settings  settingsSnapshot
	language  string
	localizer *localization.Localizer
}

func newSettingsView() *settingsView {
	return &settingsView{
		settings: settingsSnapshot{
			ScrapeEnabled:   true,
			DownloadEnabled: true,
			OrganizeEnabled: true,
			NFOEnabled:      true,
		},
		language: "auto",
	}
}

func (s *settingsView) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func (s *settingsView) SetLocalizer(l *localization.Localizer) {
	s.localizer = l
}

//nolint:unparam // variadic for API consistency with other components
func (s *settingsView) loc(id string, template ...map[string]any) string {
	if s.localizer == nil {
		return id
	}
	return s.localizer.Localize(id, template...)
}

func (s *settingsView) Init() tea.Cmd {
	return nil
}

func (s *settingsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return s, nil
}

func (s *settingsView) UpdateSettings(cursor int, settings settingsSnapshot, language string) {
	s.cursor = cursor
	s.settings = settings
	s.language = language
}

// settingRows builds the ordered row list. Toggle rows mirror the legacy
// 0-9 ordering so existing cursor indices and tests keep working; the
// language choice row is appended at settingLanguageRow.
func (s *settingsView) settingRows() []settingRow {
	return []settingRow{
		{0, "TUISettingDryRun", "TUISettingDryRunDesc", settingRowToggle, nil},
		{1, "TUISettingForceUpdate", "TUISettingForceUpdateDesc", settingRowToggle, nil},
		{2, "TUISettingForceRefresh", "TUISettingForceRefreshDesc", settingRowToggle, nil},
		{3, "TUISettingMoveFiles", "TUISettingMoveFilesDesc", settingRowToggle, nil},
		{4, "TUISettingScrape", "TUISettingScrapeDesc", settingRowToggle, nil},
		{5, "TUISettingDownload", "TUISettingDownloadDesc", settingRowToggle, nil},
		{6, "TUISettingExtrafanart", "TUISettingExtrafanartDesc", settingRowToggle, nil},
		{7, "TUISettingOrganize", "TUISettingOrganizeDesc", settingRowToggle, nil},
		{8, "TUISettingNFO", "TUISettingNFODesc", settingRowToggle, nil},
		{9, "TUISettingUpdateMode", "TUISettingUpdateModeDesc", settingRowToggle, nil},
		{settingLanguageRow, "TUISettingLanguage", "TUISettingLanguageDesc", settingRowChoice, supportedLanguages()},
	}
}

func (s *settingsView) View() string {
	view := title(s.loc("TUISettingsTitle")) + " " +
		dimmed(s.loc("TUISettingsToggleHint")) + "  " +
		dimmed(s.loc("TUISettingsChoiceHint")) + "\n\n"

	for _, row := range s.settingRows() {
		cursorStr := "  "
		if s.cursor == row.index {
			cursorStr = "> "
		}

		var value string
		switch row.typ {
		case settingRowToggle:
			checkbox := "☐"
			if s.toggleEnabled(row.index) {
				checkbox = success("☑")
			}
			value = checkbox
		case settingRowChoice:
			value = fmt.Sprintf("‹ %s ›", highlight(s.languageDisplayName(s.language)))
		}

		view += fmt.Sprintf("%s%s %s\n", cursorStr, value, helpKeyStyle.Render(s.loc(row.nameKey)))
		view += fmt.Sprintf("   %s\n\n", dimmed(s.loc(row.descKey)))
	}

	view += "\n" + dimmed(s.loc("TUISettingChangesHint"))

	return view
}

// toggleEnabled reports the boolean state for a toggle row index.
func (s *settingsView) toggleEnabled(index int) bool {
	switch index {
	case 0:
		return s.settings.DryRun
	case 1:
		return s.settings.ForceUpdate
	case 2:
		return s.settings.ForceRefresh
	case 3:
		return s.settings.MoveFiles
	case 4:
		return s.settings.ScrapeEnabled
	case 5:
		return s.settings.DownloadEnabled
	case 6:
		return s.settings.DownloadExtrafanart
	case 7:
		return s.settings.OrganizeEnabled
	case 8:
		return s.settings.NFOEnabled
	case 9:
		return s.settings.UpdateMode
	default:
		return false
	}
}
