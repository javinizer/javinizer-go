//nolint:goconst
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// newFolderPickerDeps constructs the folder picker deps.
// Accepts only the narrow deps interface instead of *Model.
func newFolderPickerDeps(deps folderPickerDeps) folderPickerDeps {
	return deps
}

// Init is a no-op; folder picker modals do not issue commands on init.
func (fp *folderPickerModal) Init() tea.Cmd { return nil }

func (fp *folderPickerModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return fp, nil
	}
	switch keyMsg.String() {
	case "esc", "q":
		fp.close()
		return fp, nil
	case "up", "k":
		if fp.cursor > 0 {
			fp.cursor--
		}
		return fp, nil
	case "down", "j":
		if fp.cursor < len(fp.items)-1 {
			fp.cursor++
		}
		return fp, nil
	case "enter":
		if fp.cursor < len(fp.items) {
			fp.navigateToFolder(fp.items[fp.cursor].Path)
		}
		return fp, nil
	case " ", "space":
		mode := fp.mode
		fp.selectCurrentFolder()
		if mode == "source" {
			return fp, func() tea.Msg { return rescanMsg{Path: fp.path} }
		}
		return fp, nil
	}
	return fp, nil
}

func (fp *folderPickerModal) View() string {
	width := fp.deps.Width()
	height := fp.deps.Height()
	modalWidth := width - 20
	if modalWidth > 80 {
		modalWidth = 80
	}
	modalHeight := height - 10
	if modalHeight > 25 {
		modalHeight = 25
	}
	var b strings.Builder
	pickerTitle := "Select Source Folder"
	if fp.mode == "dest" {
		pickerTitle = "Select Output Folder"
	}
	b.WriteString(title(pickerTitle) + "\n\n")
	displayPath := fp.path
	if len(displayPath) > modalWidth-10 {
		displayPath = "..." + displayPath[len(displayPath)-(modalWidth-13):]
	}
	b.WriteString(dimmed("Current: ") + highlight(displayPath) + "\n\n")
	if len(fp.items) == 0 {
		b.WriteString(dimmed("No folders found\n"))
	} else {
		visibleHeight := modalHeight - 8
		start := fp.cursor - visibleHeight/2
		if start < 0 {
			start = 0
		}
		end := start + visibleHeight
		if end > len(fp.items) {
			end = len(fp.items)
			start = end - visibleHeight
			if start < 0 {
				start = 0
			}
		}
		for i := start; i < end; i++ {
			item := fp.items[i]
			cursor := "  "
			if i == fp.cursor {
				cursor = "> "
			}
			icon := "📁 "
			if item.Name == ".." {
				icon = "⬆️  "
			}
			name := item.Name
			if len(name) > modalWidth-10 {
				name = name[:modalWidth-13] + "..."
			}
			line := cursor + icon + name
			if i == fp.cursor {
				line = selectedItemStyle.Render(line)
			} else {
				line = unselectedItemStyle.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n" + dimmed("↑↓/jk: navigate  Enter: select folder  Space: choose current  Esc: cancel"))
	modal := panelStyle.Width(modalWidth).Height(modalHeight).Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func (fp *folderPickerModal) open(startPath, mode string) {
	if startPath == "" {
		startPath = "."
	}
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		absPath = startPath
	}
	fp.showing = true
	fp.path = absPath
	fp.cursor = 0
	fp.mode = mode
	fp.loadFolderContents(absPath)
}

func (fp *folderPickerModal) close() {
	fp.showing = false
	fp.items = nil
	fp.cursor = 0
}

func (fp *folderPickerModal) loadFolderContents(path string) {
	items := []folderItem{}
	if path != "/" && path != "." {
		items = append(items, folderItem{Path: filepath.Dir(path), Name: "..", IsDir: true})
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		fp.deps.AddLog("error", "Failed to read directory: "+err.Error())
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			name := entry.Name()
			if len(name) == 0 || name[0] == '.' {
				continue
			}
			items = append(items, folderItem{Path: filepath.Join(path, name), Name: name, IsDir: true})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == ".." {
			return true
		}
		if items[j].Name == ".." {
			return false
		}
		return items[i].Name < items[j].Name
	})
	fp.items = items
	if fp.cursor >= len(items) {
		fp.cursor = 0
	}
}

func (fp *folderPickerModal) navigateToFolder(path string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		fp.deps.AddLog("error", "Invalid path: "+err.Error())
		return
	}
	fp.path = absPath
	fp.cursor = 0
	fp.loadFolderContents(absPath)
}

func (fp *folderPickerModal) selectCurrentFolder() {
	if fp.path != "" {
		if fp.mode == "dest" {
			fp.deps.SetDestPath(fp.path)
			fp.deps.AddLog("info", fmt.Sprintf("Output directory set to: %s", fp.path))
		} else {
			fp.deps.SetSourcePath(fp.path)
			fp.deps.AddLog("info", fmt.Sprintf("Source directory set to: %s", fp.path))
		}
		fp.close()
	}
}
