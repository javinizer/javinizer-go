package r18devdump

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

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
	rt      *core.APIRuntime
	mu      sync.Mutex
	running bool
}

func newDumpHandler(rt *core.APIRuntime) *dumpHandler {
	return &dumpHandler{rt: rt}
}

// dumpStatusResponse is the JSON shape returned by GET /status.
type dumpStatusResponse struct {
	Present    bool   `json:"present"`
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
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.running = false
		h.mu.Unlock()
	}()

	cfg := h.rt.Deps().CoreDeps.GetConfig()
	path := resolveDumpPath(cfg)
	ctx := c.Request.Context()

	// For update-only, check the current source URL so the download skips if
	// the version is unchanged.
	var currentSourceURL string
	if updateOnly {
		if store, err := r18devdump.Open(path); err == nil {
			stats, err := store.Stats(ctx)
			_ = store.Close()
			if err == nil {
				currentSourceURL = stats.SourceURL
			}
		}
	}

	progress := func(bytes, total int64) {
		h.broadcastProgress("downloading", bytes, total)
	}

	// Run the download in a background goroutine. The HTTP response is already
	// sent as 202. The context from the request keeps it cancellable if the
	// client disconnects.
	go func() {
		res, err := r18devdump.Download(ctx, &http.Client{}, currentSourceURL, progress, func(r io.Reader, d r18devdump.DownloadResult) error {
			h.broadcastProgress("importing", 0, 0)
			impRes, err := r18devdump.Import(ctx, r, path, r18devdump.ImportOptions{
				SourceURL:  d.FinalURL,
				SourceDate: d.SourceDate,
			})
			if err != nil {
				return err
			}
			_ = impRes
			h.broadcastProgress("done", 0, 0)
			return nil
		})
		if err != nil {
			logging.Warnf("r18dev dump download failed: %v", err)
			h.broadcastProgress("error", 0, 0)
			return
		}
		if res.Unchanged {
			logging.Infof("r18dev dump unchanged (%s), no update needed", res.SourceDate)
			h.broadcastProgress("done", 0, 0)
			return
		}
		// Hot-swap: reload the scraper registry so it picks up the new dump
		// without a server restart.
		if reloadErr := h.reloadDump(path); reloadErr != nil {
			logging.Warnf("r18dev dump downloaded but hot-swap failed: %v", reloadErr)
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{"message": "download started", "update_only": updateOnly})
}

// reloadDump reopens the dump store and triggers a config reload so the scraper
// registry rebuilds with the new dump lookup wired in.
func (h *dumpHandler) reloadDump(path string) error {
	cfg := h.rt.Deps().CoreDeps.GetConfig()
	// ReloadConfig reopens the dump via OpenR18DevDumpLookup and rebuilds the
	// scraper registry atomically. This is the same path the config hot-reload
	// uses, so it's safe to call from a request goroutine.
	if err := h.rt.ReloadConfig(cfg); err != nil {
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

// broadcastProgress sends a dump progress message over the WebSocket hub so
// the WebUI can render a progress bar. Non-blocking — if no WS clients are
// connected, the message is silently dropped.
func (h *dumpHandler) broadcastProgress(phase string, bytes, total int64) {
	hub := h.rt.GetRuntime().WebSocketHub()
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
	if err := hub.BroadcastProgress(msg); err != nil {
		logging.Debugf("r18dev dump: failed to broadcast progress: %v", err)
	}
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
