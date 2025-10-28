package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette
var (
	ColorPrimary    = lipgloss.Color("#8B5CF6") // Bright Purple
	ColorSuccess    = lipgloss.Color("#22C55E") // Bright Green
	ColorWarning    = lipgloss.Color("#FBBF24") // Bright Amber
	ColorError      = lipgloss.Color("#EF4444") // Red
	ColorInfo       = lipgloss.Color("#60A5FA") // Bright Blue
	ColorMuted      = lipgloss.Color("#9CA3AF") // Light Gray
	ColorBorder     = lipgloss.Color("#6B7280") // Medium gray
	ColorBackground = lipgloss.Color("#111827") // Very dark
	ColorForeground = lipgloss.Color("#F9FAFB") // Very light
	ColorHighlight  = lipgloss.Color("#A78BFA") // Light purple
	ColorTab        = lipgloss.Color("#4B5563") // Medium dark gray
	ColorTabActive  = lipgloss.Color("#8B5CF6") // Active tab purple
)

// Styles
var (
	// Header styles
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Background(ColorBackground).
			Padding(0, 1)

	StatusStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 1)

	// Border styles
	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	ActiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Padding(0, 1)

	// Title styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	// List item styles
	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Bold(true).
				PaddingLeft(2)

	UnselectedItemStyle = lipgloss.NewStyle().
				Foreground(ColorForeground).
				PaddingLeft(2)

	CheckedItemStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess).
				PaddingLeft(2)

	// Progress bar styles
	ProgressBarStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary)

	ProgressEmptyStyle = lipgloss.NewStyle().
				Foreground(ColorMuted)

	// Status badge styles
	SuccessBadge = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	ErrorBadge = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	WarningBadge = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	InfoBadge = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Bold(true)

	RunningBadge = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	// Log styles
	LogDebugStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	LogInfoStyle = lipgloss.NewStyle().
			Foreground(ColorInfo)

	LogWarnStyle = lipgloss.NewStyle().
			Foreground(ColorWarning)

	LogErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	// Help styles
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	HelpSeparatorStyle = lipgloss.NewStyle().
				Foreground(ColorBorder)

	// Dimmed text
	DimmedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// Highlight
	HighlightStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	// Error text
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	// Success text
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	// Tab styles
	TabStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Background(ColorTab).
			Padding(0, 2).
			MarginRight(1)

	ActiveTabStyle = lipgloss.NewStyle().
			Foreground(ColorForeground).
			Background(ColorTabActive).
			Padding(0, 2).
			MarginRight(1).
			Bold(true)

	// Panel style with lighter border
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)
)

// Helper functions for styled text

func Success(text string) string {
	return SuccessStyle.Render(text)
}

func Error(text string) string {
	return ErrorStyle.Render(text)
}

func Warning(text string) string {
	return WarningBadge.Render(text)
}

func Info(text string) string {
	return InfoBadge.Render(text)
}

func Dimmed(text string) string {
	return DimmedStyle.Render(text)
}

func Highlight(text string) string {
	return HighlightStyle.Render(text)
}

func Title(text string) string {
	return TitleStyle.Render(text)
}

func Subtitle(text string) string {
	return SubtitleStyle.Render(text)
}
