package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHeader_SetWidth(t *testing.T) {
	h := newHeader()
	h.SetWidth(80)
	assert.Equal(t, 80, h.width)
}

func TestHeader_UpdateStats(t *testing.T) {
	h := newHeader()
	stats := jobStats{Total: 10, Running: 5}
	h.UpdateStats(stats)
	assert.Equal(t, 10, h.stats.Total)
	assert.Equal(t, 5, h.stats.Running)
}

func TestHeader_View(t *testing.T) {
	h := newHeader()
	view := h.View()
	assert.Contains(t, view, "Javinizer")
}

func TestBrowser_SetItems(t *testing.T) {
	b := newBrowser()
	items := []fileItem{
		{Path: "/test/file1.mp4", Name: "file1.mp4"},
		{Path: "/test/file2.mp4", Name: "file2.mp4"},
	}
	b.SetItems(items)
	assert.Equal(t, 2, len(b.items))
}

func TestBrowser_SetPathDisplay(t *testing.T) {
	b := newBrowser()
	b.SetPathDisplay("/custom/display")
	assert.Equal(t, "/custom/display", b.pathDisplay)
}

func TestBrowser_CursorUp(t *testing.T) {
	b := newBrowser()
	b.SetItems([]fileItem{
		{Path: "/a"}, {Path: "/b"}, {Path: "/c"},
	})
	b.cursor = 2
	b.CursorUp()
	assert.Equal(t, 1, b.cursor)
	// Can't go above 0
	b.cursor = 0
	b.CursorUp()
	assert.Equal(t, 0, b.cursor)
}

func TestBrowser_CursorDown(t *testing.T) {
	b := newBrowser()
	b.SetItems([]fileItem{
		{Path: "/a"}, {Path: "/b"}, {Path: "/c"},
	})
	b.CursorDown()
	assert.Equal(t, 1, b.cursor)
	b.CursorDown()
	assert.Equal(t, 2, b.cursor)
	// Can't go beyond last item
	b.CursorDown()
	assert.Equal(t, 2, b.cursor)
}

func TestBrowser_CursorDown_EmptyItems(t *testing.T) {
	b := newBrowser()
	b.CursorDown()
	assert.Equal(t, 0, b.cursor)
}

func TestBrowser_ToggleSelection(t *testing.T) {
	b := newBrowser()
	b.SetItems([]fileItem{
		{Path: "/a.mp4", Name: "a.mp4", IsDir: false},
	})
	b.ToggleSelection("/a.mp4")
	assert.True(t, b.selected["/a.mp4"])
	b.ToggleSelection("/a.mp4")
	assert.False(t, b.selected["/a.mp4"])
}

func TestBrowser_ToggleSelection_Directory(t *testing.T) {
	b := newBrowser()
	b.SetItems([]fileItem{
		{Path: "/dir", Name: "dir", IsDir: true},
		{Path: "/dir/file1.mp4", Name: "file1.mp4", IsDir: false},
		{Path: "/dir/file2.mp4", Name: "file2.mp4", IsDir: false},
	})
	b.ToggleSelection("/dir")
	assert.True(t, b.selected["/dir"])
	assert.True(t, b.selected["/dir/file1.mp4"])
	assert.True(t, b.selected["/dir/file2.mp4"])
	// Toggle off
	b.ToggleSelection("/dir")
	assert.False(t, b.selected["/dir"])
	assert.False(t, b.selected["/dir/file1.mp4"])
}

func TestBrowser_ToggleSelection_NonexistentPath(t *testing.T) {
	b := newBrowser()
	b.SetItems([]fileItem{{Path: "/a.mp4", Name: "a.mp4"}})
	// Should not panic on unknown path
	b.ToggleSelection("/nonexistent")
	assert.Len(t, b.selected, 0)
}

func TestBrowser_SelectAll(t *testing.T) {
	b := newBrowser()
	b.SetItems([]fileItem{
		{Path: "/a.mp4", Name: "a.mp4"},
		{Path: "/b.mp4", Name: "b.mp4"},
	})
	b.SelectAll()
	assert.True(t, b.selected["/a.mp4"])
	assert.True(t, b.selected["/b.mp4"])
}

func TestLogViewer_ScrollUp(t *testing.T) {
	l := newLogViewer()
	logs := []logEntry{
		{Level: "info", Message: "line1", Timestamp: time.Now()},
		{Level: "info", Message: "line2", Timestamp: time.Now()},
		{Level: "info", Message: "line3", Timestamp: time.Now()},
	}
	l.SetLogs(logs, 1, false)
	assert.Equal(t, 1, l.scroll)
}

func TestLogViewer_ScrollDown(t *testing.T) {
	l := newLogViewer()
	logs := []logEntry{
		{Level: "info", Message: "line1", Timestamp: time.Now()},
		{Level: "info", Message: "line2", Timestamp: time.Now()},
		{Level: "info", Message: "line3", Timestamp: time.Now()},
	}
	l.SetLogs(logs, 0, false)
	assert.Equal(t, 0, l.scroll)
}

func TestLogViewer_ScrollToTop(t *testing.T) {
	l := newLogViewer()
	logs := []logEntry{
		{Level: "info", Message: "line1", Timestamp: time.Now()},
		{Level: "info", Message: "line2", Timestamp: time.Now()},
	}
	l.SetLogs(logs, 1, false)
	assert.Equal(t, 1, l.scroll)
}

func TestLogViewer_ScrollToBottom(t *testing.T) {
	l := newLogViewer()
	logs := []logEntry{
		{Level: "info", Message: "line1", Timestamp: time.Now()},
		{Level: "info", Message: "line2", Timestamp: time.Now()},
		{Level: "info", Message: "line3", Timestamp: time.Now()},
	}
	l.SetLogs(logs, 2, false)
	assert.Equal(t, 2, l.scroll)
}

func TestLogViewer_AutoScroll(t *testing.T) {
	l := newLogViewer()
	l.SetLogs([]logEntry{{Level: "info", Message: "line1", Timestamp: time.Now()}}, 0, true)
	assert.True(t, l.autoScroll)
}

func TestLogViewer_SetLogsAutoScroll(t *testing.T) {
	l := newLogViewer()
	logs := []logEntry{
		{Level: "info", Message: "line1", Timestamp: time.Now()},
		{Level: "info", Message: "line2", Timestamp: time.Now()},
	}
	// When autoScroll is true, scroll should be set to the last index
	l.SetLogs(logs, len(logs)-1, true)
	assert.Equal(t, 1, l.scroll)
}

func TestDashboard_SetSize(t *testing.T) {
	d := newDashboard()
	d.SetSize(100, 50)
	assert.Equal(t, 100, d.width)
	assert.Equal(t, 50, d.height)
}
