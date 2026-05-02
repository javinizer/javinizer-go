package batch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/types"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	cfg.Output.MaxPathLength = 0
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

	resp := generatePreview(movie, fileResults, "/library", cfg, "", false, false)

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
	if resp.NFOPaths[0] != filepath.Join(folderPath, "ABC-123.nfo") {
		t.Fatalf("NFOPaths[0] = %q", resp.NFOPaths[0])
	}
	if resp.NFOPaths[1] != filepath.Join(folderPath, "ABC-123.nfo") {
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
	cfg.Output.SubfolderFormat = []string{} // Disable subfolder for this test
	cfg.Output.DownloadExtrafanart = true   // Enable for screenshot preview
	cfg.Metadata.NFO.PerFile = false
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"

	movie := &models.Movie{
		ID:          "XYZ-999",
		Title:       "Fallback Title",
		Screenshots: []string{"shot"},
	}

	resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

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

			resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

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

			resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

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

	resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

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

	resp := generatePreview(movie, fileResults, "/library", cfg, "", false, false)

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

	resp := generatePreview(movie, fileResults, "/library", cfg, "", false, false)

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

	resp := generatePreview(movie, fileResults, "/library", cfg, "", false, false)

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

	resp := generatePreview(movie, fileResults, "/library", cfg, "", false, false)

	// PerFile=true means NFOPaths has multiple entries, but NFOPath is set to first for backward compatibility
	assert.Len(t, resp.NFOPaths, 2, "should have 2 NFO paths for per-file mode")
	assert.NotEmpty(t, resp.NFOPath, "NFOPath should be set to first NFO for backward compatibility")
}

func TestGeneratePreview_OperationMode(t *testing.T) {
	tests := []struct {
		name           string
		operationMode  organizer.OperationMode
		expectedInResp string
	}{
		{
			name:           "organize mode in response",
			operationMode:  organizer.OperationModeOrganize,
			expectedInResp: "organize",
		},
		{
			name:           "in-place mode in response",
			operationMode:  organizer.OperationModeInPlace,
			expectedInResp: "in-place",
		},
		{
			name:           "metadata-artwork mode in response",
			operationMode:  organizer.OperationModeMetadataArtwork,
			expectedInResp: "metadata-artwork",
		},
		{
			name:           "preview mode in response",
			operationMode:  organizer.OperationModePreview,
			expectedInResp: "preview",
		},
	}

	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := &models.Movie{
				ID:    "TEST-100",
				Title: "Operation Mode Test",
			}

			resp := generatePreview(movie, nil, "/library", cfg, tt.operationMode, false, false)

			assert.Equal(t, tt.expectedInResp, resp.OperationMode,
				"operation_mode in preview response should match")
		})
	}
}

func TestGeneratePreview_OperationModeDefaultBehavior(t *testing.T) {
	t.Run("empty operation mode appears as empty string in response", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		movie := &models.Movie{
			ID:    "TEST-101",
			Title: "Empty Mode Test",
		}

		resp := generatePreview(movie, nil, "/library", cfg, organizer.OperationMode(""), false, false)

		assert.Equal(t, "", resp.OperationMode,
			"empty operation_mode should pass through as empty string")
	})
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

	resp := generatePreview(movie, fileResults, "/library", cfg, "", false, false)

	assert.NotEmpty(t, resp.NFOPath, "NFOPath should be set when PerFile is false")
	assert.True(t, strings.Contains(resp.NFOPath, "TEST-007.nfo"), "NFO path should match template")
	assert.Empty(t, resp.NFOPaths, "NFOPaths should be empty when PerFile is false")
}

// TestGeneratePreview_InPlaceNoRenameFolder tests that in-place-norenamefolder preview
// shows files in source directory without folder hierarchy
func TestGeneratePreview_InPlaceNoRenameFolder(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID> [<STUDIO>]"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.DownloadPoster = true
	cfg.Output.DownloadExtrafanart = true
	cfg.Metadata.NFO.Enabled = true

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test Movie",
	}

	fileResults := []*worker.FileResult{
		{FilePath: "/source/videos/IPX-535.mp4"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg, organizer.OperationModeInPlaceNoRenameFolder, false, false)

	// Should use source directory as target, not /library
	assert.Equal(t, "in-place-norenamefolder", resp.OperationMode)
	assert.Equal(t, "/source/videos/IPX-535.mp4", filepath.ToSlash(resp.SourcePath), "SourcePath should be the original file path")
	assert.Contains(t, filepath.ToSlash(resp.FullPath), "/source/videos/", "In-place-norenamefolder should place files in source directory")
	assert.NotContains(t, filepath.ToSlash(resp.FullPath), "/library/", "In-place-norenamefolder should NOT use destination directory")
	assert.Empty(t, resp.FolderName, "In-place-norenamefolder should have no folder name (no folder creation)")
}

func TestGeneratePreview_WindowsPathFallbackUsesMatchedMovieID(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<TITLE>"

	movie := &models.Movie{
		ID:    "",
		Title: "",
	}

	fileResults := []*worker.FileResult{
		{
			FilePath: `C:\Users\me\test-videos\folder4\ABF-345.sd 5 (1).mkv`,
			MovieID:  "ABF-345",
			Status:   worker.JobStatusCompleted,
		},
	}

	resp := generatePreview(movie, fileResults, `C:\output`, cfg, organizer.OperationModeOrganize, true, true)

	assert.Equal(t, "ABF-345", resp.FileName)
	assert.Equal(t, "ABF-345", resp.FolderName)
	assert.Equal(t, `C:\output\ABF-345\ABF-345.mkv`, resp.FullPath)
	assert.Equal(t, []string{`C:\output\ABF-345\ABF-345.mkv`}, resp.VideoFiles)
}

func TestGeneratePreview_InPlaceNoRenameFolder_WindowsSourcePath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<TITLE>"

	movie := &models.Movie{
		ID:    "",
		Title: "",
	}

	fileResults := []*worker.FileResult{
		{
			FilePath: `C:\Users\me\test-videos\folder4\ABF-345.sd 5 (1).mkv`,
			MovieID:  "ABF-345",
			Status:   worker.JobStatusCompleted,
		},
	}

	resp := generatePreview(movie, fileResults, `C:\output`, cfg, organizer.OperationModeInPlaceNoRenameFolder, true, true)

	assert.Equal(t, "ABF-345", resp.FileName)
	assert.Equal(t, `C:\Users\me\test-videos\folder4\ABF-345.sd 5 (1).mkv`, resp.SourcePath)
	assert.Equal(t, `C:\Users\me\test-videos\folder4\ABF-345.mkv`, resp.FullPath)
	assert.Equal(t, []string{`C:\Users\me\test-videos\folder4\ABF-345.mkv`}, resp.VideoFiles)
}

func TestGeneratePreview_UNCSourcePath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"

	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie",
	}

	fileResults := []*worker.FileResult{
		{
			FilePath: `\\nas\media\videos\ABC-123.mp4`,
			MovieID:  "ABC-123",
			Status:   worker.JobStatusCompleted,
		},
	}

	t.Run("metadata-artwork preserves UNC source path", func(t *testing.T) {
		resp := generatePreview(movie, fileResults, `\\nas\output`, cfg, organizer.OperationModeMetadataArtwork, true, true)
		assert.Equal(t, `\\nas\media\videos\ABC-123.mp4`, resp.SourcePath)
		assert.Equal(t, `\\nas\media\videos\ABC-123.mp4`, resp.FullPath)
		assert.Equal(t, "ABC-123", resp.FileName)
	})

	t.Run("in-place-norenamefolder preserves UNC source dir", func(t *testing.T) {
		resp := generatePreview(movie, fileResults, `\\nas\output`, cfg, organizer.OperationModeInPlaceNoRenameFolder, true, true)
		assert.Equal(t, `\\nas\media\videos\ABC-123.mp4`, resp.SourcePath)
		assert.Equal(t, `\\nas\media\videos\ABC-123.mp4`, resp.FullPath)
		assert.Equal(t, "ABC-123", resp.FileName)
	})

	t.Run("in-place uses UNC destination when not dedicated", func(t *testing.T) {
		resp := generatePreview(movie, fileResults, `\\nas\output`, cfg, organizer.OperationModeInPlace, true, true)
		assert.Equal(t, `\\nas\media\videos\ABC-123.mp4`, resp.SourcePath)
		assert.Equal(t, "ABC-123", resp.FolderName)
		assert.Contains(t, resp.FullPath, `\\nas\output`)
	})

	t.Run("organize computes folder and subfolder under UNC destination", func(t *testing.T) {
		cfgOrganize := config.DefaultConfig()
		cfgOrganize.Output.FolderFormat = "<ID>"
		cfgOrganize.Output.FileFormat = "<ID>"
		cfgOrganize.Output.SubfolderFormat = []string{"<STUDIO>"}
		movieOrg := &models.Movie{ID: "ABC-123", Title: "Test", Maker: "StudioA"}
		fileResultsOrg := []*worker.FileResult{
			{FilePath: `\\nas\media\ABC-123.mp4`, MovieID: "ABC-123", Status: worker.JobStatusCompleted},
		}
		resp := generatePreview(movieOrg, fileResultsOrg, `\\nas\output`, cfgOrganize, organizer.OperationModeOrganize, true, true)
		assert.Equal(t, "ABC-123", resp.FolderName)
		assert.Equal(t, "StudioA", resp.SubfolderPath)
		assert.Contains(t, resp.FullPath, `\\nas\output`)
		assert.Contains(t, resp.FullPath, "ABC-123")
	})
}

func TestGeneratePreview_UNCShareRoot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"

	movie := &models.Movie{
		ID:    "XYZ-789",
		Title: "Test Movie",
	}

	t.Run("in-place preserves UNC share root", func(t *testing.T) {
		fileResults := []*worker.FileResult{
			{FilePath: `\\nas\share\XYZ-789.mp4`, MovieID: "XYZ-789", Status: worker.JobStatusCompleted},
		}
		resp := generatePreview(movie, fileResults, `\\nas\output`, cfg, organizer.OperationModeInPlace, true, true)
		assert.Equal(t, `\\nas\share\XYZ-789.mp4`, resp.SourcePath)
		assert.Contains(t, resp.FullPath, `\\nas\output`)
	})

	t.Run("metadata-artwork preserves UNC share root dir", func(t *testing.T) {
		fileResults := []*worker.FileResult{
			{FilePath: `\\nas\share\XYZ-789.mp4`, MovieID: "XYZ-789", Status: worker.JobStatusCompleted},
		}
		resp := generatePreview(movie, fileResults, `\\nas\output`, cfg, organizer.OperationModeMetadataArtwork, true, true)
		assert.Equal(t, `\\nas\share\XYZ-789.mp4`, resp.SourcePath)
		assert.Equal(t, `\\nas\share\XYZ-789.mp4`, resp.FullPath)
	})
}

func TestGeneratePreview_UNCMultipart(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID><PARTSUFFIX>"

	movie := &models.Movie{
		ID:    "MUL-001",
		Title: "Multipart Test",
	}

	fileResults := []*worker.FileResult{
		{FilePath: `\\nas\media\MUL-001-pt1.mp4`, MovieID: "MUL-001", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1", Status: worker.JobStatusCompleted},
		{FilePath: `\\nas\media\MUL-001-pt2.mp4`, MovieID: "MUL-001", IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2", Status: worker.JobStatusCompleted},
	}

	t.Run("each part gets its own filename", func(t *testing.T) {
		resp := generatePreview(movie, fileResults, `\\nas\output`, cfg, organizer.OperationModeInPlaceNoRenameFolder, true, true)
		assert.Len(t, resp.VideoFiles, 2)
		assert.Contains(t, resp.VideoFiles[0], "MUL-001-pt1")
		assert.Contains(t, resp.VideoFiles[1], "MUL-001-pt2")
		assert.NotEqual(t, resp.VideoFiles[0], resp.VideoFiles[1])
	})
}

// TestGeneratePreview_InPlace tests that in-place preview shows folder rename in source parent
func TestGeneratePreview_InPlace(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID> [<STUDIO>]"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.DownloadPoster = true
	cfg.Metadata.NFO.Enabled = true

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test Movie",
	}

	fileResults := []*worker.FileResult{
		{FilePath: "/source/videos/IPX-535.mp4"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg, organizer.OperationModeInPlace, false, false)

	assert.Equal(t, "in-place", resp.OperationMode)
	assert.Equal(t, "/source/videos/IPX-535.mp4", filepath.ToSlash(resp.SourcePath), "SourcePath should be the original file path")
	assert.Contains(t, filepath.ToSlash(resp.FullPath), "/source/", "In-place should use parent of source directory")
	assert.NotEmpty(t, resp.FolderName, "In-place should have a folder name for potential rename")
}

// TestGeneratePreview_MetadataArtwork tests that metadata-artwork preview shows no file changes
func TestGeneratePreview_MetadataArtwork(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.DownloadPoster = true
	cfg.Metadata.NFO.Enabled = true

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test Movie",
	}

	fileResults := []*worker.FileResult{
		{FilePath: "/source/videos/IPX-535.mp4"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg, organizer.OperationModeMetadataArtwork, false, false)

	assert.Equal(t, "metadata-artwork", resp.OperationMode)
	assert.Equal(t, "/source/videos/IPX-535.mp4", filepath.ToSlash(resp.SourcePath))
	assert.Equal(t, "/source/videos/IPX-535.mp4", filepath.ToSlash(resp.FullPath), "Metadata-artwork should keep original file path")
	assert.Empty(t, resp.FolderName, "Metadata-artwork should have no folder name")
	assert.Contains(t, filepath.ToSlash(resp.NFOPath), "/source/videos/", "NFO should be in source directory")
}

// TestGeneratePreview_OrganizeModeDefault tests that organize mode (default) uses destination directory
func TestGeneratePreview_SubfolderPath_NonOrganizeModes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.SubfolderFormat = []string{"<MAKER>"}
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"

	movie := &models.Movie{
		ID:    "MODE-001",
		Title: "Mode Test",
		Maker: "StudioC",
	}

	fileResults := []*worker.FileResult{
		{FilePath: "/source/MODE-001.mp4"},
	}

	t.Run("in-place mode has no subfolder_path", func(t *testing.T) {
		resp := generatePreview(movie, fileResults, "/library", cfg, organizer.OperationModeInPlace, false, false)
		assert.Empty(t, resp.SubfolderPath, "In-place mode should not have SubfolderPath")
	})

	t.Run("in-place-norenamefolder mode has no subfolder_path", func(t *testing.T) {
		resp := generatePreview(movie, fileResults, "/library", cfg, organizer.OperationModeInPlaceNoRenameFolder, false, false)
		assert.Empty(t, resp.SubfolderPath, "In-place-norenamefolder mode should not have SubfolderPath")
	})

	t.Run("metadata-artwork mode has no subfolder_path", func(t *testing.T) {
		resp := generatePreview(movie, fileResults, "/library", cfg, organizer.OperationModeMetadataArtwork, false, false)
		assert.Empty(t, resp.SubfolderPath, "Metadata-artwork mode should not have SubfolderPath")
	})
}

func TestGeneratePreview_OrganizeModeDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.DownloadPoster = true
	cfg.Metadata.NFO.Enabled = true

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test Movie",
	}

	fileResults := []*worker.FileResult{
		{FilePath: "/source/videos/IPX-535.mp4"},
	}

	resp := generatePreview(movie, fileResults, "/library", cfg, organizer.OperationModeOrganize, false, false)

	assert.Equal(t, "organize", resp.OperationMode)
	assert.Contains(t, filepath.ToSlash(resp.FullPath), "/library/", "Organize mode should use destination directory")
	assert.NotContains(t, filepath.ToSlash(resp.FullPath), "/source/", "Organize mode should NOT use source directory")
	assert.Equal(t, "", resp.SourcePath, "Organize mode should not set source path")
	assert.NotEmpty(t, resp.FolderName, "Organize mode should have a folder name")
}

func TestGeneratePreview_SubfolderPath(t *testing.T) {
	t.Run("subfolder_path populated with subfolder format", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.SubfolderFormat = []string{"<MAKER>", "<YEAR>"}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		releaseDate := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
		movie := &models.Movie{
			ID:          "ABC-123",
			Title:       "Test Movie",
			Maker:       "IdeaPocket",
			ReleaseDate: &releaseDate,
		}

		resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

		assert.Equal(t, filepath.Join("IdeaPocket", "2025"), resp.SubfolderPath, "SubfolderPath should contain subfolder parts joined by platform separator")
		assert.Contains(t, filepath.ToSlash(resp.FullPath), "IdeaPocket/2025", "FullPath should include subfolder hierarchy")
	})

	t.Run("subfolder_path empty when no subfolder format", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.SubfolderFormat = []string{}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		movie := &models.Movie{
			ID:    "XYZ-999",
			Title: "Test Movie",
		}

		resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

		assert.Equal(t, "", resp.SubfolderPath, "SubfolderPath should be empty when no subfolder format configured")
	})

	t.Run("subfolder_path with single subfolder", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.SubfolderFormat = []string{"<ID>"}
		cfg.Output.FolderFormat = "<ID> [<STUDIO>] - <TITLE> (<YEAR>)"
		cfg.Output.FileFormat = "<ID>"

		releaseDate := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
		movie := &models.Movie{
			ID:          "IPX-535",
			Title:       "Test Movie",
			Maker:       "IdeaPocket",
			ReleaseDate: &releaseDate,
		}

		resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

		assert.Equal(t, "IPX-535", resp.SubfolderPath, "SubfolderPath should contain single subfolder part")
		assert.NotEmpty(t, resp.FolderName, "FolderName should be populated")
		assert.NotEqual(t, resp.SubfolderPath, resp.FolderName, "FolderName and SubfolderPath should differ when formats differ")
	})

	t.Run("subfolder_path empty when template resolves to empty", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.SubfolderFormat = []string{"<YEAR>", "<MAKER>"}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		movie := &models.Movie{
			ID:    "NO-DATE-001",
			Title: "No Date Movie",
		}

		resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

		assert.Equal(t, "", resp.SubfolderPath, "SubfolderPath should be empty when all templates resolve to empty")
		assert.Contains(t, filepath.ToSlash(resp.FullPath), "/library/NO-DATE-001", "FullPath should skip empty subfolders")
	})

	t.Run("subfolder_path partially resolves", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.SubfolderFormat = []string{"<MAKER>", "<YEAR>"}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		movie := &models.Movie{
			ID:    "PARTIAL-001",
			Title: "Partial Subfolder",
			Maker: "S1",
		}

		resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

		assert.Equal(t, "S1", resp.SubfolderPath, "SubfolderPath should only contain non-empty parts")
		assert.Contains(t, filepath.ToSlash(resp.FullPath), "S1/", "FullPath should include non-empty subfolder")
	})

	t.Run("subfolder_path with special characters sanitized", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.SubfolderFormat = []string{"<MAKER>", "<LABEL>"}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		movie := &models.Movie{
			ID:    "SAN-001",
			Title: "Sanitize Test",
			Maker: "Studio: Tokyo",
			Label: `Label? "Test"`,
		}

		resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

		assert.NotContains(t, resp.SubfolderPath, ":", "SubfolderPath should not contain colons")
		assert.NotContains(t, resp.SubfolderPath, "?", "SubfolderPath should not contain question marks")
		assert.NotContains(t, resp.SubfolderPath, `"`, "SubfolderPath should not contain double quotes")
		assert.NotContains(t, filepath.ToSlash(resp.FullPath), ":", "FullPath should not contain unsanitized characters")
	})

	t.Run("subfolder_path uses platform separator consistent with FullPath", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.SubfolderFormat = []string{"<MAKER>", "<YEAR>"}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		releaseDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
		movie := &models.Movie{
			ID:          "CROSS-001",
			Title:       "Cross Platform",
			Maker:       "StudioA",
			ReleaseDate: &releaseDate,
		}

		resp := generatePreview(movie, nil, "/library", cfg, "", false, false)

		expectedSubfolderParts := []string{"StudioA", "2025"}
		expectedSubfolderPath := filepath.Join(expectedSubfolderParts...)
		assert.Equal(t, expectedSubfolderPath, resp.SubfolderPath, "SubfolderPath should use platform path separator")

		expectedFullPath := filepath.Join("/library", "StudioA", "2025", "CROSS-001", "CROSS-001.mp4")
		assert.Equal(t, filepath.ToSlash(expectedFullPath), filepath.ToSlash(resp.FullPath), "FullPath should match subfolder hierarchy")
	})

	t.Run("subfolder_path with multipart files", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.SubfolderFormat = []string{"<MAKER>"}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID><IF:MULTIPART>-pt<PART></IF>"
		cfg.Output.DownloadPoster = true
		cfg.Output.DownloadExtrafanart = true

		movie := &models.Movie{
			ID:    "MULTI-001",
			Title: "Multi Part Movie",
			Maker: "StudioB",
		}

		fileResults := []*worker.FileResult{
			{FilePath: "/source/MULTI-001-pt1.mp4", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
			{FilePath: "/source/MULTI-001-pt2.mp4", IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"},
		}

		resp := generatePreview(movie, fileResults, "/library", cfg, "", false, false)

		assert.Equal(t, "StudioB", resp.SubfolderPath, "SubfolderPath should work with multipart files")
		assert.Len(t, resp.VideoFiles, 2, "Should have 2 video files")
		for _, vf := range resp.VideoFiles {
			assert.Contains(t, filepath.ToSlash(vf), "StudioB/", "All video file paths should include subfolder")
		}
	})
}

func TestGeneratePreview_StrategyParity(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.RenameFile = true

	modes := []organizer.OperationMode{
		organizer.OperationModeOrganize,
		organizer.OperationModeInPlace,
		organizer.OperationModeInPlaceNoRenameFolder,
		organizer.OperationModeMetadataArtwork,
	}

	for _, mode := range modes {
		t.Run(string(mode)+"_parity", func(t *testing.T) {
			movie := &models.Movie{
				ID:    "PARITY-001",
				Title: "Parity Test",
			}

			fileResults := []*worker.FileResult{
				{FilePath: "/source/PARITY-001.mp4", MovieID: "PARITY-001"},
			}

			destination := "/library"
			resp := generatePreview(movie, fileResults, destination, cfg, mode, true, true)

			outputConfig := cfg.Output
			outputConfig.OperationMode = mode
			outputConfig.MaxPathLength = 0

			fs := afero.NewOsFs()
			sharedEngine := template.NewEngine()
			fileMatcher, _ := matcher.NewMatcher(&cfg.Matching)

			var strategy organizer.OperationStrategy
			switch mode {
			case types.OperationModeOrganize:
				strategy = organizer.NewOrganizeStrategy(fs, &outputConfig, sharedEngine)
			case types.OperationModeInPlace:
				if fileMatcher != nil {
					strategy = organizer.NewInPlaceStrategy(fs, &outputConfig, fileMatcher, sharedEngine)
				} else {
					strategy = organizer.NewOrganizeStrategy(fs, &outputConfig, sharedEngine)
				}
			case types.OperationModeInPlaceNoRenameFolder:
				if fileMatcher != nil {
					strategy = organizer.NewInPlaceNoRenameFolderStrategy(fs, &outputConfig, fileMatcher, sharedEngine)
				} else {
					strategy = organizer.NewOrganizeStrategy(fs, &outputConfig, sharedEngine)
				}
			case types.OperationModeMetadataArtwork:
				strategy = organizer.NewMetadataArtworkStrategy(fs, &outputConfig)
			default:
				strategy = organizer.NewOrganizeStrategy(fs, &outputConfig, sharedEngine)
			}

			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      "/source/PARITY-001.mp4",
					Name:      "PARITY-001.mp4",
					Extension: ".mp4",
					Dir:       "/source",
				},
				ID: "PARITY-001",
			}

			plan, err := strategy.Plan(match, movie, destination, false)
			require.NoError(t, err)

			assert.Equal(t, filepath.ToSlash(plan.TargetPath), filepath.ToSlash(resp.FullPath),
				"preview FullPath should match strategy TargetPath for mode %s", mode)

			if mode != types.OperationModeOrganize {
				assert.Equal(t, filepath.ToSlash(plan.SourcePath), filepath.ToSlash(resp.SourcePath),
					"preview SourcePath should match strategy SourcePath for mode %s", mode)
			}

			assert.Equal(t, plan.FolderName, resp.FolderName,
				"preview FolderName should match plan FolderName for mode %s", mode)
			assert.Equal(t, filepath.ToSlash(plan.SubfolderPath), filepath.ToSlash(resp.SubfolderPath),
				"preview SubfolderPath should match plan SubfolderPath for mode %s", mode)
		})
	}
}

func TestGeneratePreview_StrategyParity_InPlaceRename(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID> [<STUDIO>]"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.RenameFile = true

	tmpDir := t.TempDir()
	dedicatedDir := filepath.Join(tmpDir, "old-folder-name")
	require.NoError(t, os.MkdirAll(dedicatedDir, 0755))

	movie := &models.Movie{
		ID:    "INPLACE-001",
		Title: "In-Place Rename Test",
		Maker: "StudioA",
	}

	sourceFile := filepath.Join(dedicatedDir, "INPLACE-001.mp4")
	require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

	fileResults := []*worker.FileResult{
		{FilePath: sourceFile, MovieID: "INPLACE-001"},
	}

	destination := "/library"
	resp := generatePreview(movie, fileResults, destination, cfg, organizer.OperationModeInPlace, true, true)

	outputConfig := cfg.Output
	outputConfig.OperationMode = organizer.OperationModeInPlace
	outputConfig.MaxPathLength = 0

	fs := afero.NewOsFs()
	sharedEngine := template.NewEngine()
	fileMatcher, _ := matcher.NewMatcher(&cfg.Matching)
	require.NotNil(t, fileMatcher, "matcher required for in-place rename test")

	strategy := organizer.NewInPlaceStrategy(fs, &outputConfig, fileMatcher, sharedEngine)

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "INPLACE-001.mp4",
			Extension: ".mp4",
			Dir:       dedicatedDir,
		},
		ID: "INPLACE-001",
	}

	plan, err := strategy.Plan(match, movie, destination, false)
	require.NoError(t, err)

	assert.True(t, plan.InPlace, "plan should detect in-place rename with dedicated folder")
	assert.Equal(t, filepath.ToSlash(plan.TargetPath), filepath.ToSlash(resp.FullPath),
		"preview FullPath should match strategy TargetPath for in-place rename")
	assert.Equal(t, plan.FolderName, resp.FolderName,
		"preview FolderName should match plan FolderName for in-place rename")
}
