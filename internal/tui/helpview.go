package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/tui/localization"
)

// helpView component
type helpView struct {
	width     int
	height    int
	localizer *localization.Localizer
}

func newHelpView() *helpView {
	return &helpView{}
}

func (h *helpView) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h *helpView) SetLocalizer(l *localization.Localizer) {
	h.localizer = l
}

//nolint:unparam // variadic for API consistency with other components
func (h *helpView) loc(id string, template ...map[string]any) string {
	if h.localizer == nil {
		return id
	}
	return h.localizer.Localize(id, template...)
}

func (h *helpView) Init() tea.Cmd {
	return nil
}

func (h *helpView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return h, nil
}

// helpLine renders a key/action help line in the form "  key - action".
func helpLine(key, action string) string {
	return "  " + key + " - " + action + "\n"
}

func (h *helpView) View() string {
	var b strings.Builder
	b.WriteString(title(h.loc("TUIHelpTitle")) + "\n\n")

	b.WriteString(helpKeyStyle.Render(h.loc("TUIHelpGlobalKeys")) + "\n")
	b.WriteString(helpLine("?", h.loc("TUIHelpActionToggleHelp")))
	b.WriteString(helpLine("q/Ctrl+C", h.loc("TUIHelpActionQuit")))
	b.WriteString(helpLine("1-3", h.loc("TUIHelpActionSwitchViews")))
	b.WriteString(helpLine("Tab", h.loc("TUIHelpActionCycleViews")))
	b.WriteString("\n")

	b.WriteString(helpKeyStyle.Render(h.loc("TUIHelpBrowserSection")) + "\n")
	b.WriteString(helpLine("m", h.loc("TUIHelpActionManualSearch")))
	b.WriteString(helpLine("f", h.loc("TUIHelpActionChangeFolder")))
	b.WriteString(helpLine("r", h.loc("TUIHelpActionRefresh")))
	b.WriteString(helpLine("↑/k", h.loc("TUIHelpActionMoveUp")))
	b.WriteString(helpLine("↓/j", h.loc("TUIHelpActionMoveDown")))
	b.WriteString(helpLine("Space", h.loc("TUIHelpActionToggleSelection")))
	b.WriteString(helpLine("a", h.loc("TUIHelpActionSelectAll")))
	b.WriteString(helpLine("A", h.loc("TUIHelpActionDeselectAll")))
	b.WriteString(helpLine("Enter", h.loc("TUIHelpActionStartProcessing")))
	b.WriteString(helpLine("p", h.loc("TUIHelpActionPauseResume")))
	b.WriteString("\n")

	b.WriteString(helpKeyStyle.Render(h.loc("TUIHelpLogsSection")) + "\n")
	b.WriteString(helpLine("↑/k", h.loc("TUIHelpActionScrollUp")))
	b.WriteString(helpLine("↓/j", h.loc("TUIHelpActionScrollDown")))
	b.WriteString(helpLine("g", h.loc("TUIHelpActionGoTop")))
	b.WriteString(helpLine("G", h.loc("TUIHelpActionGoBottom")))
	b.WriteString(helpLine("a", h.loc("TUIHelpActionToggleAutoScroll")))
	b.WriteString("\n")

	b.WriteString(helpKeyStyle.Render(h.loc("TUIHelpSettingsSection")) + "\n")
	b.WriteString(helpLine("↑/k ↓/j", h.loc("TUIHelpActionMoveUp")))
	b.WriteString(helpLine("Space", h.loc("TUIHelpActionToggleSetting")))
	b.WriteString(helpLine("←/→ Enter", h.loc("TUIHelpActionCycleChoice")))

	b.WriteString(dimmed(h.loc("TUIHelpFoldersNote")))

	return b.String()
}
