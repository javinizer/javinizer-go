package update

import (
	"context"
	"encoding/json"
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
	for _, tc := range []struct {
		name  string
		state *updateState
	}{
		{name: "replacement_state", state: &updateState{Version: "v9.0.0", Source: UpdateSourceFresh}},
		{name: "nil_state"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			store := newStateStore(path, defaultCheckInterval)
			original := &updateState{Version: "v1.2.3", Source: UpdateSourceCached, ETag: "previous-tag"}
			require.NoError(t, store.SaveState(original))
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			cachedBefore := store.GetState()
			require.Equal(t, original, cachedBefore)
			// A directory left at the temporary publication name must reject writes,
			// rather than replacing the previous cache or the directory's contents.
			require.NoError(t, os.Mkdir(path+".tmp", 0700))
			sentinel := filepath.Join(path+".tmp", "sentinel")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0600))
			err = store.SaveState(tc.state)
			require.Error(t, err)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Equal(t, cachedBefore, store.GetState())
			cached, err := store.LoadState()
			require.NoError(t, err)
			require.Equal(t, original, cached)
			contents, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep", string(contents))
			entries, err := os.ReadDir(path + ".tmp")
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.Equal(t, "sentinel", entries[0].Name())

			// Remove only the owned obstruction, then retry the same publication.
			require.NoError(t, os.Remove(sentinel))
			require.NoError(t, os.Remove(path+".tmp"))
			require.NoError(t, store.SaveState(tc.state))
			published, err := os.ReadFile(path)
			require.NoError(t, err)
			if tc.state == nil {
				// SaveState(nil) publishes null; unlike ClearState, it keeps the file.
				require.Equal(t, []byte("null"), published)
				require.Nil(t, store.GetState())
				require.True(t, store.ShouldCheck())
			} else {
				expected, err := json.Marshal(tc.state)
				require.NoError(t, err)
				require.Equal(t, expected, published)
				require.Equal(t, tc.state, store.GetState())
			}
			_, err = os.Stat(path + ".tmp")
			require.True(t, os.IsNotExist(err), "temporary publication path should be absent: %v", err)

			// A subsequent nonnil save must recover both persistence and copy semantics.
			recovered := updateState{Version: "v2.3.4", Source: UpdateSourceFresh, ETag: "recovered-tag"}
			wantRecovered := recovered
			require.NoError(t, store.SaveState(&recovered))
			recovered.Version = "caller-only-change"
			recovered.ETag = "caller-only-tag"
			require.Equal(t, &wantRecovered, store.GetState())
			published, err = os.ReadFile(path)
			require.NoError(t, err)
			expected, err := json.Marshal(&wantRecovered)
			require.NoError(t, err)
			require.Equal(t, expected, published)
			reloaded, err := newStateStore(path, defaultCheckInterval).LoadState()
			require.NoError(t, err)
			require.Equal(t, &wantRecovered, reloaded)
			_, err = os.Stat(path + ".tmp")
			require.True(t, os.IsNotExist(err), "temporary publication path should be absent after recovery: %v", err)
		})
	}
}
