package tui

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func filterVideoFiles(files []fileItem, extensions []string) []fileItem {
	if len(extensions) == 0 {
		return []fileItem{}
	}

	filtered := make([]fileItem, 0, len(files))

	for _, file := range files {
		if file.IsDir {
			continue
		}

		for _, ext := range extensions {
			if strings.HasSuffix(strings.ToLower(file.Path), strings.ToLower(ext)) {
				filtered = append(filtered, file)
				break
			}
		}
	}

	return filtered
}

func formatFileStatus(file fileItem) string {
	if file.Matched && file.ID != "" {
		return fmt.Sprintf("matched [%s]", file.ID)
	}
	return "unmatched"
}

func sortFilesByStatus(files []fileItem) []fileItem {
	sorted := make([]fileItem, len(files))
	copy(sorted, files)

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].IsDir && !sorted[j].IsDir {
			return false
		}
		if !sorted[i].IsDir && sorted[j].IsDir {
			return true
		}

		if sorted[i].Matched && !sorted[j].Matched {
			return true
		}
		if !sorted[i].Matched && sorted[j].Matched {
			return false
		}

		return sorted[i].Path < sorted[j].Path
	})

	return sorted
}

func formatTaskStatus(step SortEventStep, progress float64) string {
	switch step {
	case taskStepComplete:
		return "OK"
	case taskStepFailed:
		return "ERR"
	default:
		if progress > 0 {
			return "RUN"
		}
		return "..."
	}
}

func TestFilterVideoFiles(t *testing.T) {
	tests := []struct {
		name       string
		files      []fileItem
		extensions []string
		want       []fileItem
	}{
		{
			name: "valid extensions mp4 and mkv",
			files: []fileItem{
				{Path: "movie.mp4", Name: "movie.mp4"},
				{Path: "video.mkv", Name: "video.mkv"},
				{Path: "subtitle.srt", Name: "subtitle.srt"},
			},
			extensions: []string{".mp4", ".mkv"},
			want: []fileItem{
				{Path: "movie.mp4", Name: "movie.mp4"},
				{Path: "video.mkv", Name: "video.mkv"},
			},
		},
		{
			name: "empty extensions list",
			files: []fileItem{
				{Path: "movie.mp4", Name: "movie.mp4"},
			},
			extensions: []string{},
			want:       []fileItem{},
		},
		{
			name: "no matches",
			files: []fileItem{
				{Path: "subtitle.srt", Name: "subtitle.srt"},
				{Path: "image.jpg", Name: "image.jpg"},
			},
			extensions: []string{".mp4", ".mkv"},
			want:       []fileItem{},
		},
		{
			name: "mixed file types",
			files: []fileItem{
				{Path: "movie.mp4", Name: "movie.mp4"},
				{Path: "subtitle.srt", Name: "subtitle.srt"},
				{Path: "video.avi", Name: "video.avi"},
				{Path: "image.jpg", Name: "image.jpg"},
			},
			extensions: []string{".mp4", ".avi"},
			want: []fileItem{
				{Path: "movie.mp4", Name: "movie.mp4"},
				{Path: "video.avi", Name: "video.avi"},
			},
		},
		{
			name:       "empty input files",
			files:      []fileItem{},
			extensions: []string{".mp4", ".mkv"},
			want:       []fileItem{},
		},
		{
			name: "skip directories",
			files: []fileItem{
				{Path: "/videos/", Name: "videos", IsDir: true},
				{Path: "/videos/movie.mp4", Name: "movie.mp4", IsDir: false},
			},
			extensions: []string{".mp4"},
			want: []fileItem{
				{Path: "/videos/movie.mp4", Name: "movie.mp4", IsDir: false},
			},
		},
		{
			name: "case insensitive matching",
			files: []fileItem{
				{Path: "MOVIE.MP4", Name: "MOVIE.MP4"},
				{Path: "video.MKV", Name: "video.MKV"},
			},
			extensions: []string{".mp4", ".mkv"},
			want: []fileItem{
				{Path: "MOVIE.MP4", Name: "MOVIE.MP4"},
				{Path: "video.MKV", Name: "video.MKV"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalFiles := make([]fileItem, len(tt.files))
			copy(originalFiles, tt.files)

			got := filterVideoFiles(tt.files, tt.extensions)

			assert.Equal(t, tt.want, got)

			// Verify immutability - original files unchanged
			assert.Equal(t, originalFiles, tt.files, "filterVideoFiles should not modify input files")
		})
	}
}

func TestFormatFileStatus(t *testing.T) {
	tests := []struct {
		name string
		file fileItem
		want string
	}{
		{
			name: "matched file with ID",
			file: fileItem{
				Path:    "IPX-123.mp4",
				Matched: true,
				ID:      "IPX-123",
			},
			want: "matched [IPX-123]",
		},
		{
			name: "unmatched file",
			file: fileItem{
				Path:    "random-file.mp4",
				Matched: false,
				ID:      "",
			},
			want: "unmatched",
		},
		{
			name: "matched true but no ID",
			file: fileItem{
				Path:    "file.mp4",
				Matched: true,
				ID:      "",
			},
			want: "unmatched",
		},
		{
			name: "matched false with ID present",
			file: fileItem{
				Path:    "file.mp4",
				Matched: false,
				ID:      "IPX-999",
			},
			want: "unmatched",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFileStatus(tt.file)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSortFilesByStatus(t *testing.T) {
	tests := []struct {
		name  string
		files []fileItem
		want  []fileItem
	}{
		{
			name: "mixed statuses - matched before unmatched",
			files: []fileItem{
				{Path: "c-unmatched.mp4", Matched: false},
				{Path: "a-matched.mp4", Matched: true, ID: "IPX-123"},
				{Path: "b-unmatched.mp4", Matched: false},
			},
			want: []fileItem{
				{Path: "a-matched.mp4", Matched: true, ID: "IPX-123"},
				{Path: "b-unmatched.mp4", Matched: false},
				{Path: "c-unmatched.mp4", Matched: false},
			},
		},
		{
			name: "all same status - alphabetical sort",
			files: []fileItem{
				{Path: "zebra.mp4", Matched: true, ID: "IPX-999"},
				{Path: "apple.mp4", Matched: true, ID: "IPX-123"},
				{Path: "banana.mp4", Matched: true, ID: "IPX-456"},
			},
			want: []fileItem{
				{Path: "apple.mp4", Matched: true, ID: "IPX-123"},
				{Path: "banana.mp4", Matched: true, ID: "IPX-456"},
				{Path: "zebra.mp4", Matched: true, ID: "IPX-999"},
			},
		},
		{
			name:  "empty files list",
			files: []fileItem{},
			want:  []fileItem{},
		},
		{
			name: "single file",
			files: []fileItem{
				{Path: "single.mp4", Matched: true, ID: "IPX-123"},
			},
			want: []fileItem{
				{Path: "single.mp4", Matched: true, ID: "IPX-123"},
			},
		},
		{
			name: "directories come last",
			files: []fileItem{
				{Path: "/videos/", Name: "videos", IsDir: true},
				{Path: "/videos/movie.mp4", Name: "movie.mp4", Matched: true, ID: "IPX-123"},
				{Path: "/docs/", Name: "docs", IsDir: true},
			},
			want: []fileItem{
				{Path: "/videos/movie.mp4", Name: "movie.mp4", Matched: true, ID: "IPX-123"},
				{Path: "/docs/", Name: "docs", IsDir: true},
				{Path: "/videos/", Name: "videos", IsDir: true},
			},
		},
		{
			name: "full sort priority test",
			files: []fileItem{
				{Path: "unmatched-2.mp4", Matched: false, IsDir: false},
				{Path: "/folder-a/", IsDir: true},
				{Path: "matched-2.mp4", Matched: true, ID: "IPX-456", IsDir: false},
				{Path: "unmatched-1.mp4", Matched: false, IsDir: false},
				{Path: "matched-1.mp4", Matched: true, ID: "IPX-123", IsDir: false},
				{Path: "/folder-b/", IsDir: true},
			},
			want: []fileItem{
				{Path: "matched-1.mp4", Matched: true, ID: "IPX-123", IsDir: false},
				{Path: "matched-2.mp4", Matched: true, ID: "IPX-456", IsDir: false},
				{Path: "unmatched-1.mp4", Matched: false, IsDir: false},
				{Path: "unmatched-2.mp4", Matched: false, IsDir: false},
				{Path: "/folder-a/", IsDir: true},
				{Path: "/folder-b/", IsDir: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalFiles := make([]fileItem, len(tt.files))
			copy(originalFiles, tt.files)

			got := sortFilesByStatus(tt.files)

			assert.Equal(t, tt.want, got)

			// Verify immutability - original files unchanged
			assert.Equal(t, originalFiles, tt.files, "sortFilesByStatus should not modify input files")
		})
	}
}

func TestFormatTaskStatus(t *testing.T) {
	tests := []struct {
		name     string
		step     SortEventStep
		progress float64
		want     string
	}{
		{
			name:     "running status (step=scraping, progress>0)",
			step:     sortStepScrape,
			progress: 0.5,
			want:     "RUN",
		},
		{
			name:     "complete status",
			step:     sortStepComplete,
			progress: 1.0,
			want:     "OK",
		},
		{
			name:     "failed status",
			step:     sortStepFailed,
			progress: 0.5,
			want:     "ERR",
		},
		{
			name:     "pending status (step=queued, progress=0)",
			step:     sortStepQueued,
			progress: 0,
			want:     "...",
		},
		{
			name:     "unknown step with zero progress",
			step:     SortEventStep("unknown"),
			progress: 0,
			want:     "...",
		},
		{
			name:     "unknown step with progress>0 is running",
			step:     SortEventStep("custom_step"),
			progress: 0.3,
			want:     "RUN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTaskStatus(tt.step, tt.progress)
			assert.Equal(t, tt.want, got)
		})
	}
}
