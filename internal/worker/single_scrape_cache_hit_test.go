package worker

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
)

func newCacheHitTestEnv(t *testing.T) (*config.Config, *database.MovieRepository, *aggregator.Aggregator, *matcher.Matcher) {
	t.Helper()
	cfg, _, movieRepo, agg, fileMatcher := newRunBatchTestEnv(t, "resolver")
	cfg.Metadata.NFO.PerFile = true
	cfg.Metadata.NFO.DisplayTitle = "[<ID>] <TITLE>"
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"
	return cfg, movieRepo, agg, fileMatcher
}

func TestRunBatchScrapeOnce_CacheHitReusesPosterAndLegacyPerFileNFO(t *testing.T) {
	cfg, movieRepo, agg, fileMatcher := newCacheHitTestEnv(t)

	cachedMovie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Cached Title",
		Maker:     "Cached Maker",
	}
	if _, err := movieRepo.Upsert(cachedMovie); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	registry := models.NewScraperRegistry()
	registry.Register(&runBatchTestScraper{
		name:    "resolver",
		enabled: true,
		results: map[string]*models.ScraperResult{},
		errors:  map[string]error{},
	})

	jobID := "job-cache-legacy-nfo"
	tempPosterDir := filepath.Join("data", "temp", "posters", jobID)
	t.Cleanup(func() {
		_ = os.RemoveAll(tempPosterDir)
	})
	if err := os.MkdirAll(tempPosterDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempPosterDir, "ABC-123.jpg"), []byte("poster"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "ABC-123-pt1.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{})
	legacyNFOPath := filepath.Join(sourceDir, "ABC-123-pt1.nfo")
	if err := gen.WriteNFO(&nfo.Movie{ID: "ABC-123", Title: "Existing NFO Title"}, legacyNFOPath); err != nil {
		t.Fatalf("WriteNFO() error = %v", err)
	}

	processedMovieIDs := map[string]bool{"ABC-123": true}
	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: jobID},
		filePath,
		0,
		"",
		registry,
		agg,
		movieRepo,
		fileMatcher,
		http.DefaultClient,
		"test-agent",
		"test-ref",
		false,
		true,
		nil,
		nil,
		processedMovieIDs,
		cfg,
		"prefer-nfo",
		"merge",
	)
	if err != nil {
		t.Fatalf("RunBatchScrapeOnce() error = %v", err)
	}

	if result.Status != JobStatusCompleted {
		t.Fatalf("result.Status = %q, want completed", result.Status)
	}
	if !result.IsMultiPart || result.PartNumber != 1 || result.PartSuffix != "-pt1" {
		t.Fatalf("multipart result = %#v", result)
	}
	if movie == nil {
		t.Fatal("expected movie")
	}
	if movie.Title != "Existing NFO Title" {
		t.Fatalf("movie.Title = %q", movie.Title)
	}
	if movie.DisplayTitle != "[ABC-123] Existing NFO Title" {
		t.Fatalf("movie.DisplayTitle = %q", movie.DisplayTitle)
	}
	if movie.CroppedPosterURL != "/api/v1/temp/posters/"+jobID+"/ABC-123.jpg" {
		t.Fatalf("movie.CroppedPosterURL = %q", movie.CroppedPosterURL)
	}
}

func TestRunBatchScrapeOnce_CacheHitRegeneratesPosterAndAppliesDisplayTitle(t *testing.T) {
	cfg, movieRepo, agg, fileMatcher := newCacheHitTestEnv(t)
	cfg.Metadata.NFO.PerFile = false
	cfg.Metadata.NFO.FilenameTemplate = "<INVALID"

	posterData := makePosterJPEG(t)
	posterServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(posterData)
	}))
	defer posterServer.Close()

	cachedMovie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Cached Title",
		CoverURL:  posterServer.URL + "/poster.jpg",
	}
	if _, err := movieRepo.Upsert(cachedMovie); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	registry := models.NewScraperRegistry()
	jobID := "job-cache-regenerate"
	tempPosterDir := filepath.Join("data", "temp", "posters", jobID)
	t.Cleanup(func() {
		_ = os.RemoveAll(tempPosterDir)
	})

	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "ABC-123.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{})
	if err := gen.WriteNFO(&nfo.Movie{ID: "ABC-123", Title: "Existing Plain Title"}, filepath.Join(sourceDir, "ABC-123.nfo")); err != nil {
		t.Fatalf("WriteNFO() error = %v", err)
	}

	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: jobID},
		filePath,
		0,
		"",
		registry,
		agg,
		movieRepo,
		fileMatcher,
		posterServer.Client(),
		"test-agent",
		"test-ref",
		false,
		true,
		nil,
		nil,
		map[string]bool{"ABC-123": true},
		cfg,
		"prefer-nfo",
		"merge",
	)
	if err != nil {
		t.Fatalf("RunBatchScrapeOnce() error = %v", err)
	}
	if result.Status != JobStatusCompleted {
		t.Fatalf("result.Status = %q", result.Status)
	}
	if movie.DisplayTitle != "[ABC-123] Existing Plain Title" {
		t.Fatalf("movie.DisplayTitle = %q", movie.DisplayTitle)
	}
	if movie.CroppedPosterURL != "/api/v1/temp/posters/"+jobID+"/ABC-123.jpg" {
		t.Fatalf("movie.CroppedPosterURL = %q", movie.CroppedPosterURL)
	}
	if _, err := os.Stat(filepath.Join(tempPosterDir, "ABC-123.jpg")); err != nil {
		t.Fatalf("expected regenerated poster: %v", err)
	}
}

func TestRunBatchScrapeOnce_UpdateModeSkipsDuplicatePosterGenerationForMultipart(t *testing.T) {
	cfg, movieRepo, agg, fileMatcher := newCacheHitTestEnv(t)
	cfg.Metadata.NFO.FilenameTemplate = "<INVALID"

	registry := models.NewScraperRegistry()
	registry.Register(&runBatchTestScraper{
		name:    "resolver",
		enabled: true,
		results: map[string]*models.ScraperResult{
			"ABC-123": testRunBatchResult("resolver", "ABC-123", "Scraped Title", "abc123"),
		},
		errors: map[string]error{},
	})

	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "ABC-123-pt1.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{})
	if err := gen.WriteNFO(&nfo.Movie{ID: "ABC-123", Title: "Existing Plain Title"}, filepath.Join(sourceDir, "ABC-123.nfo")); err != nil {
		t.Fatalf("WriteNFO() error = %v", err)
	}

	jobID := "job-update-skip-poster"
	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: jobID},
		filePath,
		0,
		"",
		registry,
		agg,
		movieRepo,
		fileMatcher,
		http.DefaultClient,
		"test-agent",
		"test-ref",
		false,
		true,
		nil,
		nil,
		map[string]bool{"ABC-123": true},
		cfg,
		"prefer-nfo",
		"merge",
	)
	if err != nil {
		t.Fatalf("RunBatchScrapeOnce() error = %v", err)
	}
	if result.Status != JobStatusCompleted {
		t.Fatalf("result.Status = %q", result.Status)
	}
	if !result.IsMultiPart || result.PartNumber != 1 || result.PartSuffix != "-pt1" {
		t.Fatalf("multipart result = %#v", result)
	}
	if movie.DisplayTitle != "[ABC-123] Existing Plain Title" {
		t.Fatalf("movie.DisplayTitle = %q", movie.DisplayTitle)
	}
	if movie.CroppedPosterURL != "/api/v1/temp/posters/"+jobID+"/ABC-123.jpg" {
		t.Fatalf("movie.CroppedPosterURL = %q", movie.CroppedPosterURL)
	}
}

func makePosterJPEG(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1500))
	for y := 0; y < 1500; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 120, B: 180, A: 255})
		}
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode() error = %v", err)
	}

	return buf.Bytes()
}
