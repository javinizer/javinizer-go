package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/javinizer/javinizer-go/internal/tui/localization"
)

// console component - shows live output and metadata preview
type console struct {
	width      int
	height     int
	entries    []string
	maxEntries int
	autoScroll bool
	scroll     int
	localizer  *localization.Localizer
}

func newConsole() *console {
	return &console{
		entries:    make([]string, 0),
		maxEntries: 1000,
		autoScroll: true,
		scroll:     0,
	}
}

// SetLocalizer wires the localizer used for translating console chrome.
func (c *console) SetLocalizer(l *localization.Localizer) {
	c.localizer = l
}

// loc returns the localized message for id, applying template data when supplied.
// It is nil-safe so render code cannot panic if the localizer failed to
// construct at startup; in that case the raw id is returned.
func (c *console) loc(id string, template ...map[string]any) string {
	if c.localizer == nil {
		return id
	}
	return c.localizer.Localize(id, template...)
}

func (c *console) SetSize(width, height int) {
	c.width = width
	c.height = height
}

func (c *console) Init() tea.Cmd {
	return nil
}

func (c *console) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return c, nil
}

func (c *console) AddEntry(entry string) {
	c.entries = append(c.entries, entry)

	// Trim if exceeds max
	if len(c.entries) > c.maxEntries {
		c.entries = c.entries[len(c.entries)-c.maxEntries:]
	}

	// Auto-scroll to bottom
	if c.autoScroll {
		c.ScrollToBottom()
	}
}

func (c *console) Clear() {
	c.entries = make([]string, 0)
	c.scroll = 0
}

func (c *console) ScrollUp() {
	if c.scroll > 0 {
		c.scroll--
	}
}

func (c *console) ScrollDown() {
	maxScroll := len(c.entries) - c.height + 3
	if maxScroll < 0 {
		maxScroll = 0
	}
	if c.scroll < maxScroll {
		c.scroll++
	}
}

func (c *console) ScrollToTop() {
	c.scroll = 0
}

func (c *console) ScrollToBottom() {
	maxScroll := len(c.entries) - c.height + 3
	if maxScroll < 0 {
		maxScroll = 0
	}
	c.scroll = maxScroll
}

func (c *console) ToggleAutoScroll() {
	c.autoScroll = !c.autoScroll
}

func (c *console) View() string {
	view := title(c.loc("TUIConsoleTitle")) + "\n"

	if len(c.entries) == 0 {
		return view + dimmed(c.loc("TUIConsoleNoOutput"))
	}

	// Calculate visible range
	visibleHeight := c.height - 2 // Account for title
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	start := c.scroll
	if start < 0 {
		start = 0
	}
	end := start + visibleHeight
	if end > len(c.entries) {
		end = len(c.entries)
		start = end - visibleHeight
		if start < 0 {
			start = 0
		}
	}

	// Render entries
	for i := start; i < end; i++ {
		entry := c.entries[i]

		// Word wrap if needed
		maxWidth := c.width - 2
		if maxWidth < 40 {
			maxWidth = 40
		}

		if len(entry) > maxWidth {
			// Simple wrapping - split into chunks
			for len(entry) > 0 {
				if len(entry) > maxWidth {
					view += entry[:maxWidth] + "\n"
					entry = entry[maxWidth:]
				} else {
					view += entry + "\n"
					break
				}
			}
		} else {
			view += entry + "\n"
		}
	}

	// Show scroll indicator if not all entries visible
	if len(c.entries) > visibleHeight {
		view += dimmed(c.loc("TUIConsoleScrollIndicator", map[string]any{"Visible": end, "Total": len(c.entries)}))
	}

	return view
}
