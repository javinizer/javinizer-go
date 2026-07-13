package r18devdump

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"

	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

const (
	dumpJobID = "r18dev-dump-download"
)

// dumpHandler holds the APIRuntime reference and a mutex guarding concurrent
// download/update operations. The dump download streams ~250MB over several
// minutes, so only one may run at a time.
type dumpHandler struct {
	rt         *core.APIRuntime
	mu         sync.Mutex
	dumpMu     sync.RWMutex
	running    bool
	lastError  string // last download outcome; non-empty when the most recent run failed
	httpClient *http.Client
	reloadFn   func(cfg *config.Config, lockHeld bool) error
	removeFn   func(string) error
	done       chan struct{} // closed when the download goroutine finishes
}

func newDumpHandler(rt *core.APIRuntime) *dumpHandler {
	h := &dumpHandler{rt: rt, httpClient: &http.Client{}, removeFn: os.Remove}
	h.reloadFn = func(cfg *config.Config, lockHeld bool) error {
		if lockHeld {
			return h.rt.ReloadConfigLocked(cfg)
		}
		return h.rt.ReloadConfig(cfg)
	}
	return h
}

// dumpStatusResponse is the JSON shape returned by GET /status.
type dumpStatusResponse struct {
	Present    bool   `json:"present"`
	Running    bool   `json:"running"`
	LastError  string `json:"last_error,omitempty"`
	RowCount   int64  `json:"row_count,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	SourceDate string `json:"source_date,omitempty"`
	ImportedAt string `json:"imported_at,omitempty"`
	Path       string `json:"path"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	Enabled    bool   `json:"enabled"`
}

// getStatus godoc
// @Summary Get r18.dev dump status
// @Description Check whether the local r18.dev dump sidecar is present and return its stats (rows, source date, size).
// @Tags r18dev
// @Produce json
// @Success 200 {object} dumpStatusResponse
// @Router /api/v1/r18dev/dump/status [get]
func (h *dumpHandler) getStatus(c *gin.Context) {
	cfg := h.rt.Deps().CoreDeps.GetConfig()
	path := resolveDumpPath(cfg)

	resp := dumpStatusResponse{
		Path:    path,
		Enabled: cfg.Metadata.R18DevDump.Enabled,
	}

	// Don't open the dump file while an import is in progress — on Windows,
	// the open handle can block the import's os.Rename. Return the last
	// known status instead.
	h.mu.Lock()
	running := h.running
	lastErr := h.lastError
	h.mu.Unlock()
	resp.Running = running
	resp.LastError = lastErr
	if running {
		c.JSON(http.StatusOK, resp)
		return
	}

	h.dumpMu.RLock()
	defer h.dumpMu.RUnlock()
	store, err := r18devdump.Open(path)
	if err != nil {
		// Not present or unreadable — return present:false, not an error.
		c.JSON(http.StatusOK, resp)
		return
	}
	defer func() { _ = store.Close() }()

	stats, err := store.Stats(c.Request.Context())
	if err != nil {
		logging.Warnf("r18dev dump status: failed to read stats from %s: %v", path, err)
		c.JSON(http.StatusOK, resp)
		return
	}

	resp.Present = true
	resp.RowCount = stats.RowCount
	resp.SourceURL = stats.SourceURL
	resp.SourceDate = stats.SourceDate
	resp.ImportedAt = stats.ImportedAt
	if size, err := fileSize(path); err == nil {
		resp.SizeBytes = size
	}

	c.JSON(http.StatusOK, resp)
}

// startDownload godoc
// @Summary Download the r18.dev dump
// @Description Start a full download of the r18.dev database dump and build the local lookup database. Progress is streamed via WebSocket. Returns 409 if a download is already running.
// @Tags r18dev
// @Accept json
// @Produce json
// @Success 202 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/v1/r18dev/dump/download [post]
func (h *dumpHandler) startDownload(c *gin.Context) {
	h.startDownloadOrUpdate(c, false)
}

// startUpdate godoc
// @Summary Update the r18.dev dump
// @Description Re-download the dump only if a newer version is available. Progress is streamed via WebSocket. Returns 409 if a download is already running.
// @Tags r18dev
// @Accept json
// @Produce json
// @Success 202 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/v1/r18dev/dump/update [post]
func (h *dumpHandler) startUpdate(c *gin.Context) {
	h.startDownloadOrUpdate(c, true)
}

func (h *dumpHandler) startDownloadOrUpdate(c *gin.Context, updateOnly bool) {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "a dump download is already in progress"})
		return
	}
	h.running = true
	h.lastError = ""
	h.done = make(chan struct{})
	h.mu.Unlock()

	cfg := h.rt.Deps().CoreDeps.GetConfig()
	path := resolveDumpPath(cfg)
	// Use a detached context with a generous timeout so the download goroutine
	// survives the HTTP response being sent (202). The request context is
	// cancelled when the handler returns, which would abort the download.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)

	// For update-only, check the current source URL so the download skips if
	// the version is unchanged.
	var currentSourceURL string
	if updateOnly {
		h.dumpMu.RLock()
		if store, err := r18devdump.Open(path); err == nil {
			stats, err := store.Stats(ctx)
			_ = store.Close()
			if err == nil {
				currentSourceURL = stats.SourceURL
			}
		}
		h.dumpMu.RUnlock()
	}

	progress := func(bytes, total int64) {
		h.broadcastProgress("downloading", bytes, total)
	}

	// Run the download in a background goroutine. The HTTP response is already
	// sent as 202. The context from the request keeps it cancellable if the
	// client disconnects.
	client := h.httpClient
	if client == nil {
		client = &http.Client{}
	}

	done := h.done
	go func() {
		defer cancel()
		defer close(done)
		var succeeded bool
		var failErr error
		defer func() {
			h.mu.Lock()
			h.running = false
			if !succeeded {
				h.lastError = failErr.Error()
			} else {
				h.lastError = ""
			}
			h.mu.Unlock()
			// Only broadcast 'done' if the download succeeded.
			// If it failed, the error path already broadcast 'error'.
			if succeeded {
				h.broadcastProgress("done", 0, 0)
			}
		}()
		res, err := r18devdump.Download(ctx, client, currentSourceURL, progress, func(r io.Reader, d r18devdump.DownloadResult) error {
			h.broadcastProgress("importing", 0, 0)
			var unlockReload func()
			impRes, importErr := r18devdump.Import(ctx, r, path, r18devdump.ImportOptions{
				SourceURL:  d.FinalURL,
				SourceDate: d.SourceDate,
				BeforeSwap: func() error {
					h.dumpMu.Lock()
					unlockReload = h.rt.LockReload()
					old := h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(nil)
					if old != nil {
						_ = old.Close()
					}
					return nil
				},
				AfterSwap: func() {
					defer h.dumpMu.Unlock()
					defer unlockReload()
					unlockReload = nil
					if reloadErr := h.reloadDumpLocked(path); reloadErr != nil {
						logging.Warnf("r18dev dump downloaded but hot-swap failed: %v", reloadErr)
					}
				},
			})
			_ = impRes
			if importErr != nil {
				// Import failed — restore the old dump handle so the scraper
				// can keep using the existing dump. The old file is intact
				// (Import writes to .tmp and only renames on success).
				if reloadErr := h.reloadDump(path); reloadErr != nil {
					logging.Warnf("r18dev dump: failed to restore handle after import error: %v", reloadErr)
				}
				return importErr
			}
			return nil
		})
		if err != nil {
			logging.Warnf("r18dev dump download failed: %v", err)
			failErr = err
			// Clean up only temp files — the existing dump (if any) is still
			// valid and should be preserved so the scraper can keep using it.
			// Import writes to path+".tmp" and only renames on success, so a
			// failed import leaves the original dump intact.
			for _, p := range []string{path + ".tmp", path + ".tmp-wal", path + ".tmp-shm"} {
				_ = os.Remove(p)
			}
			// Re-open the dump handle if we closed it above (restore the
			// previous dump so the scraper can keep using it).
			if reloadErr := h.reloadDump(path); reloadErr != nil {
				logging.Warnf("r18dev dump: failed to restore handle after failed download: %v", reloadErr)
			}
			h.broadcastProgress("error", 0, 0)
			return
		}
		if res.Unchanged {
			logging.Infof("r18dev dump unchanged (%s), no update needed", res.SourceDate)
			// Reload even when unchanged — the dump file may have been created
			// externally or a previous hot-swap may have failed. This lets the
			// API activate an already-present dump without a server restart.
			if reloadErr := h.reloadDump(path); reloadErr != nil {
				logging.Warnf("r18dev dump unchanged but hot-swap failed: %v", reloadErr)
			}
			succeeded = true
			return
		}
		succeeded = true
	}()

	c.JSON(http.StatusAccepted, gin.H{"message": "download started", "update_only": updateOnly})
}

// reloadDump reopens the dump store and triggers a config reload so the scraper
// registry rebuilds with the new dump lookup wired in.
func (h *dumpHandler) reloadDump(path string) error {
	return h.reloadDumpWith(path, false)
}

func (h *dumpHandler) reloadDumpLocked(path string) error {
	return h.reloadDumpWith(path, true)
}

func (h *dumpHandler) reloadDumpWith(path string, lockHeld bool) error {
	cfg := h.rt.Deps().CoreDeps.GetConfig().Clone()
	if err := h.reloadFn(cfg, lockHeld); err != nil {
		return fmt.Errorf("reload config after dump download: %w", err)
	}
	logging.Infof("r18dev dump hot-swapped: %s", path)
	return nil
}

// searchResponse is the JSON shape returned by GET /search.
type searchResponse struct {
	Query     string  `json:"query"`
	ContentID *string `json:"content_id"`
	DVDID     *string `json:"dvd_id"`
}

// search godoc
// @Summary Search the r18.dev dump
// @Description Look up a dvd_id or content_id in the local r18.dev dump. Tries dvd_id first, then content_id.
// @Tags r18dev
// @Param q query string true "dvd_id or content_id to look up"
// @Produce json
// @Success 200 {object} searchResponse
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/v1/r18dev/dump/search [get]
func (h *dumpHandler) search(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	cfg := h.rt.Deps().CoreDeps.GetConfig()
	path := resolveDumpPath(cfg)

	// Don't open the dump file while an import is in progress — on Windows,
	// the open handle can block the import's os.Rename.
	h.mu.Lock()
	running := h.running
	h.mu.Unlock()
	if running {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "dump import in progress"})
		return
	}

	h.dumpMu.RLock()
	defer h.dumpMu.RUnlock()
	store, err := r18devdump.Open(path)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "dump not downloaded"})
		return
	}
	defer func() { _ = store.Close() }()

	ctx := c.Request.Context()
	resp := searchResponse{Query: query}

	if cid, err := store.LookupByDVDID(ctx, query); err == nil {
		resp.ContentID = &cid
		c.JSON(http.StatusOK, resp)
		return
	} else if !errors.Is(err, models.ErrDumpMiss) {
		logging.Warnf("r18dev dump search: dvd_id lookup failed for %s: %v", query, err)
	}

	if did, err := store.LookupByContentID(ctx, query); err == nil {
		resp.DVDID = &did
		c.JSON(http.StatusOK, resp)
		return
	} else if !errors.Is(err, models.ErrDumpMiss) {
		logging.Warnf("r18dev dump search: content_id lookup failed for %s: %v", query, err)
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "not found in dump"})
}

// clearDump godoc
// @Summary Clear the r18.dev dump
// @Description Delete the local r18.dev dump sidecar database. After clearing, the scraper falls back to HTTP content_id resolution until the dump is re-downloaded.
// @Tags r18dev
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/v1/r18dev/dump [delete]
func (h *dumpHandler) clearDump(c *gin.Context) {
	h.dumpMu.Lock()
	defer h.dumpMu.Unlock()
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "a dump download is already in progress"})
		return
	}
	// Keep the lock held for the entire clear operation so a concurrent
	// download/update can't start while we're deleting the dump file.
	defer h.mu.Unlock()

	cfg := h.rt.Deps().CoreDeps.GetConfig()
	path := resolveDumpPath(cfg)

	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dump not present"})
		return
	}

	store, err := r18devdump.Open(path)
	if err == nil {
		_, err = store.Stats(c.Request.Context())
		_ = store.Close()
	}
	if err != nil {
		logging.Warnf("r18dev dump clear: refusing to delete invalid dump database %s: %v", path, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is not a valid r18.dev dump database"})
		return
	}

	// Close the active dump handle before deleting so the SQLite file can
	// be removed on Windows (which blocks deletion of open files).
	unlockReload := h.rt.LockReload()
	defer unlockReload()
	if old := h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(nil); old != nil {
		_ = old.Close()
	}

	// Remove the dump DB and its WAL/SHM sidecars. Report failure if the
	// main DB file can't be deleted (sidecar failures are non-fatal).
	var removeErr error
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := h.removeFn(p); err != nil && !os.IsNotExist(err) && p == path {
			removeErr = err
		}
	}
	if removeErr != nil {
		logging.Warnf("r18dev dump clear: failed to delete %s: %v", path, removeErr)
		// Restore the dump handle since the file still exists and the
		// closer was closed above. Without this, the scraper registry
		// points at a closed store and falls back to HTTP unnecessarily.
		if reloadErr := h.reloadDumpLocked(path); reloadErr != nil {
			logging.Warnf("r18dev dump clear: failed to restore handle after delete error: %v", reloadErr)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete dump file: %v", removeErr)})
		return
	}

	// Hot-swap: reload config so the scraper registry drops the old dump handle.
	if reloadErr := h.reloadDumpLocked(path); reloadErr != nil {
		logging.Warnf("r18dev dump cleared but hot-swap failed: %v", reloadErr)
	}

	logging.Infof("r18dev dump cleared: %s", path)
	c.JSON(http.StatusOK, gin.H{"message": "dump cleared"})
}

// broadcastProgress sends a dump progress message over the WebSocket hub so
// the WebUI can render a progress bar. Non-blocking — if no WS clients are
// connected, the message is silently dropped.
func (h *dumpHandler) broadcastProgress(phase string, bytes, total int64) {
	rt := h.rt.GetRuntime()
	if rt == nil {
		return
	}
	hub := rt.WebSocketHub()
	if hub == nil {
		return
	}
	msg := &ws.ProgressMessage{
		JobID:    dumpJobID,
		Status:   ws.ProgressStatusPending,
		Progress: progressPercent(bytes, total),
		Message:  phase,
	}
	if phase == "done" {
		msg.Status = ws.ProgressStatusSuccess
		msg.Progress = 100
	}
	if phase == "error" {
		msg.Status = ws.ProgressStatusError
		msg.Error = "dump download failed"
	}
	_ = hub.BroadcastProgress(msg)
}

func progressPercent(bytes, total int64) float64 {
	if total <= 0 {
		return 0
	}
	pct := float64(bytes) / float64(total) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

// resolveDumpPath returns the configured dump sidecar path, applying the
// default when the config leaves it empty.
func resolveDumpPath(cfg *config.Config) string {
	path := cfg.Metadata.R18DevDump.Path
	if path == "" {
		path = commandutil.DefaultR18DevDumpPath
	}
	return path
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
