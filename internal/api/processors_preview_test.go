package api

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
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
