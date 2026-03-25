package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
)

type runBatchTestScraper struct {
	name     string
	enabled  bool
	mappings map[string]string
	results  map[string]*models.ScraperResult
	errors   map[string]error
	calls    []string
}

func (s *runBatchTestScraper) Name() string { return s.name }

func (s *runBatchTestScraper) Search(id string) (*models.ScraperResult, error) {
	s.calls = append(s.calls, id)
	if err := s.errors[id]; err != nil {
		return nil, err
	}
	if err := s.errors[strings.ToUpper(id)]; err != nil {
		return nil, err
	}
	if err := s.errors[strings.ToLower(id)]; err != nil {
		return nil, err
	}
	if result := s.results[id]; result != nil {
		copy := *result
		return &copy, nil
	}
	if result := s.results[strings.ToUpper(id)]; result != nil {
		copy := *result
		return &copy, nil
	}
	if result := s.results[strings.ToLower(id)]; result != nil {
		copy := *result
		return &copy, nil
	}
	return nil, errors.New("not found")
}

func (s *runBatchTestScraper) GetURL(id string) (string, error) { return "", nil }

func (s *runBatchTestScraper) IsEnabled() bool { return s.enabled }

func (s *runBatchTestScraper) ResolveSearchQuery(input string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(input))
	query, ok := s.mappings[key]
	return query, ok
}

func (s *runBatchTestScraper) Close() error { return nil }

func (s *runBatchTestScraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{Enabled: s.enabled}
}

func newRunBatchTestEnv(t *testing.T, scraperName string) (*config.Config, *database.DB, *database.MovieRepository, *aggregator.Aggregator, *matcher.Matcher) {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  filepath.Join(t.TempDir(), "run-batch.db"),
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{scraperName},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:          []string{scraperName},
				ContentID:   []string{scraperName},
				Title:       []string{scraperName},
				Description: []string{scraperName},
				Maker:       []string{scraperName},
				Actress:     []string{scraperName},
				Genre:       []string{scraperName},
			},
			NFO: config.NFOConfig{
				FilenameTemplate: "<ID>.nfo",
				DisplayName:      "[<ID>] <TITLE>",
			},
		},
		Matching: config.MatchingConfig{
			Extensions:   []string{".mp4", ".mkv"},
			RegexPattern: `(?i)([a-z]{2,5}-?\d{2,5})`,
			RegexEnabled: true,
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	fileMatcher, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		t.Fatalf("matcher.NewMatcher() error = %v", err)
	}

	return cfg, db, database.NewMovieRepository(db), aggregator.NewWithDatabase(cfg, db), fileMatcher
}

func testRunBatchResult(source, id, title, contentID string) *models.ScraperResult {
	releaseDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	return &models.ScraperResult{
		Source:      source,
		Language:    "ja",
		ID:          id,
		ContentID:   contentID,
		Title:       title,
		Description: "description",
		Maker:       "maker",
		Genres:      []string{"Drama"},
		ReleaseDate: &releaseDate,
		Runtime:     120,
	}
}

func TestScraperListContains(t *testing.T) {
	if !scraperListContains([]string{" DMM ", "javbus"}, "dmm") {
		t.Fatal("expected scraperListContains to match case-insensitively")
	}
	if scraperListContains([]string{"javdb"}, "dmm") {
		t.Fatal("expected scraperListContains to return false for missing scraper")
	}
}

func TestRunBatchScrapeOnce_MatcherFallbackRoutesToResolverAndSkipsPersistence(t *testing.T) {
	cfg, _, movieRepo, agg, fileMatcher := newRunBatchTestEnv(t, "resolver")

	registry := models.NewScraperRegistry()
	scraper := &runBatchTestScraper{
		name:    "resolver",
		enabled: true,
		mappings: map[string]string{
			"special-title": "SPECIAL-777",
		},
		results: map[string]*models.ScraperResult{
			"SPECIAL-777": testRunBatchResult("resolver", "SPECIAL-777", "Resolved Title", "special777"),
		},
		errors: map[string]error{},
	}
	registry.Register(scraper)

	job := &BatchJob{ID: "job-custom"}
	filePath := filepath.Join(t.TempDir(), "special-title.mp4")

	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		job,
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
	if result.Status != JobStatusCompleted {
		t.Fatalf("result.Status = %q, want completed", result.Status)
	}
	if movie == nil || movie.ID != "SPECIAL-777" {
		t.Fatalf("movie = %#v", movie)
	}
	if len(scraper.calls) != 1 || scraper.calls[0] != "SPECIAL-777" {
		t.Fatalf("scraper.calls = %#v", scraper.calls)
	}
	if _, err := movieRepo.FindByID("SPECIAL-777"); err == nil {
		t.Fatal("expected custom scraper mode to skip database persistence")
	}
}

func TestRunBatchScrapeOnce_MatcherFallbackWithoutResolverFails(t *testing.T) {
	cfg, _, movieRepo, agg, fileMatcher := newRunBatchTestEnv(t, "resolver")

	registry := models.NewScraperRegistry()
	registry.Register(&runBatchTestScraper{
		name:     "resolver",
		enabled:  true,
		mappings: map[string]string{},
		results:  map[string]*models.ScraperResult{},
		errors:   map[string]error{},
	})

	filePath := filepath.Join(t.TempDir(), "mystery-title.mp4")
	_, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: "job-no-match"},
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
	if err == nil {
		t.Fatal("expected error when no scraper resolver matches")
	}
	if result == nil || result.Status != JobStatusFailed {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.Error, "No scraper query resolver matched filename input") {
		t.Fatalf("result.Error = %q", result.Error)
	}
}

func TestRunBatchScrapeOnce_RetriesOriginalIDAfterMappedQueryFails(t *testing.T) {
	cfg, _, movieRepo, agg, fileMatcher := newRunBatchTestEnv(t, "resolver")

	registry := models.NewScraperRegistry()
	scraper := &runBatchTestScraper{
		name:    "resolver",
		enabled: true,
		mappings: map[string]string{
			"abc-123": "ALT-123",
		},
		results: map[string]*models.ScraperResult{
			"ABC-123":   testRunBatchResult("resolver", "ABC-123", "Recovered Title", "abc123"),
			"abc-123":   testRunBatchResult("resolver", "ABC-123", "Recovered Title", "abc123"),
			"ABC123":    testRunBatchResult("resolver", "ABC-123", "Recovered Title", "abc123"),
			"abc123":    testRunBatchResult("resolver", "ABC-123", "Recovered Title", "abc123"),
			"ABC-00123": testRunBatchResult("resolver", "ABC-123", "Recovered Title", "abc123"),
			"ABC00123":  testRunBatchResult("resolver", "ABC-123", "Recovered Title", "abc123"),
			"abc-00123": testRunBatchResult("resolver", "ABC-123", "Recovered Title", "abc123"),
			"abc00123":  testRunBatchResult("resolver", "ABC-123", "Recovered Title", "abc123"),
		},
		errors: map[string]error{
			"ALT-123": errors.New("mapped query failed"),
		},
	}
	registry.Register(scraper)

	filePath := filepath.Join(t.TempDir(), "ABC-123.mp4")
	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: "job-retry"},
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
		t.Fatalf("RunBatchScrapeOnce() error = %v (calls=%#v)", err, scraper.calls)
	}
	if result.Status != JobStatusCompleted {
		t.Fatalf("result.Status = %q, want completed", result.Status)
	}
	if got, want := scraper.calls, []string{"ALT-123", "ABC-123"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("scraper.calls = %#v, want %#v", got, want)
	}
	if movie == nil || movie.Title != "Recovered Title" {
		t.Fatalf("movie = %#v", movie)
	}
	cached, err := movieRepo.FindByID("ABC-123")
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if cached.Title != "Recovered Title" {
		t.Fatalf("cached.Title = %q", cached.Title)
	}
}

func TestRunBatchScrapeOnce_UpdateModeMergesExistingNFO(t *testing.T) {
	cfg, _, movieRepo, agg, fileMatcher := newRunBatchTestEnv(t, "resolver")
	cfg.Metadata.NFO.DisplayName = "[<ID>] <TITLE>"

	registry := models.NewScraperRegistry()
	registry.Register(&runBatchTestScraper{
		name:     "resolver",
		enabled:  true,
		mappings: map[string]string{},
		results: map[string]*models.ScraperResult{
			"ABC-123": testRunBatchResult("resolver", "ABC-123", "Scraped Title", "abc123"),
		},
		errors: map[string]error{},
	})

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "ABC-123.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{})
	if err := gen.WriteNFO(&nfo.Movie{ID: "ABC-123", Title: "Existing NFO Title"}, filepath.Join(tempDir, "ABC-123.nfo")); err != nil {
		t.Fatalf("WriteNFO() error = %v", err)
	}

	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: "job-update-merge"},
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
		true,
		nil,
		nil,
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
	if movie.Title != "Existing NFO Title" {
		t.Fatalf("movie.Title = %q", movie.Title)
	}
	if movie.DisplayName != "[ABC-123] Existing NFO Title" {
		t.Fatalf("movie.DisplayName = %q", movie.DisplayName)
	}
}

func TestRunBatchScrapeOnce_ForceRefreshReplacesCachedMovie(t *testing.T) {
	cfg, _, movieRepo, agg, fileMatcher := newRunBatchTestEnv(t, "resolver")

	if err := movieRepo.Upsert(&models.Movie{
		ID:        "IPX-777",
		ContentID: "ipx777",
		Title:     "Cached Title",
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	registry := models.NewScraperRegistry()
	registry.Register(&runBatchTestScraper{
		name:     "resolver",
		enabled:  true,
		mappings: map[string]string{},
		results: map[string]*models.ScraperResult{
			"IPX-777": testRunBatchResult("resolver", "IPX-777", "Fresh Title", "ipx777"),
		},
		errors: map[string]error{},
	})

	filePath := filepath.Join(t.TempDir(), "IPX-777.mp4")
	movie, result, err := RunBatchScrapeOnce(
		context.Background(),
		&BatchJob{ID: "job-force-refresh"},
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
		true,
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
		t.Fatalf("result.Status = %q, want completed", result.Status)
	}
	if movie.Title != "Fresh Title" {
		t.Fatalf("movie.Title = %q", movie.Title)
	}
	cached, err := movieRepo.FindByID("IPX-777")
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if cached.Title != "Fresh Title" {
		t.Fatalf("cached.Title = %q", cached.Title)
	}
}
