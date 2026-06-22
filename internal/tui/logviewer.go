package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// logViewer component — stateless renderer for data. Log entries and scroll state
// are owned by Model and pushed via SetLogs before each render.
type logViewer struct {
	// Render snapshot — set by Model before each View call.
	logs       []logEntry
	scroll     int
	autoScroll bool
	width      int
	height     int
}

func newLogViewer() *logViewer {
	return &logViewer{}
}

func (l *logViewer) SetSize(width, height int) {
	l.width = width
	l.height = height
}

// SetLogs replaces the log data snapshot used for rendering.
// The Model owns the canonical log state and pushes it before each View call.
func (l *logViewer) SetLogs(logs []logEntry, scroll int, autoScroll bool) {
	l.logs = logs
	l.scroll = scroll
	l.autoScroll = autoScroll
}

func (l *logViewer) Init() tea.Cmd {
	return nil
}

func (l *logViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return l, nil
}

func (l *logViewer) View() string {
	view := title("Logs") + "\n\n"

	if len(l.logs) == 0 {
		return view + dimmed("No logs yet")
	}

	// Show logs around scroll position
	start := l.scroll - l.height + 4
	if start < 0 {
		start = 0
	}
	end := l.scroll + 1
	if end > len(l.logs) {
		end = len(l.logs)
	}

	for i := start; i < end; i++ {
		log := l.logs[i]
		timestamp := log.Timestamp.Format("15:04:05")

		var levelStyle lipgloss.Style
		switch log.Level {
		case "debug":
			levelStyle = logDebugStyle
		case "info":
			levelStyle = logInfoStyle
		case "warn":
			levelStyle = logWarnStyle
		case "error":
			levelStyle = logErrorStyle
		default:
			levelStyle = logInfoStyle
		}

		level := levelStyle.Render(fmt.Sprintf("[%-5s]", log.Level))

		// Wrap long messages to fit width
		// Account for timestamp (8) + level (7) + spacing (2) = 17 chars
		maxMessageWidth := l.width - 17
		if maxMessageWidth < 40 {
			maxMessageWidth = 40
		}

		message := log.Message
		if len(message) > maxMessageWidth {
			// Word wrap the message
			words := strings.Fields(message)
			var lines []string
			currentLine := ""

			for _, word := range words {
				if len(currentLine)+len(word)+1 <= maxMessageWidth {
					if currentLine == "" {
						currentLine = word
					} else {
						currentLine += " " + word
					}
				} else {
					if currentLine != "" {
						lines = append(lines, currentLine)
					}
					currentLine = word
				}
			}
			if currentLine != "" {
				lines = append(lines, currentLine)
			}

			// Render first line with timestamp and level
			if len(lines) > 0 {
				view += fmt.Sprintf("%s %s %s\n", dimmed(timestamp), level, lines[0])
				// Continuation lines with indentation
				for j := 1; j < len(lines); j++ {
					view += fmt.Sprintf("%s %s %s\n", strings.Repeat(" ", 8), strings.Repeat(" ", 7), lines[j])
				}
			}
		} else {
			view += fmt.Sprintf("%s %s %s\n", dimmed(timestamp), level, message)
		}
	}

	return view
}
