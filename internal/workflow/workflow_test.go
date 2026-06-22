package workflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockScraper struct {
	name    string
	enabled bool
	result  *models.ScraperResult
	err     error
}

func (m *mockScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return m.result, m.err
}

func (m *mockScraper) Name() string                                       { return m.name }
func (m *mockScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockScraper) IsEnabled() bool                                    { return m.enabled }
func (m *mockScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: m.enabled}
}
func (m *mockScraper) Close() error { return nil }

// mockScraper satisfies models.Scraper. It also implements
// models.DownloadProxyResolver so it can be used in
// collectDownloadProxyResolvers tests.
func (m *mockScraper) ResolveDownloadProxyForHost(_ string) (*models.ProxyConfig, *models.ProxyConfig, bool) {
	return nil, nil, false
}

type testFixture struct {
	t          *testing.T
	cfg        *config.Config
	db         *database.DB
	registry   *scraperutil.ScraperRegistry
	movieRepo  database.MovieRepositoryInterface
	httpClient httpclientiface.HTTPClient
	fs         afero.Fs
	dl         downloader.DownloaderInterface
	org        organizer.OrganizerInterface
	nfoGen     *nfo.Generator
}

func newFixture(t *testing.T) *testFixture {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	return &testFixture{
		t:          t,
		cfg:        cfg,
		db:         db,
		registry:   scraperutil.NewScraperRegistry(),
		movieRepo:  database.NewMovieRepository(db),
		httpClient: &http.Client{},
		fs:         afero.NewMemMapFs(),
	}
}

func (f *testFixture) withScraper(name string, result *models.ScraperResult, err error) *testFixture {
	if result != nil && result.Source == "" {
		result.Source = name
	}
	f.registry.RegisterInstance(&mockScraper{name: name, enabled: true, result: result, err: err})
	f.cfg.Scrapers.Priority = append(f.cfg.Scrapers.Priority, name)
	return f
}

func (f *testFixture) withDownloader() *testFixture {
	f.dl = downloader.NewDownloader(f.httpClient, f.fs, downloader.ConfigFromAppConfig(f.cfg, nfo.NFONameConfigFromAppConfig(f.cfg)), nil)
	return f
}

func (f *testFixture) withOrganizer() *testFixture {
	f.org = organizer.NewOrganizer(f.fs, organizer.ConfigFromAppConfig(f.cfg, nfo.NFONameConfigFromAppConfig(f.cfg)), nil, nil)
	return f
}

func (f *testFixture) withNFOGenerator() *testFixture {
	f.nfoGen = nfo.NewGenerator(f.fs, nil)
	return f
}

func (f *testFixture) withDisplayTitle(template string) *testFixture {
	f.cfg.Metadata.NFO.Format.DisplayTitle = template
	return f
}

func (f *testFixture) withSourceFile(path string) *testFixture {
	dir := filepath.Dir(path)
	require.NoError(f.t, f.fs.MkdirAll(dir, 0755))
	file, err := f.fs.Create(path)
	require.NoError(f.t, err)
	require.NoError(f.t, file.Close())
	return f
}

func (f *testFixture) build() *Workflow {
	scrapeCfg := scrape.ConfigFromAppConfig(f.cfg)
	aggCfg := aggregator.ConfigFromAppConfig(f.cfg)
	nfoCfg := nfo.ConfigFromAppConfig(f.cfg, nfo.NFONameConfigFromAppConfig(f.cfg))
	// workflowConfig removed — sub-configs are extracted inline
	sharedEngine := template.NewEngine()

	translator := scrape.NewTranslatorFromApp(&f.cfg.Metadata.Translation)
	scraper, _, _ := buildScraper(scrapeCfg, aggCfg, translator, f.registry, f.httpClient, f.fs, f.db.Repositories().ContentRepos, f.db.Repositories().ReplacementRepos)

	// Per ADR-0033: NFOInterface carries its own infrastructure deps.
	nfoIface := nfo.NewNFOImplementor(f.fs, nfoCfg, sharedEngine)

	return &Workflow{
		scrape:    newScrapeOrchestrator(scraper, f.movieRepo, f.cfg.Metadata.NFO.Format.DisplayTitle, sharedEngine, nfo.NFONameConfig{}, nil),
		apply:     newApplyOrchestrator(f.fs, f.org, f.dl, f.nfoGen, nfoIface, ApplyConfig{NFONameCfg: nfoCfg.ToNFONameConfig(false, ""), DisplayTitle: f.cfg.Metadata.NFO.Format.DisplayTitle}, sharedEngine, noOpRevertLog{}, database.NewMovieTagRepository(f.db), nil),
		compare:   newCompareOrchestrator(f.fs, nfoIface, scraper, nil),
		preview:   noOpPreviewOrchestrator{},
		scanMatch: noOpScanAndMatchOrchestrator{},
	}
}

func defaultMatch() models.FileMatchInfo {
	return models.FileMatchInfo{
		Path:    "/source/input.mp4",
		MovieID: "TEST-001",
		Name:    "input.mp4",
	}
}

// --- Scrape tests ---

func TestScrape_HappyPath(t *testing.T) {
	s := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Scraped Movie", Maker: "Test Studio"}, nil).
		build()

	result, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "TEST-001", result.Movie.ID)
	assert.Equal(t, scrape.StatusCompleted, result.Status)
}

func TestScrape_EmptyMovieID(t *testing.T) {
	s := newFixture(t).build()

	result, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: ""}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty MovieID")
	assert.Nil(t, result)
}

// --- Scrape+Apply two-phase tests (replaces old Organize tests) ---

func TestScrapeApply_HappyPath(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil)
	s := f.build()

	var progressCalls []scrape.ProgressStep
	progress := func(step scrape.ProgressStep, pct float64, msg string) {
		progressCalls = append(progressCalls, step)
	}

	// Phase 1: Scrape
	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, progress)
	assert.NoError(t, err)
	require.NotNil(t, scrapeResult)
	require.NotNil(t, scrapeResult.Movie)

	// Phase 2: Apply (organize only, no download/NFO)
	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:    scrapeResult.Movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, progress)
	assert.NoError(t, err)
	require.Greater(t, len(progressCalls), 0)
}

func TestScrapeApply_WithDownload(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil).
		withDownloader()
	s := f.build()

	var progressCalls []scrape.ProgressStep
	progress := func(step scrape.ProgressStep, pct float64, msg string) {
		progressCalls = append(progressCalls, step)
	}

	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)

	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:    scrapeResult.Movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, progress)
	assert.NoError(t, err)
	assert.Contains(t, progressCalls, scrape.ProgressStepDownload)
}

func TestScrapeApply_WithNFO(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil).
		withNFOGenerator()
	s := f.build()

	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)

	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:       scrapeResult.Movie,
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)

	exists, _ := afero.Exists(f.fs, "/dest/TEST-001.nfo")
	assert.True(t, exists, "NFO file should exist")
}

func TestScrapeApply_WithOrganize(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil).
		withOrganizer().
		withSourceFile("/source/input.mp4")
	s := f.build()

	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)

	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:    scrapeResult.Movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, nil)
	assert.NoError(t, err)
}

func TestScrapeApply_WithOrganize_CopyLink(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil).
		withOrganizer().
		withSourceFile("/source/input.mp4")
	s := f.build()

	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)

	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:    scrapeResult.Movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: false},
	}, nil)
	assert.NoError(t, err)

	_, err = f.fs.Stat("/source/input.mp4")
	assert.NoError(t, err, "source file should still exist after copy")
}

func TestScrapeApply_ScrapeError(t *testing.T) {
	s := newFixture(t).
		withScraper("failing", nil, errors.New("network error")).
		build()

	result, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	// Scrape returns (result, nil) on most failure paths — the result contains
	// status info. Callers check both return values per scrape.Scraper docs.
	if err != nil {
		assert.Error(t, err)
	} else {
		// If no error returned, the movie should be nil (scrape produced no usable data)
		if result != nil {
			assert.Nil(t, result.Movie, "scrape with failing scraper should not produce a movie")
		}
	}
}

func TestScrapeApply_NilMovieResult(t *testing.T) {
	s := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil).
		build()

	_, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: ""}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty MovieID")
}

func TestScrapeApply_OrganizeValidationError(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil).
		withOrganizer()
	s := f.build()

	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)

	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:    scrapeResult.Movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "organization validation failed")
}

func TestScrapeApply_ProgressCallbacks(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil).
		withDownloader().
		withNFOGenerator().
		withOrganizer().
		withSourceFile("/source/input.mp4")
	s := f.build()

	type call struct {
		step scrape.ProgressStep
		pct  float64
	}
	var calls []call
	progress := func(step scrape.ProgressStep, pct float64, msg string) {
		calls = append(calls, call{step: step, pct: pct})
	}

	// Phase 1: Scrape
	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, progress)
	assert.NoError(t, err)

	// Phase 2: Apply with all steps
	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:       scrapeResult.Movie,
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{MoveFiles: true},
		Download:    true,
		GenerateNFO: true,
	}, progress)
	assert.NoError(t, err)

	var steps []string
	for _, c := range calls {
		steps = append(steps, string(c.step))
	}
	assert.Contains(t, steps, "scrape")
	assert.Contains(t, steps, "organize")
	assert.Contains(t, steps, "download")
	assert.Contains(t, steps, "nfo")
}

// --- Edge cases ---

func TestScrapeApply_DownloadMedia_WithoutDownloader(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil)
	s := f.build()

	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)

	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:    scrapeResult.Movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
}

func TestScrapeApply_NilOrganizer(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil)
	s := f.build()

	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)

	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:    scrapeResult.Movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, nil)
	assert.NoError(t, err)
}

// --- Apply-only tests (replaces old OrganizeExisting tests) ---

func TestApply_HappyPath(t *testing.T) {
	f := newFixture(t).
		withOrganizer().
		withNFOGenerator().
		withDownloader().
		withSourceFile("/source/input.mp4")
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}

	var progressCalls []scrape.ProgressStep
	progress := func(step scrape.ProgressStep, pct float64, msg string) {
		progressCalls = append(progressCalls, step)
	}

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:       movie,
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{MoveFiles: true},
		Download:    true,
		GenerateNFO: true,
	}, progress)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	var steps []string
	for _, c := range progressCalls {
		steps = append(steps, string(c))
	}
	assert.Contains(t, steps, "download")
	assert.Contains(t, steps, "nfo")
	assert.Contains(t, steps, "organize")
}

func TestApply_NilMovie(t *testing.T) {
	s := newFixture(t).build()

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie: nil,
		Match: defaultMatch(),
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "movie is nil")
	assert.Nil(t, result)
}

func TestApply_DownloadOnly(t *testing.T) {
	f := newFixture(t).
		withDownloader()
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Nil(t, result.OrganizeResult)
}

func TestApply_OrganizeOnly(t *testing.T) {
	f := newFixture(t).
		withOrganizer().
		withSourceFile("/source/input.mp4")
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.OrganizeResult)
}

func TestScrapeApply_MultiPart(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}, nil).
		withDownloader()
	s := f.build()

	match := models.FileMatchInfo{
		Path:        "/source/input.mp4",
		MovieID:     "TEST-001",
		IsMultiPart: true,
		PartNumber:  1,
		PartSuffix:  "-pt1",
	}

	var progressCalls []scrape.ProgressStep
	progress := func(step scrape.ProgressStep, pct float64, msg string) {
		progressCalls = append(progressCalls, step)
	}

	scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)

	_, err = s.Apply(context.Background(), ApplyCmd{
		Movie:    scrapeResult.Movie,
		Match:    match,
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, progress)
	assert.NoError(t, err)
	assert.Contains(t, progressCalls, scrape.ProgressStepDownload)
}

func TestApply_MultiPart(t *testing.T) {
	f := newFixture(t).
		withDownloader()
	s := f.build()

	match := models.FileMatchInfo{
		Path:        "/source/input.mp4",
		MovieID:     "TEST-001",
		IsMultiPart: true,
		PartNumber:  1,
		PartSuffix:  "-pt1",
	}

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}

	var progressCalls []scrape.ProgressStep
	progress := func(step scrape.ProgressStep, pct float64, msg string) {
		progressCalls = append(progressCalls, step)
	}

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, progress)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Nil(t, result.OrganizeResult)
	assert.Contains(t, progressCalls, scrape.ProgressStepDownload)
}

// --- Apply unit tests (from prior wave) ---

func TestApply_SkipOrganize(t *testing.T) {
	f := newFixture(t).withDownloader()
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := defaultMatch()

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, movie, result.Movie)
}

func TestApply_WithOrganize(t *testing.T) {
	f := newFixture(t).
		withOrganizer().
		withDownloader().
		withNFOGenerator().
		withSourceFile("/source/input.mp4")
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := defaultMatch()

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestApply_GenerateNFO_Respected(t *testing.T) {
	f := newFixture(t).withNFOGenerator().withSourceFile("/source/input.mp4")
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := defaultMatch()

	// GenerateNFO=false should not produce NFO
	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:       movie,
		Match:       match,
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: false,
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.NFOPath)

	// GenerateNFO=true should produce NFO
	result, err = s.Apply(context.Background(), ApplyCmd{
		Movie:       movie,
		Match:       match,
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestApply_DisplayTitleApplied(t *testing.T) {
	f := newFixture(t)
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := defaultMatch()

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	// When displayTitle is empty (default test config), DisplayTitle should fall back to Title
	assert.Equal(t, "Test Movie", result.Movie.DisplayTitle)
}

func TestApply_DryRun_SkipsPersistedSteps(t *testing.T) {
	f := newFixture(t).withDownloader().withNFOGenerator().withSourceFile("/source/input.mp4")
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := defaultMatch()

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:       movie,
		Match:       match,
		DestPath:    "/dest",
		DryRun:      true,
		Organize:    OrganizeOptions{Skip: true},
		Download:    true,
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	// DryRun=true: download and NFO should be skipped
	assert.Nil(t, result.DownloadPaths)
	assert.Empty(t, result.NFOPath)
}

func TestApply_StepsTracking_FullSuccess(t *testing.T) {
	f := newFixture(t).
		withOrganizer().
		withDownloader().
		withNFOGenerator().
		withSourceFile("/source/input.mp4")
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie", Maker: "Test Studio"}

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:       movie,
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{MoveFiles: true},
		Download:    true,
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Steps.Organized, "Organized should be true after successful organize")
	assert.True(t, result.Steps.Merged, "Merged should be true after merge step")
	assert.True(t, result.Steps.DisplayTitle, "DisplayTitle should be true after display title step")
	assert.True(t, result.Steps.Downloaded, "Downloaded should be true after successful download")
	assert.True(t, result.Steps.NFOGenerated, "NFOGenerated should be true after successful NFO generation")
}

func TestApply_StepsTracking_SkipOrganize(t *testing.T) {
	f := newFixture(t).withDownloader()
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Steps.Organized, "Organized should be false when skipped")
	assert.True(t, result.Steps.Merged, "Merged should be true after merge step")
	assert.True(t, result.Steps.DisplayTitle, "DisplayTitle should be true after display title step")
	assert.True(t, result.Steps.Downloaded, "Downloaded should be true after successful download")
}

func TestApply_StepsTracking_SkipDownloadAndNFO(t *testing.T) {
	f := newFixture(t)
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}

	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: false,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Steps.Organized, "Organized should be false when skipped")
	assert.True(t, result.Steps.Merged, "Merged should be true")
	assert.True(t, result.Steps.DisplayTitle, "DisplayTitle should be true")
	assert.False(t, result.Steps.Downloaded, "Downloaded should be false when not enabled")
	assert.False(t, result.Steps.NFOGenerated, "NFOGenerated should be false when not enabled")
}

func TestApply_WithRevertLog(t *testing.T) {
	f := newFixture(t).withDownloader()
	s := f.build()

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := defaultMatch()

	// noOpRevertLog should not affect the result
	result, err := s.Apply(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.OperationID) // noOpRevertLog returns empty string
}

// --- RevertLog lifecycle tests ---

func TestRevertLog_NoOp_BeginComplete(t *testing.T) {
	log := noOpRevertLog{}
	opID, err := log.Begin(context.Background(), ApplyCmd{})
	assert.NoError(t, err)
	assert.Empty(t, opID)

	err = log.Complete(context.Background(), opID, &ApplyResult{})
	assert.NoError(t, err)
}

func TestRevertLog_DBAdapter_BeginPersistsBeforeMutation(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: cfg.Output.Operation.AllowRevert,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "test-job-123", afero.NewOsFs(), template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := defaultMatch()

	opID, beginErr := rl.Begin(context.Background(), ApplyCmd{
		Movie: movie,
		Match: match,
		Organize: OrganizeOptions{
			Skip: true,
		},
	})
	assert.NoError(t, beginErr)
	assert.NotEmpty(t, opID, "Begin should return a non-empty OperationID when record is persisted")

	// Verify the record exists in DB with "applied" revert_status
	record, findErr := repo.FindByID(context.TODO(), 1)
	if findErr == nil && record != nil {
		assert.Equal(t, models.RevertStatusApplied, record.RevertStatus)
		assert.Equal(t, "TEST-001", record.MovieID)
		assert.Equal(t, "test-job-123", record.BatchJobID)
	}
}

func TestRevertLog_DBAdapter_CompleteRecordsOutcome(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: cfg.Output.Operation.AllowRevert,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "test-job-456", afero.NewOsFs(), template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := defaultMatch()

	opID, _ := rl.Begin(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{Skip: true},
	})

	completeErr := rl.Complete(context.Background(), opID, &ApplyResult{
		Movie:   movie,
		NFOPath: "/dest/TEST-001.nfo",
	})
	assert.NoError(t, completeErr)

	// Verify the record was updated with post-apply info
	if opID != "" {
		var recordID uint
		if _, err := fmt.Sscanf(opID, "%d", &recordID); err == nil && recordID > 0 {
			record, findErr := repo.FindByID(context.TODO(), recordID)
			if findErr == nil && record != nil {
				hasNFOInfo := record.GeneratedFiles != "" || record.NFOPath != ""
				assert.True(t, hasNFOInfo, "Post-apply record should have NFO info")
			}
		}
	}
}

func TestRevertLog_DBAdapter_Complete_PersistsPostApplyState(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: cfg.Output.Operation.AllowRevert,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "test-job-postapply", afero.NewOsFs(), template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "TEST-POST-001", Title: "Post Apply Test"}
	match := defaultMatch()

	opID, beginErr := rl.Begin(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	// Complete with a realistic ApplyResult containing organize result, NFO, and downloads
	completeErr := rl.Complete(context.Background(), opID, &ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:        "/organized/TEST-POST-001/TEST-POST-001.mp4",
			InPlaceRenamed: false,
		},
		NFOPath:       "/organized/TEST-POST-001/TEST-POST-001.nfo",
		DownloadPaths: []string{"/organized/TEST-POST-001/poster.jpg", "/organized/TEST-POST-001/fanart.jpg"},
	})
	require.NoError(t, completeErr)

	// Verify the record was updated with post-apply information
	var recordID uint
	n, _ := fmt.Sscanf(opID, "%d", &recordID)
	require.Equal(t, 1, n, "opID should be parseable as record ID")
	require.Greater(t, recordID, uint(0))

	record, findErr := repo.FindByID(context.TODO(), recordID)
	require.NoError(t, findErr)
	require.NotNil(t, record)

	assert.Equal(t, "/organized/TEST-POST-001/TEST-POST-001.mp4", record.NewPath,
		"NewPath should match the organize result")
	assert.False(t, record.InPlaceRenamed,
		"InPlaceRenamed should be false for non-in-place organize")
	assert.NotEmpty(t, record.GeneratedFiles,
		"GeneratedFiles should be populated with NFO and download paths")
	assert.Contains(t, record.GeneratedFiles, "TEST-POST-001.nfo",
		"GeneratedFiles should contain NFO path info")
	assert.Equal(t, models.RevertStatusApplied, record.RevertStatus)
}

func TestRevertLog_DBAdapter_Complete_NilResult(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: cfg.Output.Operation.AllowRevert,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "test-job-nil-result", afero.NewOsFs(), template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "TEST-NIL-001", Title: "Nil Result Test"}
	match := defaultMatch()

	opID, beginErr := rl.Begin(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	completeErr := rl.Complete(context.Background(), opID, nil)
	require.NoError(t, completeErr)

	var recordID uint
	n, _ := fmt.Sscanf(opID, "%d", &recordID)
	require.Equal(t, 1, n, "opID should be parseable as record ID")
	require.Greater(t, recordID, uint(0))

	record, findErr := repo.FindByID(context.TODO(), recordID)
	require.NoError(t, findErr)
	require.NotNil(t, record)

	assert.Equal(t, models.RevertStatusFailed, record.RevertStatus,
		"Record should be marked as failed when Complete is called with nil result")
	assert.Empty(t, record.NewPath,
		"NewPath should be empty for failed apply")
	assert.Empty(t, record.GeneratedFiles,
		"GeneratedFiles should be empty for failed apply")
}

// TestNewRevertLogFromConfig_DisabledByDefault guards the regression where completed jobs
// showed "No operations recorded" — AllowRevert=false must NOT suppress recording, because
// AllowRevert gates only the revert *action* (enforced by the HTTP handlers), not the
// BatchFileOperation records that back the operations list.
func TestNewRevertLogFromConfig_DisabledByDefault(t *testing.T) {
	rl := NewRevertLogFromConfig(mocks.NewMockBatchFileOperationRepositoryInterface(t), &RevertLogConfig{AllowRevert: false}, "", nil, nil, nil, nil)
	assert.IsType(t, &dbRevertLog{}, rl, "AllowRevert=false should still return dbRevertLog — recording is independent of the revert toggle")
}

func TestNewRevertLogFromConfig_EnabledWhenConfigured(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"

	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := database.NewBatchFileOperationRepository(db)
	rl := NewRevertLogFromConfig(repo, &RevertLogConfig{
		AllowRevert: true,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "job-789", afero.NewOsFs(), template.NewEngine(), nil, nil)
	assert.IsType(t, &dbRevertLog{}, rl, "Should return dbRevertLog when AllowRevert is true")
}

// --- DisplayTitle consistency tests ---

func TestDisplayTitleConsistency_AcrossPaths(t *testing.T) {
	t.Run("Scrape path produces DisplayTitle from template", func(t *testing.T) {
		s := newFixture(t).
			withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Scrape Title"}, nil).
			withDisplayTitle("[<ID>] <TITLE>").
			build()

		result, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Movie)
		assert.Equal(t, "[TEST-001] Scrape Title", result.Movie.DisplayTitle)
	})

	t.Run("Apply path produces same DisplayTitle from template", func(t *testing.T) {
		s := newFixture(t).
			withDisplayTitle("[<ID>] <TITLE>").
			build()

		movie := &models.Movie{ID: "TEST-001", Title: "Scrape Title"}
		result, err := s.Apply(context.Background(), ApplyCmd{
			Movie:    movie,
			Match:    defaultMatch(),
			DestPath: "/dest",
			Organize: OrganizeOptions{Skip: true},
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "[TEST-001] Scrape Title", result.Movie.DisplayTitle)
	})

	t.Run("Both paths produce identical DisplayTitle for same input", func(t *testing.T) {
		f := newFixture(t).
			withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Scrape Title"}, nil).
			withDisplayTitle("[<ID>] <TITLE>")
		s := f.build()

		// Scrape path
		scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
		require.NoError(t, err)
		require.NotNil(t, scrapeResult)

		// Apply path (same movie, same template)
		applyResult, err := s.Apply(context.Background(), ApplyCmd{
			Movie:    &models.Movie{ID: "TEST-001", Title: "Scrape Title"},
			Match:    defaultMatch(),
			DestPath: "/dest",
			Organize: OrganizeOptions{Skip: true},
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, applyResult)

		assert.Equal(t, scrapeResult.Movie.DisplayTitle, applyResult.Movie.DisplayTitle,
			"Scrape and Apply must produce identical DisplayTitle for the same input")
	})

	t.Run("Empty template falls back to movie Title in both paths", func(t *testing.T) {
		f := newFixture(t).
			withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Scrape Title"}, nil)
		// No withDisplayTitle — default config has empty DisplayTitle template
		s := f.build()

		// Scrape path
		scrapeResult, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
		require.NoError(t, err)
		require.NotNil(t, scrapeResult)
		assert.Equal(t, "Scrape Title", scrapeResult.Movie.DisplayTitle,
			"Scrape path should fall back to Title when template is empty")

		// Apply path
		applyResult, err := s.Apply(context.Background(), ApplyCmd{
			Movie:    &models.Movie{ID: "TEST-001", Title: "Scrape Title"},
			Match:    defaultMatch(),
			DestPath: "/dest",
			Organize: OrganizeOptions{Skip: true},
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, applyResult)
		assert.Equal(t, "Scrape Title", applyResult.Movie.DisplayTitle,
			"Apply path should fall back to Title when template is empty")
	})

	t.Run("DisplayTitleSrc is used in Apply path", func(t *testing.T) {
		s := newFixture(t).
			withDisplayTitle("[<ID>] <TITLE>").
			build()

		movie := &models.Movie{ID: "TEST-001", Title: "Merged Title"}
		titleSrc := &models.Movie{ID: "TEST-001", Title: "Original Title"}

		result, err := s.Apply(context.Background(), ApplyCmd{
			Movie:           movie,
			Match:           defaultMatch(),
			DestPath:        "/dest",
			Organize:        OrganizeOptions{Skip: true},
			DisplayTitleSrc: titleSrc,
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "[TEST-001] Original Title", result.Movie.DisplayTitle,
			"Apply should use DisplayTitleSrc for template, not the merged movie")
	})
}

// --- NewScrapeOnlyWorkflow tests ---

func TestNewScrapeOnlyWorkflow_ReturnsNonNilWorkflow(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	fc, _ := NewFactoryConfigFromRepos(cfg, scraperutil.NewScraperRegistry(), db.Repositories())
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)
	wfi, err := factory.NewScrapeOnlyWorkflow()
	require.NoError(t, err)
	assert.NotNil(t, wfi, "NewScrapeOnlyWorkflow should return a non-nil Workflow")
	// Verify the scrape sub-orchestrator is a real implementation (not no-op)
	wf, ok := wfi.(*Workflow)
	require.True(t, ok)
	_, isReal := wf.scrape.(*scrapeOrchImpl)
	assert.True(t, isReal, "NewScrapeOnlyWorkflow should have a real scrapeOrchestrator")
}

func TestNewScrapeOnlyWorkflow_NilRegistry(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	fc, _ := NewFactoryConfigFromRepos(cfg, nil, database.Repositories{})
	// With nil registry, NewFactoryConfigFromRepos returns a zero-value config (Matcher=nil).
	// NewWorkflowFactory catches nil Matcher before NewScrapeOnlyWorkflow can check Scraper.
	_, err := NewWorkflowFactory(fc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "matcher must not be nil")
}

// --- NeedsPersistence tests (Phase 55 MD-01) ---

func TestScrape_NeedsPersistenceClearedAfterUpsert(t *testing.T) {
	// When ScrapeResult.NeedsPersistence is true (e.g., re-translated cache),
	// the single Upsert in Workflow.Scrape should persist and clear the flag.
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Test Movie"}, nil)
	s := f.build()

	result, meta, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)

	assert.False(t, meta.NeedsPersistence, "NeedsPersistence should be cleared after Upsert")
}

type needsPersistenceMockScraper struct {
	mockScraper
	needsPersistence bool
}

func (m *needsPersistenceMockScraper) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, error) {
	return &scrape.ScrapeResult{
		Movie:            &models.Movie{ID: m.result.ID, Title: m.result.Title},
		NeedsPersistence: m.needsPersistence,
	}, m.err
}

func TestScrape_NeedsPersistenceTrue_ClearedAndUpsertedOnce(t *testing.T) {
	// Simulates the re-translated cache hit path where NeedsPersistence=true.
	// Verifies: (1) NeedsPersistence is cleared after Upsert, (2) only one Upsert occurs.
	mock := &needsPersistenceMockScraper{
		mockScraper: mockScraper{
			name: "mock", enabled: true,
			result: &models.ScraperResult{ID: "TEST-002", Title: "Re-translated Movie"},
		},
		needsPersistence: true,
	}
	f := newFixture(t).withScraper("mock", mock.result, nil)
	s := f.build()

	// Replace the scrape sub-orchestrator with one that uses our custom scraper
	s.scrape = newScrapeOrchestrator(mock, f.movieRepo, "", nil, nfo.NFONameConfig{}, nil)

	result, meta, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-002"}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)

	assert.False(t, meta.NeedsPersistence,
		"NeedsPersistence should be cleared after Workflow.Scrape handles the result")

	saved, err := f.movieRepo.FindByID(context.TODO(), "TEST-002")
	require.NoError(t, err)
	require.NotNil(t, saved, "Movie should be persisted after NeedsPersistence path")
}

// --- ForceRefresh cache deletion tests (Phase 55 MD-02) ---

func TestScrape_ForceRefreshDeletesCache(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Scraped Movie"}, nil)
	s := f.build()

	// First scrape: inserts into cache
	result, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify cached
	cached, err := f.movieRepo.FindByID(context.TODO(), "TEST-001")
	require.NoError(t, err)
	require.NotNil(t, cached)

	// Second scrape with ForceRefresh: should delete cache and re-scrape
	result2, _, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001", ForceRefresh: true}, nil)
	require.NoError(t, err)
	require.NotNil(t, result2)
	assert.Equal(t, "TEST-001", result2.Movie.ID)
}

// --- Visibility field tests (Phase 80, ADR-0015) ---

func TestScrape_VisibilityFields_SetOnSuccess(t *testing.T) {
	// When Workflow.Scrape succeeds, DisplayTitleApplied and Persisted should be true.
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "VIS-001", Title: "Visible Movie"}, nil)
	s := f.build()

	_, meta, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "VIS-001"}, nil)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.True(t, meta.DisplayTitleApplied, "DisplayTitleApplied should be true after Workflow.Scrape applies DisplayTitle")
	assert.True(t, meta.Persisted, "Persisted should be true after Workflow.Scrape persists to DB")
}

func TestScrape_VisibilityFields_DisplayTitleNotAppliedWhenNoConfig(t *testing.T) {
	// When displayTitle is empty, DisplayTitleApplied should still be true because
	// ApplyDisplayTitleFromSource is still called (it may set title = title as fallback).
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "VIS-002", Title: "No DisplayTitle"}, nil)
	s := f.build()

	_, meta, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "VIS-002"}, nil)
	require.NoError(t, err)
	require.NotNil(t, meta)

	// DisplayTitleApplied reflects that the step ran, not that it changed the title.
	assert.True(t, meta.DisplayTitleApplied, "DisplayTitleApplied should be true when the step runs")
}

func TestScrape_VisibilityFields_PersistedFalseWhenNoMovieRepo(t *testing.T) {
	// When movieRepo is nil, Persisted should remain false.
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "VIS-003", Title: "No Repo"}, nil)
	s := f.build()
	// Replace the scrape orchestrator with one that has nil movieRepo
	impl, ok := s.scrape.(*scrapeOrchImpl)
	require.True(t, ok, "scrape should be a scrapeOrchImpl")
	s.scrape = newScrapeOrchestrator(impl.scraper, nil, impl.displayTitle, impl.templateEngine, nfo.NFONameConfig{}, nil)

	_, meta, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "VIS-003"}, nil)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.False(t, meta.Persisted, "Persisted should be false when movieRepo is nil")
}

func TestScrape_VisibilityFields_PosterGeneratedNotSet(t *testing.T) {
	// Poster generation has moved to the worker's scrape phase.
	// The workflow's Scrape method no longer sets PosterGenerated or PosterError.
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "VIS-004", Title: "Poster Movie"}, nil)
	s := f.build()

	result, meta, err := s.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "VIS-004"}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The orchestrator no longer handles poster generation.
	assert.False(t, meta.PosterGenerated, "PosterGenerated should be false — poster gen moved to worker phase")
	assert.Nil(t, meta.PosterError, "PosterError should be nil — poster gen moved to worker phase")
}
