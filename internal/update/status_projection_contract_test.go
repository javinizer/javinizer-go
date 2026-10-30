package update_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/stretchr/testify/require"
)

// This successful, public Checker implementation makes any accidental refresh
// observable without injecting an error or allowing a real network request.
type statusProjectionChecker struct{ calls atomic.Int32 }

func (c *statusProjectionChecker) CheckLatestVersion(context.Context) (*update.VersionInfo, error) {
	c.calls.Add(1)
	return &update.VersionInfo{Version: "v99.0.0", TagName: "v99.0.0"}, nil
}

func TestStatusProjectionContractCacheOnlyReads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		enabled   bool
		source    update.UpdateSource
		version   string
		available bool
		loadError bool
	}{
		{name: "disabled ignores malformed cache", source: update.UpdateSourceDisabled},
		{name: "missing", enabled: true, source: update.UpdateSourceNone},
		{name: "fresh available cache", enabled: true, source: update.UpdateSourceCached, version: "v8.4.2", available: true},
		{name: "fresh unavailable cache", enabled: true, source: update.UpdateSourceCached, version: "v8.4.2"},
		{name: "malformed JSON", enabled: true, source: update.UpdateSourceError, loadError: true},
		{name: "blocked cache parent", enabled: true, source: update.UpdateSourceError, loadError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "cache", "state.json")
			preservedPath := path
			var original []byte
			switch tc.name {
			case "missing":
				// Leave both the cache and its parent absent.
			case "blocked cache parent":
				preservedPath = filepath.Dir(path)
				original = []byte("owned regular file prevents a cache directory")
				require.NoError(t, os.WriteFile(preservedPath, original, 0600))
			default:
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
				original = []byte(`{"version":`)
				if tc.source == update.UpdateSourceCached {
					// A future timestamp guarantees these read-only projection
					// calls cannot legitimately schedule a stale-cache refresh.
					var err error
					original, err = json.Marshal(map[string]any{
						"version": tc.version, "available": tc.available,
						"source": "cached", "checked_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
						"etag": "preserve-this-tag", "no_stable_latest": true,
					})
					require.NoError(t, err)
				}
				require.NoError(t, os.WriteFile(path, original, 0600))
			}

			// Use a fresh service for each projection so a preceding GetStatus
			// call cannot hide the actual on-disk loading path of either wrapper.
			for _, projection := range []string{"status", "available", "version"} {
				t.Run(projection, func(t *testing.T) {
					checker := &statusProjectionChecker{}
					service := update.NewServiceWithOptions(update.UpdateConfig{
						Enabled: tc.enabled, VersionCheckIntervalHours: 24,
					}, update.ServiceOptions{Checker: checker, StatePath: path})
					t.Cleanup(service.Wait)
					ctx := context.Background()
					switch projection {
					case "available":
						got, err := service.IsUpdateAvailable(ctx)
						require.NoError(t, err, "cache failures are represented by state, not projection errors")
						require.Equal(t, tc.available, got)
					case "version":
						got, err := service.GetLatestVersion(ctx)
						require.NoError(t, err, "cache failures are represented by state, not projection errors")
						require.Equal(t, tc.version, got)
					}
					state, err := service.GetStatus(ctx)
					require.NoError(t, err)
					require.NotNil(t, state)
					require.Equal(t, tc.source, state.Source)
					require.Equal(t, tc.available, state.Available)
					require.Equal(t, tc.version, state.Version)
					if tc.loadError {
						require.Empty(t, state.CheckedAt)
						require.NotEmpty(t, state.Error)
						if tc.name == "malformed JSON" {
							require.Equal(t, "unexpected end of JSON input", state.Error)
						} else {
							require.Contains(t, state.Error, preservedPath)
						}
					} else {
						require.Empty(t, state.Error)
					}
					service.Wait()
					require.Zero(t, checker.calls.Load(), "status reads must not refresh absent, failed, disabled, or fresh caches")
					if tc.name == "missing" {
						_, err := os.Stat(path)
						require.True(t, os.IsNotExist(err), "missing state must remain absent: %v", err)
					} else {
						after, err := os.ReadFile(preservedPath)
						require.NoError(t, err)
						require.Equal(t, original, after, "status projections must preserve owned cache/obstruction bytes")
					}
				})
			}
		})
	}
}
