package commandutil

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestRunBatchCommandCountsFileResultsNotMovieIDs(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	for _, rel := range []string{"a/GOOD-700.mp4", "b/GOOD-700.mp4", "c/MISS-701.mp4"} {
		path := filepath.Join(src, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("fixture video"), 0o600))
	}

	cfg := config.DefaultConfig(nil, nil)
	testutil.ConfigureLocalScraper(t, cfg, false)
	// Keep the fixture defaults, but serve a mixed result from a local endpoint.
	var misses atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" && r.URL.Query().Get("q") == "MISS-701" {
			misses.Add(1)
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/search" || r.URL.Query().Get("q") != "GOOD-700" {
			t.Errorf("unexpected fixture request: %s", r.URL)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"normalized_id":"GOOD-700","title":"Synthetic fixture"}`)
	}))
	t.Cleanup(server.Close)
	cfg.Scrapers.Override("libredmm").BaseURL = server.URL
	cfg.Database.DSN = filepath.Join(root, "batch.db")
	cfg.Matching.Extensions = []string{".mp4"}
	cfgPath := filepath.Join(root, "config.yaml")
	require.NoError(t, config.Save(cfg, cfgPath))

	var summaries []BatchCommandResult
	require.NoError(t, RunBatchCommand(t.Context(), io.Discard, BatchCommandOptions{
		ConfigFile:  cfgPath,
		SourcePath:  src,
		Destination: filepath.Join(root, "dest"),
		Recursive:   true,
		DryRun:      true,
		ForceUpdate: true,
		Resolved:    &workflow.ResolvedSeamStrings{},
		Presenter:   &SilentBatchCommandPresenter{},
		SummaryPrinter: func(_ io.Writer, _ BatchCommandOptions, result BatchCommandResult) {
			summaries = append(summaries, result)
		},
	}))
	require.Len(t, summaries, 1)
	summary := summaries[0]
	require.Equal(t, 3, summary.MatchedCount)
	require.Len(t, summary.UniqueIDs, 2)
	require.Len(t, summary.Movies, 1)
	require.Contains(t, summary.Movies, "GOOD-700")
	require.Equal(t, 2, summary.SuccessCount)
	require.Equal(t, 1, summary.FailedCount)
	require.Zero(t, summary.ApplyFailedCount)
	require.Positive(t, misses.Load(), "the failed row must come from a scraper miss")
}
