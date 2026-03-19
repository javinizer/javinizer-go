package api

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
)

func TestGeneratePreview_MultipartFallbackPaths(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.SubfolderFormat = []string{"<MAKER>", "<SERIES>"}
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID><PARTSUFFIX>"
	cfg.Output.PosterFormat = "<SERIES>"
	cfg.Output.FanartFormat = "<SERIES>"
	cfg.Output.ScreenshotFolder = "shots"
	cfg.Output.ScreenshotFormat = "<SERIES>"
	cfg.Output.ScreenshotPadding = 2
	cfg.Output.MaxPathLength = 1
	cfg.Output.DownloadExtrafanart = true // Enable for screenshot/fanart preview
	cfg.Metadata.NFO.PerFile = true
	cfg.Metadata.NFO.FilenameTemplate = "<SERIES>"

	movie := &models.Movie{
		ID:          "ABC-123",
		Title:       "Sample Title",
		Maker:       "IdeaPocket",
		Screenshots: []string{"a", "b"},
	}
	fileResults := []*worker.FileResult{
		{FilePath: "/videos/ABC-123-pt1.mp4", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
		{FilePath: "/videos/ABC-123-pt2.mp4", IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg)

	folderPath := filepath.Join("/library", "IdeaPocket", "ABC-123")
	if resp.FolderName != "ABC-123" {
		t.Fatalf("FolderName = %q, want %q", resp.FolderName, "ABC-123")
	}
	if resp.FileName != "ABC-123" {
		t.Fatalf("FileName = %q, want %q", resp.FileName, "ABC-123")
	}
	if len(resp.VideoFiles) != 2 {
		t.Fatalf("len(VideoFiles) = %d, want 2", len(resp.VideoFiles))
	}
	if resp.VideoFiles[0] != filepath.Join(folderPath, "ABC-123-pt1.mp4") {
		t.Fatalf("VideoFiles[0] = %q", resp.VideoFiles[0])
	}
	if resp.VideoFiles[1] != filepath.Join(folderPath, "ABC-123-pt2.mp4") {
		t.Fatalf("VideoFiles[1] = %q", resp.VideoFiles[1])
	}
	if len(resp.NFOPaths) != 2 {
		t.Fatalf("len(NFOPaths) = %d, want 2", len(resp.NFOPaths))
	}
	if resp.NFOPaths[0] != filepath.Join(folderPath, "ABC-123-pt1.nfo") {
		t.Fatalf("NFOPaths[0] = %q", resp.NFOPaths[0])
	}
	if resp.NFOPaths[1] != filepath.Join(folderPath, "ABC-123-pt2.nfo") {
		t.Fatalf("NFOPaths[1] = %q", resp.NFOPaths[1])
	}
	if resp.PosterPath != filepath.Join(folderPath, "ABC-123-poster.jpg") {
		t.Fatalf("PosterPath = %q", resp.PosterPath)
	}
	if resp.FanartPath != filepath.Join(folderPath, "ABC-123-fanart.jpg") {
		t.Fatalf("FanartPath = %q", resp.FanartPath)
	}
	if resp.ExtrafanartPath != filepath.Join(folderPath, "shots") {
		t.Fatalf("ExtrafanartPath = %q", resp.ExtrafanartPath)
	}
	if len(resp.Screenshots) != 2 || resp.Screenshots[0] != "fanart01.jpg" || resp.Screenshots[1] != "fanart02.jpg" {
		t.Fatalf("Screenshots = %#v", resp.Screenshots)
	}
}

func TestGeneratePreview_NoFileResultsFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.DownloadExtrafanart = true // Enable for screenshot preview
	cfg.Metadata.NFO.PerFile = false
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"

	movie := &models.Movie{
		ID:          "XYZ-999",
		Title:       "Fallback Title",
		Screenshots: []string{"shot"},
	}

	resp := generatePreview(movie, nil, "/library", cfg)

	folderPath := filepath.Join("/library", "XYZ-999")
	if resp.FullPath != filepath.Join(folderPath, "XYZ-999.mp4") {
		t.Fatalf("FullPath = %q", resp.FullPath)
	}
	if len(resp.VideoFiles) != 1 || resp.VideoFiles[0] != resp.FullPath {
		t.Fatalf("VideoFiles = %#v", resp.VideoFiles)
	}
	if resp.NFOPath != filepath.Join(folderPath, "XYZ-999.nfo") {
		t.Fatalf("NFOPath = %q", resp.NFOPath)
	}
	if len(resp.NFOPaths) != 0 {
		t.Fatalf("NFOPaths = %#v, want empty", resp.NFOPaths)
	}
	if len(resp.Screenshots) != 1 || resp.Screenshots[0] != "fanart1.jpg" {
		t.Fatalf("Screenshots = %#v", resp.Screenshots)
	}
}

// TestGeneratePreview_PosterDownloadFlag tests that poster path is only generated
// when DownloadPoster is explicitly enabled, not when DownloadCover is true
func TestGeneratePreview_PosterDownloadFlag(t *testing.T) {
	tests := []struct {
		name           string
		downloadCover  bool
		downloadPoster bool
		expectPoster   bool
	}{
		{
			name:           "poster disabled when only DownloadCover enabled",
			downloadCover:  true,
			downloadPoster: false,
			expectPoster:   false,
		},
		{
			name:           "poster generated when DownloadPoster enabled",
			downloadCover:  false,
			downloadPoster: true,
			expectPoster:   true,
		},
		{
			name:           "poster generated when both enabled",
			downloadCover:  true,
			downloadPoster: true,
			expectPoster:   true,
		},
		{
			name:           "poster not generated when both disabled",
			downloadCover:  false,
			downloadPoster: false,
			expectPoster:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Output.FolderFormat = "<ID>"
			cfg.Output.FileFormat = "<ID>"
			cfg.Output.PosterFormat = "<ID>-poster.jpg"
			cfg.Output.DownloadCover = tt.downloadCover
			cfg.Output.DownloadPoster = tt.downloadPoster

			movie := &models.Movie{
				ID:    "TEST-001",
				Title: "Test Movie",
			}

			resp := generatePreview(movie, nil, "/library", cfg)

			if tt.expectPoster {
				assert.NotEmpty(t, resp.PosterPath, "poster path should be generated")
				assert.True(t, strings.Contains(resp.PosterPath, "poster"), "poster path should contain 'poster'")
			} else {
				assert.Empty(t, resp.PosterPath, "poster path should be empty when DownloadPoster is disabled")
			}
		})
	}
}

// TestGeneratePreview_FanartDownloadFlag tests that fanart path is only generated
// when DownloadExtrafanart is enabled
func TestGeneratePreview_FanartDownloadFlag(t *testing.T) {
	tests := []struct {
		name                string
		downloadExtrafanart bool
		expectFanart        bool
		expectScreenshots   bool
	}{
		{
			name:                "fanart and screenshots disabled",
			downloadExtrafanart: false,
			expectFanart:        false,
			expectScreenshots:   false,
		},
		{
			name:                "fanart enabled but no screenshots in movie",
			downloadExtrafanart: true,
			expectFanart:        true,
			expectScreenshots:   false, // Screenshots require movie.Screenshots to have URLs
		},
		{
			name:                "fanart and screenshots enabled with movie screenshots",
			downloadExtrafanart: true,
			expectFanart:        true,
			expectScreenshots:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Output.FolderFormat = "<ID>"
			cfg.Output.FileFormat = "<ID>"
			cfg.Output.FanartFormat = "<ID>-fanart.jpg"
			cfg.Output.ScreenshotFolder = "extrafanart"
			cfg.Output.DownloadExtrafanart = tt.downloadExtrafanart

			// Only add screenshots for the test case that expects them
			movie := &models.Movie{
				ID:    "TEST-002",
				Title: "Test Movie",
			}
			if tt.expectScreenshots {
				movie.Screenshots = []string{"http://example.com/shot1.jpg", "http://example.com/shot2.jpg"}
			}

			resp := generatePreview(movie, nil, "/library", cfg)

			if tt.expectFanart {
				assert.NotEmpty(t, resp.FanartPath, "fanart path should be generated")
				assert.True(t, strings.Contains(resp.FanartPath, "fanart"), "fanart path should contain 'fanart'")
			} else {
				assert.Empty(t, resp.FanartPath, "fanart path should be empty when DownloadExtrafanart is disabled")
			}

			if tt.expectScreenshots {
				assert.NotEmpty(t, resp.Screenshots, "screenshots should be generated")
			} else {
				assert.Empty(t, resp.Screenshots, "screenshots should be empty when DownloadExtrafanart is disabled or no movie screenshots")
			}
		})
	}
}

// TestGeneratePreview_NFODisabled tests that NFO paths are empty when NFO is disabled
func TestGeneratePreview_NFODisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Metadata.NFO.Enabled = false
	cfg.Metadata.NFO.PerFile = true
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"

	movie := &models.Movie{
		ID:    "TEST-003",
		Title: "Test Movie",
	}

	resp := generatePreview(movie, nil, "/library", cfg)

	assert.Empty(t, resp.NFOPath, "NFO path should be empty when NFO is disabled")
	assert.Empty(t, resp.NFOPaths, "NFO paths should be empty when NFO is disabled")
}

// TestGeneratePreview_MultipartContext tests that poster/fanart use the first
// file result's multipart context (as implemented in generatePreview)
func TestGeneratePreview_MultipartContext(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.PosterFormat = "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg"
	cfg.Output.FanartFormat = "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg"
	cfg.Output.DownloadPoster = true
	cfg.Output.DownloadExtrafanart = true

	movie := &models.Movie{
		ID:    "TEST-004",
		Title: "Test Movie",
	}

	// Add pt1 first - poster/fanart will use first file's context
	fileResults := []*worker.FileResult{
		{FilePath: "/videos/TEST-004-pt1.mp4", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
		{FilePath: "/videos/TEST-004-pt2.mp4", IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg)

	assert.Contains(t, resp.PosterPath, "-pt1-poster", "poster should use first file's pt1 suffix")
	assert.Contains(t, resp.FanartPath, "-pt1-fanart", "fanart should use first file's pt1 suffix")
}

// TestGeneratePreview_MultipartContextReverse tests that poster/fanart use the
// first file result's context even when pt2 comes first in the slice
func TestGeneratePreview_MultipartContextReverse(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.PosterFormat = "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg"
	cfg.Output.FanartFormat = "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg"
	cfg.Output.DownloadPoster = true
	cfg.Output.DownloadExtrafanart = true

	movie := &models.Movie{
		ID:    "TEST-004B",
		Title: "Test Movie",
	}

	// Add pt2 first - poster/fanart will use first file's context (pt2)
	fileResults := []*worker.FileResult{
		{FilePath: "/videos/TEST-004B-pt2.mp4", IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"},
		{FilePath: "/videos/TEST-004B-pt1.mp4", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg)

	// First file is pt2, so poster/fanart will use pt2 context
	assert.Contains(t, resp.PosterPath, "-pt2-poster", "poster should use first file's pt2 suffix")
	assert.Contains(t, resp.FanartPath, "-pt2-fanart", "fanart should use first file's pt2 suffix")
}

// TestGeneratePreview_SingleFileNoMultipart tests that single file doesn't
// use multipart template suffixes
func TestGeneratePreview_SingleFileNoMultipart(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.PosterFormat = "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg"
	cfg.Output.FanartFormat = "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg"
	cfg.Output.DownloadPoster = true
	cfg.Output.DownloadExtrafanart = true

	movie := &models.Movie{
		ID:    "TEST-005",
		Title: "Test Movie",
	}

	fileResults := []*worker.FileResult{
		{FilePath: "/videos/TEST-005.mp4", IsMultiPart: false},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg)

	assert.NotContains(t, resp.PosterPath, "-pt", "poster should not have multipart suffix for single file")
	assert.NotContains(t, resp.FanartPath, "-pt", "fanart should not have multipart suffix for single file")
}

// TestGeneratePreview_NFOPerFile tests NFO path generation with PerFile mode
// Note: When PerFile is true, NFOPath is set to the first NFO path for backward compatibility
func TestGeneratePreview_NFOPerFile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Metadata.NFO.Enabled = true
	cfg.Metadata.NFO.PerFile = true
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"

	movie := &models.Movie{
		ID:    "TEST-006",
		Title: "Test Movie",
	}

	fileResults := []*worker.FileResult{
		{FilePath: "/videos/TEST-006-pt1.mp4", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
		{FilePath: "/videos/TEST-006-pt2.mp4", IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg)

	// PerFile=true means NFOPaths has multiple entries, but NFOPath is set to first for backward compatibility
	assert.Len(t, resp.NFOPaths, 2, "should have 2 NFO paths for per-file mode")
	assert.NotEmpty(t, resp.NFOPath, "NFOPath should be set to first NFO for backward compatibility")
}

// TestGeneratePreview_NFOSingleFile tests NFO path generation with single file (PerFile=false)
func TestGeneratePreview_NFOSingleFile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Metadata.NFO.Enabled = true
	cfg.Metadata.NFO.PerFile = false
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"

	movie := &models.Movie{
		ID:    "TEST-007",
		Title: "Test Movie",
	}

	fileResults := []*worker.FileResult{
		{FilePath: "/videos/TEST-007.mp4"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg)

	assert.NotEmpty(t, resp.NFOPath, "NFOPath should be set when PerFile is false")
	assert.True(t, strings.Contains(resp.NFOPath, "TEST-007.nfo"), "NFO path should match template")
	assert.Empty(t, resp.NFOPaths, "NFOPaths should be empty when PerFile is false")
}
