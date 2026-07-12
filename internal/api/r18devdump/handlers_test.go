package r18devdump

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
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

	return &dumpHandler{rt: rt}, dumpPath
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
