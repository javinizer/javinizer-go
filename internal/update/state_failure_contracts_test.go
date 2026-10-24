package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type cacheFailureChecker struct{ calls int }

func (c *cacheFailureChecker) CheckLatestVersion(context.Context) (*versionInfo, error) {
	c.calls++
	return nil, os.ErrPermission
}

func TestStatusBlockedCacheParentReturnsErrorStateWithoutChecking(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(parent, []byte("keep"), 0600))
	// GetStatus must remain a cache-only read on storage failure.
	checker := &cacheFailureChecker{}
	service := NewServiceWithOptions(UpdateConfig{Enabled: true}, ServiceOptions{
		Checker: checker, StatePath: filepath.Join(parent, "state.json"),
	})
	state, err := service.GetStatus(context.Background())
	require.NoError(t, err)
	require.Zero(t, checker.calls)
	require.NotNil(t, state)
	require.Equal(t, UpdateSourceError, state.Source)
	require.NotEmpty(t, state.Error)
	require.Empty(t, state.Version)
	require.False(t, state.Available)
	contents, err := os.ReadFile(parent)
	require.NoError(t, err)
	require.Equal(t, "keep", string(contents))
}

func TestStateWriteFailureRetainsDiskAndCachedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newStateStore(path, defaultCheckInterval)
	original := &updateState{Version: "v1.2.3", Source: UpdateSourceCached, ETag: "previous-tag"}
	require.NoError(t, store.SaveState(original))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	// A directory left at the temporary publication name must reject writes,
	// rather than replacing the previous cache or the directory's contents.
	require.NoError(t, os.Mkdir(path+".tmp", 0700))
	sentinel := filepath.Join(path+".tmp", "sentinel")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0600))
	err = store.SaveState(&updateState{Version: "v9.0.0", Source: UpdateSourceFresh})
	require.Error(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	cached, err := store.LoadState()
	require.NoError(t, err)
	require.Equal(t, original, cached)
	contents, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, "keep", string(contents))
}
