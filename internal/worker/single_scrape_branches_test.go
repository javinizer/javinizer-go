package worker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

func newRunBatchFallbackEnv(t *testing.T) (*config.Config, *database.MovieRepository, *aggregator.Aggregator, *matcher.Matcher) {
	t.Helper()
	cfg, _, movieRepo, agg, fileMatcher := newRunBatchTestEnv(t, "resolver")
	return cfg, movieRepo, agg, fileMatcher
}

func TestRunBatchScrapeOnce_QueryOverrideUsesManualQuery(t *testing.T) {
	cfg, movieRepo, agg, fileMatcher := newRunBatchFallbackEnv(t)

	registry := models.NewScraperRegistry()
	scraper := &runBatchTestScraper{
		name:    "resolver",
		enabled: true,
		results: map[string]*models.ScraperResult{
			"MANUAL-321": testRunBatchResult("resolver", "MANUAL-321", "Manual Result", "manual321"),
		},
		errors: map[string]error{},
	}
	registry.Register(scraper)

	filePath := filepath.Join(t.TempDir(), "IGNORED-NAME.mp4")
	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: "job-query-override"},
		filePath,
		0,
		"MANUAL-321",
		registry,
		agg,
		movieRepo,
		fileMatcher,
		nil,
		"test-agent",
		"test-ref",
		false,
		false,
		nil,
		nil,
		cfg,
		"prefer-scraper",
		"merge",
	)
	if err != nil {
		t.Fatalf("RunBatchScrapeOnce() error = %v", err)
	}
	if result.MovieID != "MANUAL-321" {
		t.Fatalf("result.MovieID = %q", result.MovieID)
	}
	if movie == nil || movie.Title != "Manual Result" {
		t.Fatalf("movie = %#v", movie)
	}
	if len(scraper.calls) != 1 || scraper.calls[0] != "MANUAL-321" {
		t.Fatalf("scraper.calls = %#v", scraper.calls)
	}
}

func TestRunBatchScrapeOnce_ReturnsCachedMovieWithoutScraping(t *testing.T) {
	cfg, movieRepo, agg, fileMatcher := newRunBatchFallbackEnv(t)
	if err := movieRepo.Upsert(&models.Movie{
		ID:        "IPX-111",
		ContentID: "ipx111",
		Title:     "Cached Winner",
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	registry := models.NewScraperRegistry()
	scraper := &runBatchTestScraper{
		name:    "resolver",
		enabled: true,
		results: map[string]*models.ScraperResult{
			"IPX-111": testRunBatchResult("resolver", "IPX-111", "Fresh Result", "ipx111"),
		},
		errors: map[string]error{},
	}
	registry.Register(scraper)

	filePath := filepath.Join(t.TempDir(), "IPX-111.mp4")
	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: "job-cache-hit"},
		filePath,
		0,
		"",
		registry,
		agg,
		movieRepo,
		fileMatcher,
		nil,
		"test-agent",
		"test-ref",
		false,
		false,
		nil,
		nil,
		cfg,
		"prefer-scraper",
		"merge",
	)
	if err != nil {
		t.Fatalf("RunBatchScrapeOnce() error = %v", err)
	}
	if result.Status != JobStatusCompleted {
		t.Fatalf("result.Status = %q", result.Status)
	}
	if movie == nil || movie.Title != "Cached Winner" {
		t.Fatalf("movie = %#v", movie)
	}
	if len(scraper.calls) != 0 {
		t.Fatalf("expected scraper not to run, got %#v", scraper.calls)
	}
}

func TestRunBatchScrapeOnce_AllScrapersFail(t *testing.T) {
	cfg, movieRepo, agg, fileMatcher := newRunBatchFallbackEnv(t)

	registry := models.NewScraperRegistry()
	registry.Register(&runBatchTestScraper{
		name:    "resolver",
		enabled: true,
		errors: map[string]error{
			"ABC-123": errors.New("primary scraper failed"),
		},
		results: map[string]*models.ScraperResult{},
	})
	registry.Register(&runBatchTestScraper{
		name:    "backup",
		enabled: true,
		errors: map[string]error{
			"ABC-123": errors.New("backup scraper failed"),
		},
		results: map[string]*models.ScraperResult{},
	})

	filePath := filepath.Join(t.TempDir(), "ABC-123.mp4")
	_, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: "job-all-fail"},
		filePath,
		0,
		"",
		registry,
		agg,
		movieRepo,
		fileMatcher,
		nil,
		"test-agent",
		"test-ref",
		false,
		false,
		nil,
		nil,
		cfg,
		"prefer-scraper",
		"merge",
	)
	if err == nil {
		t.Fatal("expected RunBatchScrapeOnce to fail")
	}
	if result == nil || result.Status != JobStatusFailed {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.Error, "resolver") || !strings.Contains(result.Error, "backup") {
		t.Fatalf("result.Error = %q", result.Error)
	}
}

func TestRunBatchScrapeOnce_ContextCancelledBeforeScrape(t *testing.T) {
	cfg, movieRepo, agg, fileMatcher := newRunBatchFallbackEnv(t)

	registry := models.NewScraperRegistry()
	registry.Register(&runBatchTestScraper{
		name:     "resolver",
		enabled:  true,
		results:  map[string]*models.ScraperResult{},
		errors:   map[string]error{},
		mappings: map[string]string{},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	filePath := filepath.Join(t.TempDir(), "ABC-123.mp4")
	_, result, err := RunBatchScrapeOnce(
		ctx,
		&BatchJob{ID: "job-cancelled"},
		filePath,
		0,
		"",
		registry,
		agg,
		movieRepo,
		fileMatcher,
		nil,
		"test-agent",
		"test-ref",
		false,
		false,
		nil,
		nil,
		cfg,
		"prefer-scraper",
		"merge",
	)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if result == nil || result.Status != JobStatusCancelled {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunBatchScrapeOnce_DMMSelectionFallbackBranches(t *testing.T) {
	cfg, movieRepo, agg, fileMatcher := newRunBatchFallbackEnv(t)

	t.Run("selected scrapers exclude dmm", func(t *testing.T) {
		registry := models.NewScraperRegistry()
		scraper := &runBatchTestScraper{
			name:    "resolver",
			enabled: true,
			results: map[string]*models.ScraperResult{
				"ABC-123": testRunBatchResult("resolver", "ABC-123", "No DMM Resolution", "abc123"),
			},
			errors: map[string]error{},
		}
		registry.Register(scraper)

		filePath := filepath.Join(t.TempDir(), "ABC-123.mp4")
		movie, result, err := RunBatchScrapeOnce(
			context.Background(),
			&BatchJob{ID: "job-no-dmm"},
			filePath,
			0,
			"",
			registry,
			agg,
			movieRepo,
			fileMatcher,
			nil,
			"test-agent",
			"test-ref",
			false,
			false,
			[]string{"resolver"},
			nil,
			cfg,
			"prefer-scraper",
			"merge",
		)
		if err != nil {
			t.Fatalf("RunBatchScrapeOnce() error = %v", err)
		}
		if result.Status != JobStatusCompleted || movie.Title != "No DMM Resolution" {
			t.Fatalf("result = %#v movie = %#v", result, movie)
		}
		if got, want := scraper.calls, []string{"ABC-123"}; len(got) != len(want) || got[0] != want[0] {
			t.Fatalf("scraper.calls = %#v", scraper.calls)
		}
	})

	t.Run("dmm type assertion failure uses original id", func(t *testing.T) {
		registry := models.NewScraperRegistry()
		registry.Register(&runBatchTestScraper{
			name:    "dmm",
			enabled: true,
			results: map[string]*models.ScraperResult{},
			errors:  map[string]error{},
		})
		scraper := &runBatchTestScraper{
			name:    "resolver",
			enabled: true,
			results: map[string]*models.ScraperResult{
				"ABC-123": testRunBatchResult("resolver", "ABC-123", "Original ID Used", "abc123"),
			},
			errors: map[string]error{},
		}
		registry.Register(scraper)

		filePath := filepath.Join(t.TempDir(), "ABC-123.mp4")
		movie, _, err := RunBatchScrapeOnce(
			context.Background(),
			&BatchJob{ID: "job-dmm-assert"},
			filePath,
			0,
			"",
			registry,
			agg,
			movieRepo,
			fileMatcher,
			nil,
			"test-agent",
			"test-ref",
			false,
			false,
			nil,
			nil,
			cfg,
			"prefer-scraper",
			"merge",
		)
		if err != nil {
			t.Fatalf("RunBatchScrapeOnce() error = %v", err)
		}
		if movie == nil || movie.Title != "Original ID Used" {
			t.Fatalf("movie = %#v", movie)
		}
	})
}
