package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// browser component
type browser struct {
	items       []fileItem
	cursor      int
	width       int
	height      int
	selected    map[string]bool
	sourcePath  string // Current scan path
	destPath    string // Destination path for organized files
	pathDisplay string // Formatted path display for the view
}

func newBrowser() *browser {
	return &browser{
		items:    make([]fileItem, 0),
		selected: make(map[string]bool),
	}
}

func (b *browser) SetSize(width, height int) {
	b.width = width
	b.height = height
}

func (b *browser) SetItems(items []fileItem) {
	b.items = items
}

func (b *browser) SetSourcePath(path string) {
	b.sourcePath = path
	b.pathDisplay = path
}

func (b *browser) SetDestPath(path string) {
	b.destPath = path
}

func (b *browser) SetPathDisplay(display string) {
	b.pathDisplay = display
}

func (b *browser) CursorUp() {
	if b.cursor > 0 {
		b.cursor--
	}
}

func (b *browser) CursorDown() {
	if b.cursor < len(b.items)-1 {
		b.cursor++
	}
}

func (b *browser) ToggleSelection(path string) {
	// Find the item
	var targetItem *fileItem
	for i := range b.items {
		if b.items[i].Path == path {
			targetItem = &b.items[i]
			break
		}
	}

	if targetItem == nil {
		return
	}

	// If it's a directory, toggle all files within it
	if targetItem.IsDir {
		isCurrentlySelected := b.selected[path]
		newState := !isCurrentlySelected

		// Normalize the directory path so that child matching is consistent
		// cross-platform. filepath.Dir returns OS-native separators
		// (backslash on Windows); the input path must be normalized the same
		// way for the equality to succeed when callers pass forward-slash paths.
		normPath := filepath.Clean(path)

		// Toggle all files in this directory
		for i := range b.items {
			if !b.items[i].IsDir && filepath.Dir(b.items[i].Path) == normPath {
				b.selected[b.items[i].Path] = newState
			}
		}

		// Toggle the folder marker itself
		b.selected[path] = newState
	} else {
		// Regular file toggle
		b.selected[path] = !b.selected[path]
	}
}

func (b *browser) SelectAll() {
	for _, item := range b.items {
		b.selected[item.Path] = true
	}
}

func (b *browser) DeselectAll() {
	b.selected = make(map[string]bool)
}

func (b *browser) Init() tea.Cmd {
	return nil
}

func (b *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return b, nil
}

func (b *browser) View() string {
	// title
	view := title("Files") + " " + dimmed("(f:source o:output m:search M:merge)") + "\n"

	// Source path
	sourceDisplay := b.sourcePath
	if sourceDisplay == "" {
		sourceDisplay = "."
	}
	if len(sourceDisplay) > 40 {
		sourceDisplay = "..." + sourceDisplay[len(sourceDisplay)-37:]
	}
	view += dimmed("From: ") + highlight(sourceDisplay) + "\n"

	// Destination path
	destDisplay := b.destPath
	if destDisplay == "" {
		destDisplay = sourceDisplay // Default to source
	}
	if len(destDisplay) > 40 {
		destDisplay = "..." + destDisplay[len(destDisplay)-37:]
	}
	view += dimmed("To:   ") + highlight(destDisplay) + "\n\n"

	if len(b.items) == 0 {
		return view + dimmed("No files found")
	}

	// Show items around cursor
	start := b.cursor - 5
	if start < 0 {
		start = 0
	}
	end := start + b.height - 4
	if end > len(b.items) {
		end = len(b.items)
	}

	for i := start; i < end; i++ {
		item := b.items[i]
		cursor := "  "
		if i == b.cursor {
			cursor = "> "
		}

		// Tree indentation based on depth
		indent := strings.Repeat("  ", item.Depth)

		// Determine checkbox state
		checkbox := "☐ "
		if item.IsDir {
			// For folders, check if all children are selected
			allChildrenSelected := true
			hasChildren := false
			normPath := filepath.Clean(item.Path)
			for j := range b.items {
				if !b.items[j].IsDir && filepath.Dir(b.items[j].Path) == normPath {
					hasChildren = true
					if !b.selected[b.items[j].Path] {
						allChildrenSelected = false
						break
					}
				}
			}
			if hasChildren && allChildrenSelected {
				checkbox = success("☑ ")
			}
		} else {
			// For files, check direct selection
			if b.selected[item.Path] {
				checkbox = success("☑ ")
			}
		}

		// Add folder icon for directories
		icon := ""
		if item.IsDir {
			icon = "📁 "
		}

		name := item.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}

		// Show matched status for files
		matchIndicator := ""
		if !item.IsDir && item.Matched {
			matchIndicator = " " + dimmed("["+item.ID+"]")
		}

		view += cursor + indent + checkbox + icon + name + matchIndicator + "\n"
	}

	view += fmt.Sprintf("\n%d/%d files", b.cursor+1, len(b.items))
	return view
}
