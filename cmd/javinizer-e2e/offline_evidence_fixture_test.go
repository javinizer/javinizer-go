package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/batch"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

func newOfflineEvidenceFixture(t *testing.T) (*config.Config, models.R18DevDumpLookup, *scraperutil.ScraperRegistry, models.Scraper, func()) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native artifact publication deliberately unsupported on this platform")
	}
	cfg := config.DefaultConfig(nil, nil)
	reg := scraperutil.NewScraperRegistry()
	lookup, cleanup, err := prepareOfflineEvidenceFixture(t.Context(), t.TempDir(), cfg, reg)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NoError(t, cfg.Scrapers.Finalize(reg))
	_, err = scraper.NewDefaultScraperRegistryFrom(reg, scraper.ScraperRegistryConfigFromApp(cfg, reg.Names(), reg.GetAllDefaults()), nil, lookup)
	require.NoError(t, err)
	s, ok := reg.GetInstance("r18dev")
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return cfg, lookup, reg, s, cleanup
}

func TestOfflineEvidenceFixtureUsesGenuineDumpAndPublishedArtifact(t *testing.T) {
	cfg, lookup, _, s, cleanup := newOfflineEvidenceFixture(t)
	require.Equal(t, []string{"r18dev"}, cfg.Scrapers.Priority)
	require.True(t, cfg.Metadata.OfflineScrapeEvidence.Enabled)
	require.False(t, cfg.Metadata.ActressDatabase.OfflineNameSync.Enabled)
	require.False(t, cfg.System.VersionCheckEnabled)
	for _, enabled := range []bool{cfg.Output.Download.DownloadActress, cfg.Output.Download.DownloadCover, cfg.Output.Download.DownloadPoster, cfg.Output.Download.DownloadExtrafanart, cfg.Output.Download.DownloadTrailer} {
		require.False(t, enabled)
	}
	dump, err := lookup.LookupMovie(t.Context(), offlineEvidenceMovieID)
	require.NoError(t, err)
	require.Equal(t, "7", dump.Actresses[0].ID)
	require.Equal(t, "007", dump.Actresses[1].ID)
	require.Equal(t, "Original dump actor", dump.Actresses[0].NameKanji)
	for _, id := range []string{offlineEvidenceMovieID, offlineEvidenceContentID} {
		raw, err := s.Search(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, []string{"7", "007"}, raw.R18DumpActorReferences())
		require.Equal(t, offlineEvidenceMovieID, raw.ID)
		require.Equal(t, offlineEvidenceContentID, raw.ContentID)
		require.Equal(t, "Original dump actor", raw.Actresses[0].JapaneseName)
		require.Equal(t, "Original leading-zero actor", raw.Actresses[1].JapaneseName)
		for _, actor := range raw.Actresses {
			require.Equal(t, 7, actor.DMMID, "legacy integer projection is not source authority")
			require.Empty(t, actor.ThumbURL)
		}
		require.Empty(t, raw.PosterURL)
		require.Empty(t, raw.CoverURL)
		require.Empty(t, raw.ScreenshotURL)
		require.Empty(t, raw.TrailerURL)
		service, err := scrapeevidence.New(scrapeevidence.Options{Enabled: true, ArtifactDirectory: cfg.Metadata.OfflineScrapeEvidence.ArtifactDirectory, MaxAge: 168 * time.Hour})
		require.NoError(t, err)
		off, releaseOff := service.Begin(t.Context(), false)
		require.Nil(t, scrapeevidence.Resolve(off, []*models.ScraperResult{raw}, false))
		releaseOff()
		ctx, release := service.Begin(t.Context(), true)
		before := raw.Clone()
		report := scrapeevidence.Resolve(ctx, []*models.ScraperResult{raw}, false)
		release()
		require.Equal(t, scrapeevidence.Available, report.Outcome)
		require.Len(t, report.SnapshotDigest, 64)
		require.Len(t, report.Observations, 2)
		require.Equal(t, "7", report.Observations[0].SourceID)
		require.Equal(t, "Supplemental evidence name", report.Observations[0].JapaneseName)
		require.Equal(t, []string{"Supplemental whole alias"}, report.Observations[0].Aliases)
		require.Equal(t, "007", report.Observations[1].SourceID)
		require.Equal(t, "Supplemental leading-zero name", report.Observations[1].JapaneseName)
		require.Equal(t, before, raw)
		encoded, err := json.Marshal(raw)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "Supplemental")
		var decoded models.ScraperResult
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		require.Nil(t, decoded.R18DumpActorReferences())
	}
	artifactDirectory := cfg.Metadata.OfflineScrapeEvidence.ArtifactDirectory
	cleanup()
	cleanup()
	_, err = os.Stat(filepath.Dir(artifactDirectory))
	require.True(t, os.IsNotExist(err))
	_, err = lookup.LookupMovie(t.Context(), offlineEvidenceMovieID)
	require.Error(t, err)
	_, err = s.Search(t.Context(), offlineEvidenceMovieID)
	require.ErrorContains(t, err, "fixture is closed")
}

func TestOfflineEvidenceFixtureRejectsLiveFallback(t *testing.T) {
	_, lookup, _, s, _ := newOfflineEvidenceFixture(t)
	for _, id := range []string{"FAIL-001", "EVIDENCE-999", "evidence-001", "7", "007", "https://r18.dev/videos/vod/movies/detail/-/combined=evidence00001/json"} {
		result, err := s.Search(t.Context(), id)
		require.ErrorContains(t, err, "rejects unsupported query")
		require.Nil(t, result)
	}
	_, err := s.GetURL(t.Context(), offlineEvidenceMovieID)
	require.ErrorContains(t, err, "disables URL resolution")
	_, handlesURL := s.(models.URLHandler)
	require.False(t, handlesURL)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.Search(ctx, offlineEvidenceMovieID)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, lookup.(*r18devdump.Store).Close())
	result, err := s.Search(t.Context(), offlineEvidenceMovieID)
	require.Nil(t, result)
	require.ErrorIs(t, err, context.Canceled, "SQLite error cannot become live HTTP fallback")
}

func TestOfflineEvidenceFixtureConcurrentSearchAndCleanup(t *testing.T) {
	_, _, _, s, cleanup := newOfflineEvidenceFixture(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			result, err := s.Search(t.Context(), offlineEvidenceMovieID)
			if err != nil {
				require.ErrorContains(t, err, "fixture is closed")
				return
			}
			require.Equal(t, []string{"7", "007"}, result.R18DumpActorReferences())
		})
	}
	wg.Go(cleanup)
	wg.Wait()
}

func TestOfflineEvidenceFixtureSetupFailureDoesNotEnableCapability(t *testing.T) {
	for _, scenario := range []string{"nil-context", "nil-config", "nil-registry", "relative-path", "absent-parent", "cancelled", "platform-publication"} {
		t.Run(scenario, func(t *testing.T) {
			if scenario == "platform-publication" && (runtime.GOOS == "linux" || runtime.GOOS == "darwin") {
				t.Skip("publisher supported on this platform")
			}
			directory := t.TempDir()
			cfg := config.DefaultConfig(nil, nil)
			reg := scraperutil.NewScraperRegistry()
			ctx := t.Context()
			switch scenario {
			case "nil-context":
				ctx = nil
			case "nil-config":
				cfg = nil
			case "nil-registry":
				reg = nil
			case "relative-path":
				directory = "relative"
			case "absent-parent":
				directory = filepath.Join(directory, "missing")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			lookup, cleanup, err := prepareOfflineEvidenceFixture(ctx, directory, cfg, reg)
			require.Error(t, err)
			require.Nil(t, lookup)
			require.Nil(t, cleanup)
			if cfg != nil {
				require.False(t, cfg.Metadata.OfflineScrapeEvidence.Enabled)
			}
			if reg != nil {
				require.Empty(t, reg.Names())
			}
			if scenario != "relative-path" && scenario != "absent-parent" {
				entries, err := os.ReadDir(directory)
				require.NoError(t, err)
				require.Empty(t, entries)
			}
		})
	}
}

func TestOfflineEvidenceFixtureGenuineSourceThroughBatchAPI(t *testing.T) {
	cfg, _, registry, _, _ := newOfflineEvidenceFixture(t)
	root := t.TempDir()
	inputDir, outputDir := filepath.Join(root, "input"), filepath.Join(root, "output")
	require.NoError(t, os.Mkdir(inputDir, 0o700))
	require.NoError(t, os.Mkdir(outputDir, 0o700))
	file := filepath.Join(inputDir, offlineEvidenceMovieID+".mp4")
	require.NoError(t, os.WriteFile(file, []byte("synthetic test video"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, offlineEvidenceMovieID+".nfo"), []byte("existing NFO must not change during scrape review"), 0o600))
	cfg.API.Security.AllowedDirectories = []string{root}
	cfg.System.TempDir = t.TempDir()
	cfg.Metadata.Priority.Priority = []string{"r18dev"}
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = `(?i)([a-z]{2,10}-?\d{2,5}[a-z]?)`
	cfg.Performance.MaxWorkers = 2
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "application.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	writes := &fixtureSQLWrites{Interface: logger.Discard}
	db.Config.Logger = writes
	deps, rt, err := core.BootstrapAPIWithOpts(cfg, "", nil, &commandutil.DependenciesOptions{DB: db, ScraperRegistry: registry})
	require.NoError(t, err)
	t.Cleanup(rt.Shutdown)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	batch.RegisterRoutes(router.Group("/api/v1"), rt)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(string(encoded)))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	response := request(http.MethodPost, "/batch/scrape", contracts.BatchScrapeRequest{Files: []string{file}, Force: true, SelectedScrapers: []string{"r18dev"}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var started contracts.BatchScrapeResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &started))
	require.True(t, rt.WaitBackgroundTasks(10*time.Second))
	job, ok := deps.GetJobStore().GetBatchJob(started.JobID)
	require.True(t, ok)
	status := job.GetStatus()
	require.Equal(t, models.JobStatusCompleted, status.Status)
	require.Len(t, status.Results, 1)
	initial := status.Results[file]
	require.NotNil(t, initial.Movie)
	require.Nil(t, status.Provenance[file].OfflineEvidence)
	path := "/batch/" + started.JobID + "/results/" + initial.ResultID
	getSources := func() contracts.SourceResultsResponse {
		t.Helper()
		response := request(http.MethodGet, path+"/sources", nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var sources contracts.SourceResultsResponse
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &sources))
		return sources
	}
	before := getSources()
	require.Nil(t, before.OfflineEvidence)
	require.Len(t, before.Results, 1)
	require.Equal(t, "Original dump actor", before.Results[0].Actresses[0].JapaneseName)
	actorNames := func() []map[string]any {
		t.Helper()
		var rows []map[string]any
		require.NoError(t, db.WithContext(t.Context()).Model(&models.Actress{}).Select("id, japanese_name, first_name, last_name").Order("id").Find(&rows).Error)
		return rows
	}
	beforeNames := actorNames()
	require.NotEmpty(t, beforeNames)
	// The first rescrape settles existing render/filename bookkeeping. Compare
	// subsequent identical refreshes so one-time legacy maintenance is not
	// attributed to either evidence opt-in state.
	response = request(http.MethodPost, path+"/rescrape", contracts.BatchRescrapeRequest{Force: true, SelectedScrapers: []string{"r18dev"}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	// Compare identical ordinary force-rescrapes, off then on. Existing movie
	// upserts remain normal behavior; the evidence path adds no canonical SQL.
	beforeOrdinaryRevision := fixtureQuarantineRevision(t, db)
	writes.reset()
	response = request(http.MethodPost, path+"/rescrape", contracts.BatchRescrapeRequest{Force: true, SelectedScrapers: []string{"r18dev"}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	ordinaryRevision := fixtureQuarantineRevision(t, db)
	ordinaryWrites := writes.snapshot()
	require.NotEmpty(t, ordinaryWrites)
	baselineDB := fixtureCanonicalRows(t, db)
	baselineFiles := fixtureFiles(t, inputDir, outputDir, cfg.System.TempDir)
	baselineNFO := fixtureNFO(t, job.GetStatus().Results[file].Movie)
	preview := func() string {
		t.Helper()
		response := request(http.MethodPost, path+"/preview", contracts.OrganizePreviewRequest{Destination: outputDir})
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		return response.Body.String()
	}
	baselinePreview := preview()
	writes.reset()
	response = request(http.MethodPost, path+"/rescrape", contracts.BatchRescrapeRequest{Force: true, SelectedScrapers: []string{"r18dev"}, ShowOfflineEvidence: true})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, ordinaryWrites, writes.snapshot(), "opt-in adds no actor/alias/credit/translation/sync/movie SQL writes")
	evidenceRevision := fixtureQuarantineRevision(t, db)
	require.Equal(t, ordinaryRevision-beforeOrdinaryRevision, evidenceRevision-ordinaryRevision, "ordinary quarantine revision maintenance is unchanged")
	require.Equal(t, baselineDB, fixtureCanonicalRows(t, db))
	require.Equal(t, baselineNFO, fixtureNFO(t, job.GetStatus().Results[file].Movie))
	require.JSONEq(t, baselinePreview, preview(), "folder, file and media/NFO path precedence is unchanged")
	require.Equal(t, baselineFiles, fixtureFiles(t, inputDir, outputDir, cfg.System.TempDir), "read-only review creates no media/NFO/folders")
	var rescraped contracts.BatchRescrapeResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &rescraped))
	initialView := contracts.MovieViewFromModel(initial.Movie)
	require.Equal(t, initialView.Actresses, rescraped.Movie.Actresses)
	require.Equal(t, initialView.Credits, rescraped.Movie.Credits)
	require.Equal(t, scrapeevidence.Available, rescraped.OfflineEvidence.Outcome)
	sources := getSources()
	require.Equal(t, rescraped.OfflineEvidence, sources.OfflineEvidence)
	require.Equal(t, []string{"7", "007"}, []string{sources.OfflineEvidence.Observations[0].SourceID, sources.OfflineEvidence.Observations[1].SourceID})
	require.Equal(t, "Supplemental evidence name", sources.OfflineEvidence.Observations[0].JapaneseName)
	require.Equal(t, "Supplemental leading-zero name", sources.OfflineEvidence.Observations[1].JapaneseName)
	require.Equal(t, before.Results, sources.Results)
	require.Nil(t, sources.Results[0].R18DumpActorReferences())
	require.Equal(t, beforeNames, actorNames())
	require.Equal(t, baselineDB, fixtureCanonicalRows(t, db), "failed rescrape cannot modify canonical state")
	movieJSON, err := json.Marshal(rescraped.Movie)
	require.NoError(t, err)
	require.Contains(t, string(movieJSON), "Original dump actor")
	require.NotContains(t, string(movieJSON), "Supplemental")
	require.NotContains(t, string(movieJSON), "offline_evidence")
	response = request(http.MethodPost, path+"/rescrape", contracts.BatchRescrapeRequest{Force: true, SelectedScrapers: []string{"r18dev"}, ManualSearchInput: "EVIDENCE-999", ShowOfflineEvidence: true})
	require.Equal(t, http.StatusUnprocessableEntity, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "rejects unsupported query")
	require.Equal(t, sources, getSources(), "failed rescrape preserves committed source and report")
	require.Equal(t, evidenceRevision, fixtureQuarantineRevision(t, db))
	require.Equal(t, beforeNames, actorNames())
	require.Equal(t, baselineDB, fixtureCanonicalRows(t, db))
	require.Equal(t, baselineFiles, fixtureFiles(t, inputDir, outputDir, cfg.System.TempDir))
	response = request(http.MethodPost, path+"/rescrape", contracts.BatchRescrapeRequest{Force: true, SelectedScrapers: []string{"r18dev"}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "offline_evidence")
	after := getSources()
	require.Nil(t, after.OfflineEvidence)
	require.Equal(t, before.Results, after.Results)
	require.Equal(t, beforeNames, actorNames())
	response = request(http.MethodPost, "/batch/scrape", contracts.BatchScrapeRequest{Files: []string{file}, Force: true, SelectedScrapers: []string{"r18dev"}, ShowOfflineEvidence: true})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &started))
	require.True(t, rt.WaitBackgroundTasks(10*time.Second))
	job, ok = deps.GetJobStore().GetBatchJob(started.JobID)
	require.True(t, ok)
	status = job.GetStatus()
	require.Equal(t, models.JobStatusCompleted, status.Status)
	require.Equal(t, scrapeevidence.Available, status.Provenance[file].OfflineEvidence.Outcome)
	require.Equal(t, sources.OfflineEvidence.SnapshotDigest, status.Provenance[file].OfflineEvidence.SnapshotDigest)
	require.Equal(t, beforeNames, actorNames())
}

func fixtureCanonicalTable(name string) bool {
	return name == "movies" || name == "actresses" || strings.HasPrefix(name, "actress_") || strings.HasPrefix(name, "movie_") || strings.HasPrefix(name, "credit_")
}

func fixtureCanonicalRows(t *testing.T, db *database.DB) map[string][]string {
	t.Helper()
	var tables []string
	require.NoError(t, db.WithContext(t.Context()).Raw("SELECT name FROM sqlite_master WHERE type = 'table'").Scan(&tables).Error)
	result := make(map[string][]string)
	for _, table := range tables {
		if !fixtureCanonicalTable(table) {
			continue
		}
		var rows []map[string]any
		require.NoError(t, db.WithContext(t.Context()).Table(table).Find(&rows).Error)
		result[table] = []string{}
		for _, row := range rows {
			// Ordinary forced upserts refresh this timestamp with or without
			// evidence. Everything else, including ownership/row IDs, is exact.
			delete(row, "updated_at")
			if table == "actress_quarantine_state" {
				// Monotonic maintenance counters are checked by equal off/on
				// deltas above, not mistaken for actor metadata changes.
				delete(row, "revision")
				delete(row, "consumed_revision")
			}
			encoded, err := json.Marshal(row)
			require.NoError(t, err)
			result[table] = append(result[table], string(encoded))
		}
		sort.Strings(result[table])
	}
	for _, required := range []string{"movies", "actresses", "actress_aliases", "movie_credits", "movie_translations", "actress_translations", "actress_sync_jobs", "actress_sync_tasks", "actress_sync_bindings"} {
		require.Contains(t, result, required)
	}
	return result
}

var fixtureMutation = regexp.MustCompile("(?i)^\\s*(INSERT(?: OR \\w+)? INTO|UPDATE|DELETE FROM)\\s+[`\"]?(\\w+)")

type fixtureSQLWrites struct {
	logger.Interface
	mu     sync.Mutex
	counts map[string]int
}

func (w *fixtureSQLWrites) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	match := fixtureMutation.FindStringSubmatch(sql)
	if len(match) != 3 || !fixtureCanonicalTable(match[2]) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.counts != nil {
		w.counts[strings.ToUpper(match[1])+" "+match[2]]++
	}
}

func (w *fixtureSQLWrites) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.counts = make(map[string]int)
}

func (w *fixtureSQLWrites) snapshot() map[string]int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]int, len(w.counts))
	for key, count := range w.counts {
		out[key] = count
	}
	return out
}

func fixtureFiles(t *testing.T, directories ...string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, directory := range directories {
		require.NoError(t, filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				result[path] = "directory"
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] = fmt.Sprintf("%x", sha256.Sum256(data))
			return nil
		}))
	}
	return result
}

func fixtureNFO(t *testing.T, movie *models.Movie) string {
	t.Helper()
	fs := afero.NewMemMapFs()
	generator := nfo.NewGenerator(fs, &nfo.Config{ActressLanguageJA: true, UseCreditedName: true, IncludeActressImages: true})
	path := filepath.Join(string(filepath.Separator), "synthetic.nfo")
	require.NoError(t, generator.GenerateAtPath(t.Context(), movie, path, "", nil))
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	require.NotContains(t, string(data), "Supplemental")
	return string(data)
}

func fixtureQuarantineRevision(t *testing.T, db *database.DB) int64 {
	t.Helper()
	var state struct {
		Revision         int64
		ConsumedRevision int64
	}
	require.NoError(t, db.WithContext(t.Context()).Table("actress_quarantine_state").Take(&state).Error)
	require.Equal(t, state.Revision, state.ConsumedRevision)
	return state.Revision
}
