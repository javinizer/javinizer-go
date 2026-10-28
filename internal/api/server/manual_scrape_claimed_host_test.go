package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraper/mgstage"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestBatchScrapeRejectsClaimedHostWithoutProductID(t *testing.T) {
	// The real scraper factory clones DefaultTransport. Preserve its concrete
	// type and fail closed at either dial path; no provider or socket is needed.
	// This test must not run in parallel while replacing the default transport.
	originalTransport := http.DefaultTransport
	transport := originalTransport.(*http.Transport).Clone()
	var outboundCalls atomic.Int64
	denyDial := func(context.Context, string, string) (net.Conn, error) {
		outboundCalls.Add(1)
		return nil, fmt.Errorf("unexpected outbound request during manual-input rejection")
	}
	transport.DialContext = denyDial
	transport.DialTLSContext = denyDial
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
		transport.CloseIdleConnections()
		require.Zero(t, outboundCalls.Load(), "rejected intake must not contact a scraper")
	})

	root := t.TempDir()
	mediaDir, tempDir := filepath.Join(root, "media"), filepath.Join(root, "temp")
	for _, dir := range []string{mediaDir, tempDir} {
		require.NoError(t, os.Mkdir(dir, 0o700))
	}
	filePath := filepath.Join(mediaDir, "ABC-123.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("owned video fixture"), 0o600))

	// Register and initialize the built-in MGStage implementation, not a mock
	// URL classifier. All config is finalized before the runtime can read it.
	registry := scraperutil.NewScraperRegistry()
	mgstage.Register(registry)
	cfg := config.DefaultConfig([]string{"mgstage"}, nil)
	cfg.Scrapers.ApplyOverride("mgstage", func(s *models.ScraperSettings) { s.Enabled = true })
	cfg.Database.DSN = filepath.Join(root, "catalog.db")
	cfg.System.TempDir = tempDir
	cfg.System.VersionCheckEnabled = false
	cfg.System.ImageCacheEnabled = false
	cfg.System.ImageCacheTTLHours = 0
	cfg.API.Security.AllowedDirectories = []string{mediaDir}
	require.NoError(t, cfg.Scrapers.Finalize(registry))
	_, err := config.Prepare(cfg)
	require.NoError(t, err)
	registry, err = scraper.NewDefaultScraperRegistryFrom(registry,
		scraper.ScraperRegistryConfigFromApp(cfg, registry.Names(), registry.GetAllDefaults()), nil, nil)
	require.NoError(t, err)
	configPath := filepath.Join(root, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))
	deps, rt, err := core.BootstrapAPIWithOpts(cfg, configPath, testkit.NoOpAuth{},
		&commandutil.DependenciesOptions{ScraperRegistry: registry, Ctx: t.Context()})
	require.NoError(t, err)
	t.Cleanup(func() {
		testkit.CleanupServerHub(t, rt)
		require.True(t, rt.WaitBackgroundTasks(5*time.Second))
		require.NoError(t, deps.CoreDeps.Close())
	})
	router := NewServer(rt)
	require.True(t, rt.WaitBackgroundTasks(5*time.Second), "startup repair must settle before state snapshots")

	const manualURL = "https://www.mgstage.com/?token=fixture-secret"
	instances := rt.Snapshot().Registry().GetEnabledInstances()
	require.Len(t, instances, 1)
	require.Equal(t, "mgstage", instances[0].Name())
	handler, ok := instances[0].(models.URLHandler)
	require.True(t, ok)
	require.True(t, handler.CanHandleURL(manualURL), "this is not an unclaimed-host rejection")
	_, err = handler.ExtractIDFromURL(manualURL)
	require.Error(t, err, "a claimed host still needs a product-detail identifier")

	beforeJobs := deps.JobStore.ListJobs()
	require.Empty(t, beforeJobs)
	beforeRows, err := deps.Repos.JobRepo.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, beforeRows)
	beforeConfig, err := json.Marshal(cfg)
	require.NoError(t, err)
	beforeFiles := manualRejectionFiles(t, root)

	body, err := json.Marshal(BatchScrapeRequest{
		Files: []string{filePath}, ManualInputs: map[string]string{filePath: manualURL},
		SelectedScrapers: []string{"mgstage"},
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/batch/scrape", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.NotContains(t, payload, "job_id")
	var message string
	require.NoError(t, json.Unmarshal(payload["error"], &message))
	require.Contains(t, message, `URL matched scraper "mgstage" but extraction failed`)
	for _, sensitive := range []string{manualURL, "?token=", "fixture-secret"} {
		require.NotContains(t, response.Body.String(), sensitive)
	}

	require.True(t, rt.WaitBackgroundTasks(5*time.Second), "join any unexpectedly launched request work")
	require.Equal(t, beforeJobs, deps.JobStore.ListJobs())
	afterRows, err := deps.Repos.JobRepo.List(t.Context())
	require.NoError(t, err)
	require.Equal(t, beforeRows, afterRows, "rejection must not persist a job")
	afterConfig, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.Equal(t, beforeConfig, afterConfig, "intake must not mutate canonical config")
	require.Equal(t, beforeFiles, manualRejectionFiles(t, root), "media, config, catalog and temp files must remain unchanged")
	require.Zero(t, outboundCalls.Load())
}

// Include directories, modes and file contents, so a new job directory or an
// overwritten source is detected as well as a new file. Access times are not a
// persistence contract, and SQLite read locks do not change catalog contents.
func manualRejectionFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		key, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[key] = info.Mode().String()
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[key] += fmt.Sprintf(":%x", sha256.Sum256(data))
		}
		return nil
	}))
	return files
}
