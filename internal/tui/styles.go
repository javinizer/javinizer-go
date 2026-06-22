package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	colorPrimary    = lipgloss.Color("#8B5CF6")
	colorSuccess    = lipgloss.Color("#22C55E")
	colorWarning    = lipgloss.Color("#FBBF24")
	colorError      = lipgloss.Color("#EF4444")
	colorInfo       = lipgloss.Color("#60A5FA")
	colorMuted      = lipgloss.Color("#9CA3AF")
	colorBorder     = lipgloss.Color("#6B7280")
	colorBackground = lipgloss.Color("#111827")
	colorForeground = lipgloss.Color("#F9FAFB")
	colorTab        = lipgloss.Color("#4B5563")
	colorTabActive  = lipgloss.Color("#8B5CF6")
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Background(colorBackground).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			MarginBottom(1)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true).
				PaddingLeft(2)

	unselectedItemStyle = lipgloss.NewStyle().
				Foreground(colorForeground).
				PaddingLeft(2)

	progressBarStyle = lipgloss.NewStyle().
				Foreground(colorPrimary)

	progressEmptyStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	successBadge = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	errorBadge = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	warningBadge = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	infoBadge = lipgloss.NewStyle().
			Foreground(colorInfo).
			Bold(true)

	runningBadge = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	logDebugStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	logInfoStyle = lipgloss.NewStyle().
			Foreground(colorInfo)

	logWarnStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	logErrorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	helpSeparatorStyle = lipgloss.NewStyle().
				Foreground(colorBorder)

	dimmedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	highlightStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	tabStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Background(colorTab).
			Padding(0, 2).
			MarginRight(1)

	activeTabStyle = lipgloss.NewStyle().
			Foreground(colorForeground).
			Background(colorTabActive).
			Padding(0, 2).
			MarginRight(1).
			Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)
)

func success(text string) string {
	return successStyle.Render(text)
}

func errorStyled(text string) string {
	return errorStyle.Render(text)
}

func warning(text string) string {
	return warningBadge.Render(text)
}

func dimmed(text string) string {
	return dimmedStyle.Render(text)
}

func highlight(text string) string {
	return highlightStyle.Render(text)
}

func title(text string) string {
	return titleStyle.Render(text)
}
