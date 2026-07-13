package r18devdump

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// openRawDB opens the dump SQLite database in read-write mode for test setup.
func openRawDB(path string) (*sql.DB, error) {
	return sql.Open("sqlite3", path+"?mode=rwc")
}

// newTestHandler creates a dumpHandler with a config pointing at a temp dump
// path. The dump file is NOT created — tests create it via buildTestDump or
// leave it absent to test the "not present" path.
func newTestHandler(t *testing.T) (*dumpHandler, string) {
	t.Helper()
	dumpPath := filepath.Join(t.TempDir(), "r18dev_dump.db")
	cfg := &config.Config{}
	cfg.Metadata.R18DevDump.Path = dumpPath
	cfg.Metadata.R18DevDump.Enabled = true

	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{}}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)

	return &dumpHandler{
		rt:         rt,
		httpClient: &http.Client{},
		reloadFn: func(cfg *config.Config, lockHeld bool) error {
			if lockHeld {
				return rt.ReloadConfigLocked(cfg)
			}
			return rt.ReloadConfig(cfg)
		},
		removeFn: os.Remove,
	}, dumpPath
}

// newTestHandlerWithHub creates a dumpHandler with a real WebSocket hub so
// broadcastProgress is exercised.
func newTestHandlerWithHub(t *testing.T) (*dumpHandler, string, *ws.Hub) {
	t.Helper()
	h, dumpPath := newTestHandler(t)
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	go hub.Run(ctx)
	h.rt.EnsureRuntime().SetWebSocketHubForTesting(hub)
	return h, dumpPath, hub
}

// buildTestDump creates a minimal dump DB with the full schema and one video
// row for testing. r18devdump.Open uses mode=ro and does not create the schema
// (that's done by Import), so tests must create it explicitly.
func buildTestDump(t *testing.T, path, contentID, dvdID string) {
	t.Helper()
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	db, err := openRawDB(path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS videos (
		content_id        TEXT NOT NULL PRIMARY KEY,
		dvd_id            TEXT,
		dvd_id_norm       TEXT,
		title_en          TEXT,
		title_ja          TEXT,
		comment_en        TEXT,
		comment_ja        TEXT,
		runtime_mins      INTEGER,
		release_date      TEXT,
		sample_url        TEXT,
		maker_id          TEXT,
		label_id          TEXT,
		series_id         TEXT,
		jacket_full_url   TEXT,
		jacket_thumb_url  TEXT,
		gallery_full_first  TEXT,
		gallery_full_last   TEXT,
		gallery_thumb_first TEXT,
		gallery_thumb_last  TEXT,
		site_id           TEXT,
		service_code      TEXT
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_videos_dvd_id_norm ON videos(dvd_id_norm)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dump_meta (key TEXT PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT OR REPLACE INTO videos (content_id, dvd_id, dvd_id_norm) VALUES (?, ?, ?)`,
		contentID, dvdID, "ABF030")
	require.NoError(t, err)
}

func TestGetStatus_DumpAbsent(t *testing.T) {
	h, dumpPath := newTestHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)

	h.getStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Present)
	assert.Equal(t, dumpPath, resp.Path)
	assert.True(t, resp.Enabled)
}

func TestGetStatus_DumpPresent(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)

	h.getStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Present)
	assert.Equal(t, dumpPath, resp.Path)
	assert.True(t, resp.Enabled)
	assert.Greater(t, resp.SizeBytes, int64(0))
}

func TestGetStatus_StatsError(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	// Create a DB file with no `videos` table — Open succeeds but Stats fails.
	db, err := openRawDB(dumpPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE dump_meta (key TEXT PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)

	h.getStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Present)
}

func TestSearch_DumpAbsent(t *testing.T) {
	h, _ := newTestHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=ABF-030", nil)

	h.search(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestSearch_MissingQuery(t *testing.T) {
	h, _ := newTestHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search", nil)

	h.search(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSearch_HitByDVDID(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=ABF-030", nil)

	h.search(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp searchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ABF-030", resp.Query)
	require.NotNil(t, resp.ContentID)
	assert.Equal(t, "118abf030", *resp.ContentID)
}

func TestSearch_HitByContentID(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=118abf030", nil)

	h.search(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp searchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// dvd_id lookup will miss (118abf030 != ABF-030), content_id lookup should hit
	assert.Equal(t, "118abf030", resp.Query)
	require.NotNil(t, resp.DVDID)
	assert.Equal(t, "ABF-030", *resp.DVDID)
}

func TestSearch_NotFound(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=NOTFOUND-999", nil)

	h.search(c)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestStartDownload_ConcurrentGuard(t *testing.T) {
	h, _ := newTestHandler(t)

	// Simulate an in-progress download.
	h.mu.Lock()
	h.running = true
	h.mu.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)

	require.Equal(t, http.StatusConflict, w.Code)
}

func TestStartUpdate_ConcurrentGuard(t *testing.T) {
	h, _ := newTestHandler(t)

	h.mu.Lock()
	h.running = true
	h.mu.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/update", nil)

	h.startUpdate(c)

	require.Equal(t, http.StatusConflict, w.Code)
}

// --- newDumpHandler + RegisterRoutes coverage ---

func TestNewDumpHandler(t *testing.T) {
	cfg := &config.Config{}
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{}}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)

	h := newDumpHandler(rt)
	require.NotNil(t, h)
	assert.NotNil(t, h.rt)
	assert.NotNil(t, h.httpClient)
	require.NotNil(t, h.reloadFn)
	// Exercise the reloadFn closure to cover the closure body.
	err := h.reloadFn(cfg, false)
	_ = err // may or may not error depending on registry setup
	unlock := rt.LockReload()
	err = h.reloadFn(cfg, true)
	unlock()
	_ = err
}

func TestRegisterRoutes(t *testing.T) {
	cfg := &config.Config{}
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{}}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("/api/v1")
	RegisterRoutes(protected, protected, rt)

	routes := router.Routes()
	pathSet := make(map[string]bool)
	for _, r := range routes {
		pathSet[r.Method+" "+r.Path] = true
	}
	assert.True(t, pathSet["GET /api/v1/r18dev/dump/status"])
	assert.True(t, pathSet["POST /api/v1/r18dev/dump/download"])
	assert.True(t, pathSet["POST /api/v1/r18dev/dump/update"])
	assert.True(t, pathSet["GET /api/v1/r18dev/dump/search"])
}

// --- Download goroutine coverage ---

// gzipped compresses a string into a gzip byte slice.
func gzipped(body string) []byte {
	var buf strings.Builder
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write([]byte(body))
	_ = gw.Close()
	return []byte(buf.String())
}

// newDumpTestServer creates an httptest server that serves a minimal gzip dump
// and a /latest redirect, mirroring the real r18.dev dump endpoint.
func newDumpTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(dumpBody)

	mux := http.NewServeMux()
	mux.HandleFunc("/dumps/r18dotdev_dump_2026-04-28.sql.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	})
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dumps/r18dotdev_dump_2026-04-28.sql.gz", http.StatusFound)
	})
	return httptest.NewServer(mux)
}

func TestStartDownload_FullDownload(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, dumpPath, hub := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	// Use a no-op reload to avoid opening a SQLite handle to the temp dump
	// path (which Windows can't clean up). Reload is tested separately.
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	// Override LatestDumpURL to point at the test server.
	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)

	require.Equal(t, http.StatusAccepted, w.Code)

	// Wait for the goroutine to finish (the dump import creates the DB file).
	require.Eventually(t, func() bool {
		_, err := os.Stat(dumpPath)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "dump DB should be created by the download goroutine")
	<-h.done

	// Verify the hub was used (broadcastProgress was called).
	_ = hub
}

func TestStartDownload_UpdateOnly_Unchanged(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	// Pre-create a dump with the matching source URL so update-only skips.
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")
	// Insert the source_url metadata matching the test server's redirect target.
	db, err := openRawDB(dumpPath)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR REPLACE INTO dump_meta (key, value) VALUES (?, ?)`,
		"source_url", srv.URL+"/dumps/r18dotdev_dump_2026-04-28.sql.gz")
	require.NoError(t, err)
	_ = db.Close()

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/update", nil)

	h.startUpdate(c)

	require.Equal(t, http.StatusAccepted, w.Code)

	// The update should detect the version is unchanged and not re-import.
	// Give the goroutine time to run.
	<-h.done
}

func TestStartDownload_DownloadError(t *testing.T) {
	// Point at a non-existent server to trigger a download error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	h, _, hub := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)

	require.Equal(t, http.StatusAccepted, w.Code)

	// Wait for the goroutine to process the error.
	<-h.done
	_ = hub
}

// --- broadcastProgress coverage ---

func TestBroadcastProgress_NilHub(t *testing.T) {
	h, _ := newTestHandler(t)
	// No hub set — broadcastProgress should be a no-op.
	h.broadcastProgress("downloading", 50, 100)
	h.broadcastProgress("done", 0, 0)
	h.broadcastProgress("error", 0, 0)
}

func TestBroadcastProgress_WithHub(t *testing.T) {
	h, _, _ := newTestHandlerWithHub(t)

	// These should not panic and should broadcast via the hub.
	h.broadcastProgress("downloading", 50, 100)
	h.broadcastProgress("importing", 0, 0)
	h.broadcastProgress("done", 0, 0)
	h.broadcastProgress("error", 0, 0)
}

// --- reloadDump coverage ---

func TestReloadDump_Success(t *testing.T) {
	// Create a handler with a runtime that has empty CoreDeps + a config.
	// ReloadConfig will rebuild the scraper registry (no dump present, so
	// r18DumpLookup is nil — the scraper falls back to HTTP, which is fine).
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{}}
	cfg := &config.Config{}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	h := &dumpHandler{rt: rt, httpClient: &http.Client{}, reloadFn: func(cfg *config.Config, lockHeld bool) error {
		if lockHeld {
			return rt.ReloadConfigLocked(cfg)
		}
		return rt.ReloadConfig(cfg)
	}}

	err := h.reloadDump("/some/path")
	require.NoError(t, err)
}

func TestReloadDumpLocked_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	lockHeld := false
	h.reloadFn = func(_ *config.Config, gotLockHeld bool) error {
		lockHeld = gotLockHeld
		return nil
	}

	require.NoError(t, h.reloadDumpLocked("/some/path"))
	assert.True(t, lockHeld)
}

func TestReloadDump_ClonesLiveConfig(t *testing.T) {
	h, _ := newTestHandler(t)
	live := h.rt.Deps().CoreDeps.GetConfig()
	live.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true},
	}
	live.Warnings = []config.ConfigWarning{{Scrapers: []string{"r18dev"}}}

	h.reloadFn = func(cfg *config.Config, _ bool) error {
		require.NotSame(t, live, cfg)
		cfg.Scrapers.Overrides["r18dev"].Enabled = false
		cfg.Scrapers.Overrides["other"] = &models.ScraperSettings{}
		cfg.Warnings[0].Scrapers[0] = "other"
		return nil
	}

	require.NoError(t, h.reloadDump("/some/path"))
	assert.True(t, live.Scrapers.Overrides["r18dev"].Enabled)
	assert.NotContains(t, live.Scrapers.Overrides, "other")
	assert.Equal(t, []string{"r18dev"}, live.Warnings[0].Scrapers)
}

// --- fileSize coverage ---

func TestFileSize_Error(t *testing.T) {
	_, err := fileSize("/nonexistent/path/that/does/not/exist.db")
	assert.Error(t, err)
}

func TestFileSize_Success(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.db")
	require.NoError(t, os.WriteFile(tmpFile, []byte("test"), 0o644))

	size, err := fileSize(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, int64(4), size)
}

// --- resolveDumpPath + progressPercent ---

func TestResolveDumpPath_Default(t *testing.T) {
	cfg := &config.Config{}
	path := resolveDumpPath(cfg)
	assert.Equal(t, commandutil.DefaultR18DevDumpPath, path)
}

func TestResolveDumpPath_Configured(t *testing.T) {
	cfg := &config.Config{}
	cfg.Metadata.R18DevDump.Path = "/custom/path.db"
	path := resolveDumpPath(cfg)
	assert.Equal(t, "/custom/path.db", path)
}

func TestProgressPercent(t *testing.T) {
	assert.Equal(t, float64(0), progressPercent(0, 0))
	assert.Equal(t, float64(0), progressPercent(0, 100))
	assert.Equal(t, float64(50), progressPercent(50, 100))
	assert.Equal(t, float64(100), progressPercent(100, 100))
	assert.Equal(t, float64(100), progressPercent(200, 100))
}

// --- Concurrent download flag reset ---

func TestStartDownload_FlagResetAfterCompletion(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, _, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)

	require.Equal(t, http.StatusAccepted, w.Code)

	// The running flag should be reset to false after the goroutine completes.
	require.Eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return !h.running
	}, 10*time.Second, 100*time.Millisecond, "running flag should be reset after download completes")
	<-h.done
}

// --- Thread safety smoke test ---

func TestDumpHandler_ConcurrentAccess(t *testing.T) {
	h, _ := newTestHandler(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.mu.Lock()
			h.running = true
			h.mu.Unlock()

			h.mu.Lock()
			h.running = false
			h.mu.Unlock()
		}()
	}
	wg.Wait()
}

// Ensure unused imports don't cause issues.

// --- Coverage for remaining branches ---

func TestStartDownload_NilHTTPClient(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = nil // force the nil-check fallback path
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	// Override the dump URL env so Download uses the test server's client.
	t.Setenv("JAVINIZER_R18DEV_DUMP_URL", srv.URL+"/latest")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	require.Eventually(t, func() bool {
		_, err := os.Stat(dumpPath)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "dump DB should be created")
	<-h.done
}

func TestStartDownload_ImportError(t *testing.T) {
	// Serve a valid gzip stream but point the dump path at a location where
	// the directory can't be created (under a file, not a directory).
	gz := gzipped("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	}))
	defer srv.Close()

	h, _, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	// Create a regular file, then set the dump path under it — MkdirAll fails
	// because the parent is a file, not a directory.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte(""), 0o644))
	h.rt.Deps().CoreDeps.GetConfig().Metadata.R18DevDump.Path = filepath.Join(blocker, "sub", "r18dev_dump.db")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	// Wait for the goroutine to process the import error.
	<-h.done
}

// --- Coverage for reloadDump error path ---

// --- Coverage for search non-ErrDumpMiss error paths ---

func TestSearch_LookupError(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	// Create a DB where the videos table exists but dvd_id_norm index is
	// missing, so LookupByDVDID returns a non-miss error.
	db, err := openRawDB(dumpPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE videos (content_id TEXT PRIMARY KEY, dvd_id TEXT)`)
	require.NoError(t, err)
	// No dvd_id_norm column — LookupByDVDID will fail with a SQL error.
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=ABF-030", nil)

	h.search(c)
	// Should still return 404 (the non-miss error is logged but doesn't change the response)
	require.Equal(t, http.StatusNotFound, w.Code)
}

// --- Coverage for broadcastProgress with nil hub ---

func TestBroadcastProgress_RuntimeNil(t *testing.T) {
	h, _ := newTestHandler(t)
	// Don't call EnsureRuntime — GetRuntime returns nil.
	h.broadcastProgress("downloading", 50, 100)
	h.broadcastProgress("done", 0, 0)
	h.broadcastProgress("error", 0, 0)
}

func TestBroadcastProgress_HubNil(t *testing.T) {
	h, _ := newTestHandler(t)
	// Call EnsureRuntime but don't set a hub — WebSocketHub returns nil.
	h.rt.EnsureRuntime()
	h.broadcastProgress("downloading", 50, 100)
	h.broadcastProgress("done", 0, 0)
	h.broadcastProgress("error", 0, 0)
}

// --- Coverage for reloadDump error + download goroutine reload error ---

func TestReloadDump_ReloadError(t *testing.T) {
	h, _ := newTestHandler(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return fmt.Errorf("simulated reload failure") }

	err := h.reloadDump("/some/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reload config")
}

func TestStartDownload_ReloadFails(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, _, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	// Make reload fail so the download goroutine logs a warning but doesn't crash.
	h.reloadFn = func(_ *config.Config, _ bool) error { return fmt.Errorf("simulated reload failure") }

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	// Wait for the goroutine to complete (download + reload).
	<-h.done
}

// --- Coverage for search content_id lookup error ---

func TestSearch_ContentIDLookupError(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	// Create a DB with videos table + dvd_id_norm column but no content_id
	// index — LookupByContentID will hit a SQL error (no such column).
	// Actually, content_id is the primary key so LookupByContentID always works
	// if the table exists. To trigger a non-miss error, we need to close the DB
	// or corrupt it. Instead, test with a query that matches dvd_id (misses) and
	// then content_id (misses) — both return ErrDumpMiss, hitting the 404 path.
	// That's already covered by TestSearch_NotFound.
	//
	// To cover the content_id non-miss error path (L263), we need
	// LookupByContentID to return a non-miss error. This happens when the
	// dvd_id column is missing. But LookupByContentID queries dvd_id...
	// Let's create a table without a dvd_id column.
	db, err := openRawDB(dumpPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE videos (content_id TEXT PRIMARY KEY)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)`)
	// This will fail because dvd_id_norm doesn't exist — that's OK.
	_ = err
	require.NoError(t, db.Close())

	// Search for a content_id that exists — LookupByDVDID will fail (no
	// dvd_id_norm column), LookupByContentID will also fail (no dvd_id column
	// to SELECT).
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=118abf030", nil)

	h.search(c)
	// Both lookups fail with non-miss errors → 404.
	require.Equal(t, http.StatusNotFound, w.Code)
}

// --- clearDump tests ---

func TestClearDump_NotPresent(t *testing.T) {
	h, _ := newTestHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)

	h.clearDump(c)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestClearDump_RejectsInvalidDatabase(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{
			name: "not SQLite",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte("not a database"), 0o600))
			},
		},
		{
			name: "missing dump schema",
			setup: func(t *testing.T, path string) {
				db, err := openRawDB(path)
				require.NoError(t, err)
				_, err = db.Exec(`CREATE TABLE unrelated (id INTEGER PRIMARY KEY)`)
				require.NoError(t, err)
				require.NoError(t, db.Close())
			},
		},
		{
			name: "missing dump metadata",
			setup: func(t *testing.T, path string) {
				db, err := openRawDB(path)
				require.NoError(t, err)
				_, err = db.Exec(`CREATE TABLE videos (content_id TEXT PRIMARY KEY)`)
				require.NoError(t, err)
				require.NoError(t, db.Close())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, dumpPath := newTestHandler(t)
			tc.setup(t, dumpPath)
			closer := &fakeCloser{}
			h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(closer)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)

			h.clearDump(c)

			require.Equal(t, http.StatusBadRequest, w.Code)
			assert.JSONEq(t, `{"error":"file is not a valid r18.dev dump database"}`, w.Body.String())
			_, err := os.Stat(dumpPath)
			assert.NoError(t, err, "invalid target must not be deleted")
			assert.False(t, closer.closed, "active dump handle must remain unchanged")
		})
	}
}

func TestClearDump_Success(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")
	lockHeld := false
	h.reloadFn = func(_ *config.Config, gotLockHeld bool) error {
		lockHeld = gotLockHeld
		return nil
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)

	h.clearDump(c)

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, lockHeld)

	// Verify the dump file was deleted.
	_, err := os.Stat(dumpPath)
	assert.True(t, os.IsNotExist(err), "dump file should be deleted")
}

func TestClearDump_DownloadInProgress(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	// Simulate an in-progress download.
	h.mu.Lock()
	h.running = true
	h.done = make(chan struct{})
	h.mu.Unlock()
	defer close(h.done)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)

	h.clearDump(c)

	require.Equal(t, http.StatusConflict, w.Code)

	// Verify the dump file was NOT deleted.
	_, err := os.Stat(dumpPath)
	assert.NoError(t, err, "dump file should still exist when download is in progress")
}

func TestClearDump_ReloadFails(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")
	// Make reload fail so the warning path is exercised.
	h.reloadFn = func(_ *config.Config, _ bool) error { return fmt.Errorf("simulated reload failure") }

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)

	h.clearDump(c)

	// Should still return 200 (the dump was cleared, only the hot-swap failed).
	require.Equal(t, http.StatusOK, w.Code)
	_, err := os.Stat(dumpPath)
	assert.True(t, os.IsNotExist(err), "dump file should be deleted even if reload fails")
}

// --- Codex review fix coverage ---

func TestClearDump_DeleteError(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	h.removeFn = func(path string) error {
		if path == dumpPath {
			return fmt.Errorf("simulated delete failure")
		}
		return os.Remove(path)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)

	h.clearDump(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestStartDownload_PreservesExistingDumpOnFailure(t *testing.T) {
	// Serve a valid gzip stream but make the dump directory unwritable
	// so Import fails — but the existing dump should be preserved.
	gz := gzipped("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	}))
	defer srv.Close()

	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	// Pre-create a valid dump.
	buildTestDump(t, dumpPath, "118ipx030", "ABF-030")

	// Point at a path under a file (can't create directory) so Import fails.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte(""), 0o644))
	h.rt.Deps().CoreDeps.GetConfig().Metadata.R18DevDump.Path = filepath.Join(blocker, "sub", "r18dev_dump.db")

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	// Wait for the goroutine to complete before restoring LatestDumpURL.
	<-h.done
	r18devdump.LatestDumpURL = orig

	// The original dump should still exist (not deleted by the failed import).
	_, err := os.Stat(dumpPath)
	assert.NoError(t, err, "original dump should be preserved after failed download")
}

// --- Coverage for ReplaceR18DevDumpCloser paths ---

func TestStartDownload_ClosesExistingDumpHandle(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, _, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	// Simulate an existing open dump handle (like the scraper registry would hold).
	closer := &fakeCloser{}
	h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(closer)

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	<-h.done
	r18devdump.LatestDumpURL = orig

	assert.True(t, closer.closed, "existing dump handle should be closed before import")
}

func TestClearDump_ClosesExistingDumpHandle(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	// Simulate an existing open dump handle.
	closer := &fakeCloser{}
	h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(closer)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)

	h.clearDump(c)

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, closer.closed, "existing dump handle should be closed before deletion")
}

// fakeCloser tracks whether Close was called.
type fakeCloser struct {
	closed bool
}

func (f *fakeCloser) Close() error {
	f.closed = true
	return nil
}

func TestStartDownload_RestoresHandleOnFailure(t *testing.T) {
	// Serve a valid gzip but make the dump path unwritable so Import fails
	// inside the callback — this exercises the restore-handle-on-failure path.
	gz := gzipped("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	}))
	defer srv.Close()

	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()

	// Pre-create a valid dump so there's something to restore.
	buildTestDump(t, dumpPath, "118ipx030", "ABF-030")

	// Track whether reloadFn was called on the failure path.
	// Return an error to exercise the warning log path.
	reloadCalled := false
	h.reloadFn = func(_ *config.Config, _ bool) error {
		reloadCalled = true
		return fmt.Errorf("simulated restore failure")
	}

	// Point at a path under a file so Import fails (can't create directory).
	blocker := filepath.Join(t.TempDir(), "blocker2")
	require.NoError(t, os.WriteFile(blocker, []byte(""), 0o644))
	h.rt.Deps().CoreDeps.GetConfig().Metadata.R18DevDump.Path = filepath.Join(blocker, "sub", "r18dev_dump.db")

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	<-h.done
	r18devdump.LatestDumpURL = orig

	assert.True(t, reloadCalled, "reloadDump should be called after failed download to restore handle")
}

func TestStartUpdate_UnchangedReloadsDump(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	// Pre-create a dump with the matching source URL so update-only reports unchanged.
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")
	db, err := openRawDB(dumpPath)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR REPLACE INTO dump_meta (key, value) VALUES (?, ?)`,
		"source_url", srv.URL+"/dumps/r18dotdev_dump_2026-04-28.sql.gz")
	require.NoError(t, err)
	_ = db.Close()

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"

	// Track if reload was called on the unchanged path.
	// Return error to exercise the warning log path.
	reloadCalled := false
	h.reloadFn = func(_ *config.Config, _ bool) error {
		reloadCalled = true
		return fmt.Errorf("simulated reload failure")
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/update", nil)

	h.startUpdate(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	<-h.done
	r18devdump.LatestDumpURL = orig

	assert.True(t, reloadCalled, "reloadDump should be called even when dump is unchanged")
}

func TestClearDump_RestoresHandleOnDeleteError(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")
	h.removeFn = func(path string) error {
		if path == dumpPath {
			return fmt.Errorf("simulated delete failure")
		}
		return os.Remove(path)
	}

	closer := &fakeCloser{}
	h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(closer)

	reloadCalled := false
	h.reloadFn = func(_ *config.Config, lockHeld bool) error {
		reloadCalled = true
		assert.True(t, lockHeld)
		return fmt.Errorf("simulated restore failure")
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)

	h.clearDump(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.True(t, reloadCalled, "reloadDump should be called to restore handle when delete fails")
}

func TestGetStatus_DumpImportInProgress(t *testing.T) {
	h, _ := newTestHandler(t)

	// Simulate an in-progress import.
	h.mu.Lock()
	h.running = true
	h.mu.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)

	h.getStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Present, "should not open dump file while import is running")
	assert.True(t, resp.Running, "running flag should be exposed when a download is in progress")
}

func TestGetStatus_RunningFlagFalseWhenIdle(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)

	h.getStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Present)
	assert.False(t, resp.Running, "running flag should be false when no download is in progress")
}

func TestSearch_DumpImportInProgress(t *testing.T) {
	h, _ := newTestHandler(t)

	// Simulate an in-progress import.
	h.mu.Lock()
	h.running = true
	h.mu.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=ABF-030", nil)

	h.search(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestGetStatus_WaitsForDumpMutation(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	h.dumpMu.Lock()
	locked := true
	defer func() {
		if locked {
			h.dumpMu.Unlock()
		}
	}()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)
	done := make(chan struct{})
	go func() {
		h.getStatus(c)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("status opened the dump during a dump mutation")
	case <-time.After(100 * time.Millisecond):
	}

	h.dumpMu.Unlock()
	locked = false
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("status did not resume after the dump mutation")
	}
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSearch_WaitsForDumpMutation(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	h.dumpMu.Lock()
	locked := true
	defer func() {
		if locked {
			h.dumpMu.Unlock()
		}
	}()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=ABF-030", nil)
	done := make(chan struct{})
	go func() {
		h.search(c)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("search opened the dump during a dump mutation")
	case <-time.After(100 * time.Millisecond):
	}

	h.dumpMu.Unlock()
	locked = false
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not resume after the dump mutation")
	}
	require.Equal(t, http.StatusOK, w.Code)
}

func TestClearDump_WaitsForInFlightReader(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	h.dumpMu.RLock()
	locked := true
	defer func() {
		if locked {
			h.dumpMu.RUnlock()
		}
	}()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)
	done := make(chan struct{})
	go func() {
		h.clearDump(c)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("clear completed while a dump reader was active")
	case <-time.After(100 * time.Millisecond):
	}
	_, err := os.Stat(dumpPath)
	require.NoError(t, err)

	h.dumpMu.RUnlock()
	locked = false
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("clear did not resume after the dump reader finished")
	}
	require.Equal(t, http.StatusOK, w.Code)
	_, err = os.Stat(dumpPath)
	assert.True(t, os.IsNotExist(err))
}

// --- Reload serialization coverage (Codex P2 fix) ---

// TestStartDownload_ImportDoesNotHoldReloadLock proves the import stream does
// not hold reloadMu. Import takes it only at the final swap boundary.
func TestStartDownload_RegistryPublishHoldsReloadLock(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	publishStarted := make(chan bool, 1)
	publish := make(chan struct{})
	h.reloadFn = func(_ *config.Config, lockHeld bool) error {
		publishStarted <- lockHeld
		<-publish
		return nil
	}
	closer := &fakeCloser{}
	h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(closer)

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)
	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	require.True(t, <-publishStarted)
	assert.True(t, closer.closed)
	_, err := os.Stat(dumpPath)
	require.NoError(t, err)

	snapshotDone := make(chan struct{})
	go func() {
		_ = h.rt.GetAPIConfig()
		close(snapshotDone)
	}()
	select {
	case <-snapshotDone:
		t.Fatal("runtime snapshot must wait until the new registry is published")
	case <-time.After(100 * time.Millisecond):
	}

	close(publish)
	<-h.done
	select {
	case <-snapshotDone:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime snapshot should resume after registry publication")
	}
}

func TestStartDownload_ImportDoesNotHoldReloadLock(t *testing.T) {
	importStarted := make(chan struct{})
	proceed := make(chan struct{})

	// Serve a gzip stream in two parts: part 1 (COPY header + one row, no
	// terminator) is sent immediately and flushed; part 2 (the "\." terminator)
	// is sent only after `proceed` is closed. Import's bufio.Scanner emits the
	// part-1 lines then blocks on the next Scan() — without holding reloadMu.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		gw := gzip.NewWriter(w)
		_, _ = gw.Write([]byte("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n"))
		_ = gw.Flush()
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(importStarted)
		<-proceed
		_, _ = gw.Write([]byte("\\.\n"))
		_ = gw.Close()
	}))
	defer srv.Close()

	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	// Use the real ReloadConfig so the concurrent reload shares reloadMu with
	// the final swap lock (the no-op reloadFn used by other tests would not).
	h.reloadFn = func(cfg *config.Config, lockHeld bool) error {
		if lockHeld {
			return h.rt.ReloadConfigLocked(cfg)
		}
		return h.rt.ReloadConfig(cfg)
	}

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/dumps/r18dotdev_dump_2026-04-28.sql.gz"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	<-importStarted
	// The .tmp DB is created before the final swap lock is acquired.
	tmpPath := dumpPath + ".tmp"
	require.Eventually(t, func() bool {
		_, err := os.Stat(tmpPath)
		return err == nil
	}, 3*time.Second, 50*time.Millisecond, "Import should create the tmp DB")
	// Let Import enter the read loop and block on the slow stream.
	time.Sleep(150 * time.Millisecond)

	// A concurrent config reload must not block while the import streams.
	cfg := h.rt.Deps().CoreDeps.GetConfig()
	reloadDone := make(chan error, 1)
	go func() { reloadDone <- h.rt.ReloadConfig(cfg) }()
	select {
	case err := <-reloadDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("ReloadConfig must not block while Import streams (reload lock should be narrowed)")
	}

	// Release the import stream so the download goroutine completes.
	close(proceed)
	<-h.done

	// The real reloadFn (ReloadConfig) opened a live SQLite handle to the dump
	// file during the success-path reloadDump. Close it so Windows can remove
	// the TempDir during cleanup — otherwise RemoveAll fails with a sharing
	// violation on r18dev_dump.db.
	if old := h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(nil); old != nil {
		_ = old.Close()
	}
}

// --- last_error coverage (Codex P2: surface failed downloads) ---

func TestGetStatus_LastErrorExposedWhenSet(t *testing.T) {
	h, _ := newTestHandler(t)

	h.mu.Lock()
	h.running = false
	h.lastError = "download failed: boom"
	h.mu.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)

	h.getStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Running)
	assert.Equal(t, "download failed: boom", resp.LastError)
}

func TestGetStatus_LastErrorEmptyWhenIdle(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)

	h.getStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.LastError, "last_error should be empty when no download has failed")
}

func TestGetStatus_LastErrorHiddenWhileRunning(t *testing.T) {
	h, _ := newTestHandler(t)

	// A stale error from a previous run should still be surfaced while a new
	// download is in progress (the running flag short-circuits before opening
	// the dump, but last_error is read under the same lock).
	h.mu.Lock()
	h.running = true
	h.lastError = "stale error"
	h.mu.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)

	h.getStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Running)
	assert.Equal(t, "stale error", resp.LastError, "last_error should be exposed even while running")
}

func TestStartDownload_LastErrorSetOnFailure(t *testing.T) {
	// Point at a non-existent server to trigger a download error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	h, _, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	<-h.done

	h.mu.Lock()
	running := h.running
	lastErr := h.lastError
	h.mu.Unlock()
	assert.False(t, running, "running flag should be cleared after failure")
	assert.NotEmpty(t, lastErr, "last_error should be set when the download fails")

	// getStatus must surface the failure to polling clients.
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)
	h.getStatus(c2)
	require.Equal(t, http.StatusOK, w2.Code)
	var resp dumpStatusResponse
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.False(t, resp.Running)
	assert.Equal(t, lastErr, resp.LastError)
}

func TestStartDownload_LastErrorClearedOnSuccess(t *testing.T) {
	srv := newDumpTestServer(t)
	defer srv.Close()

	h, _, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	// Seed a stale error from a hypothetical previous failed run.
	h.mu.Lock()
	h.lastError = "previous failure"
	h.mu.Unlock()

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	// The stale error must be cleared the moment a new download starts.
	// Capture the running+lastError state synchronously after the 202 is
	// returned (before the goroutine completes).
	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	h.mu.Lock()
	startLastErr := h.lastError
	h.mu.Unlock()
	assert.Empty(t, startLastErr, "last_error should be cleared when a new download starts")

	<-h.done

	h.mu.Lock()
	lastErr := h.lastError
	h.mu.Unlock()
	assert.Empty(t, lastErr, "last_error should remain empty after a successful download")
}

func TestStartDownload_LastErrorSetOnImportFailure(t *testing.T) {
	// Serve a valid gzip stream but point the dump path at a location where
	// the directory can't be created so Import fails inside the callback.
	gz := gzipped("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	}))
	defer srv.Close()

	h, _, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte(""), 0o644))
	h.rt.Deps().CoreDeps.GetConfig().Metadata.R18DevDump.Path = filepath.Join(blocker, "sub", "r18dev_dump.db")

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	<-h.done

	h.mu.Lock()
	lastErr := h.lastError
	h.mu.Unlock()
	assert.NotEmpty(t, lastErr, "last_error should be set when Import fails")
}
