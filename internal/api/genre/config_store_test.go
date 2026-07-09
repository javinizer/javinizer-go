package genre

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfig() *config.Config {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.IgnoreGenres = []string{"Sample", "Trailer"}
	cfg.WebUI.Favorites.Genre = []string{"Drama", "Action"}
	return cfg
}

func newRuntimeStore(t *testing.T) (*RuntimeGenreConfigStore, *core.APIRuntime, string) {
	t.Helper()
	rt := core.NewAPIRuntime(&core.APIDeps{})
	rt.SetConfig(newTestConfig())
	rt.EnsureRuntime()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	return NewRuntimeGenreConfigStore(rt, configFile), rt, configFile
}

func TestRuntimeGenreConfigStore_GetIgnoreGenres(t *testing.T) {
	store, _, _ := newRuntimeStore(t)
	got, err := store.GetIgnoreGenres(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"Sample", "Trailer"}, got)
}

func TestRuntimeGenreConfigStore_GetFavoriteGenres(t *testing.T) {
	store, _, _ := newRuntimeStore(t)
	got, err := store.GetFavoriteGenres(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"Drama", "Action"}, got)
}

func TestRuntimeGenreConfigStore_SetIgnoreGenres_PersistsAndPublishes(t *testing.T) {
	store, rt, configFile := newRuntimeStore(t)
	require.NoError(t, store.SetIgnoreGenres(context.Background(), []string{"VR", "HD"}))

	assert.Equal(t, []string{"VR", "HD"}, rt.Deps().CoreDeps.GetConfig().Metadata.IgnoreGenres)
	_, err := os.Stat(configFile)
	require.NoError(t, err, "config file should be written to disk")

	got, err := store.GetIgnoreGenres(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"VR", "HD"}, got)
}

func TestRuntimeGenreConfigStore_SetFavoriteGenres_PersistsAndPublishes(t *testing.T) {
	store, rt, configFile := newRuntimeStore(t)
	require.NoError(t, store.SetFavoriteGenres(context.Background(), []string{"Comedy"}))

	assert.Equal(t, []string{"Comedy"}, rt.Deps().CoreDeps.GetConfig().WebUI.Favorites.Genre)
	_, err := os.Stat(configFile)
	require.NoError(t, err, "config file should be written to disk")
}

func TestRuntimeGenreConfigStore_Persist_SaveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Rename over a directory may succeed on Windows")
	}
	rt := core.NewAPIRuntime(&core.APIDeps{})
	rt.SetConfig(newTestConfig())
	rt.EnsureRuntime()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.MkdirAll(configPath, 0o755))

	store := NewRuntimeGenreConfigStore(rt, configPath)
	err := store.SetIgnoreGenres(context.Background(), []string{"VR"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save genre config")
}

func TestRuntimeGenreConfigStore_NilRuntime_GetErrors(t *testing.T) {
	store := NewRuntimeGenreConfigStore(nil, "config.yaml")

	_, err := store.GetIgnoreGenres(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime is not initialized")

	_, err = store.GetFavoriteGenres(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime is not initialized")

	err = store.SetIgnoreGenres(context.Background(), []string{"VR"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime is not initialized")

	err = store.SetFavoriteGenres(context.Background(), []string{"VR"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime is not initialized")
}

func TestRuntimeGenreConfigStore_NilRuntimeState_GetErrors(t *testing.T) {
	rt := core.NewAPIRuntime(&core.APIDeps{})
	rt.SetConfig(newTestConfig())
	store := NewRuntimeGenreConfigStore(rt, "config.yaml")

	_, err := store.GetIgnoreGenres(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime state is not initialized")

	_, err = store.GetFavoriteGenres(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime state is not initialized")

	err = store.SetIgnoreGenres(context.Background(), []string{"VR"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime state is not initialized")
}

func TestCloneStrings(t *testing.T) {
	assert.Equal(t, []string{}, cloneStrings(nil))
	assert.Equal(t, []string{"a"}, cloneStrings([]string{"a"}))

	src := []string{"a", "b"}
	cp := cloneStrings(src)
	cp[0] = "z"
	assert.Equal(t, "a", src[0], "cloneStrings should return an independent copy")
}

func TestNoopGenreConfigStore(t *testing.T) {
	s := noopGenreConfigStore{}

	got, err := s.GetIgnoreGenres(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = s.GetFavoriteGenres(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)

	err = s.SetIgnoreGenres(context.Background(), []string{"VR"})
	require.Error(t, err)

	err = s.SetFavoriteGenres(context.Background(), []string{"VR"})
	require.Error(t, err)
}

func TestRuntimeGenreConfigStore_AddIgnoreGenre_NewAndIdempotent(t *testing.T) {
	store, rt, _ := newRuntimeStore(t)

	result, changed, err := store.AddIgnoreGenre(context.Background(), "VR")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []string{"Sample", "Trailer", "VR"}, result)
	assert.Equal(t, result, rt.Deps().CoreDeps.GetConfig().Metadata.IgnoreGenres)

	result, changed, err = store.AddIgnoreGenre(context.Background(), "VR")
	require.NoError(t, err)
	assert.False(t, changed, "adding an existing genre must be idempotent")
	assert.Equal(t, []string{"Sample", "Trailer", "VR"}, result)
}

func TestRuntimeGenreConfigStore_RemoveIgnoreGenre_RemovedAndAbsent(t *testing.T) {
	store, rt, _ := newRuntimeStore(t)

	result, changed, err := store.RemoveIgnoreGenre(context.Background(), "Trailer")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []string{"Sample"}, result)
	assert.Equal(t, result, rt.Deps().CoreDeps.GetConfig().Metadata.IgnoreGenres)

	result, changed, err = store.RemoveIgnoreGenre(context.Background(), "Missing")
	require.NoError(t, err)
	assert.False(t, changed, "removing an absent genre must report no change")
	assert.Equal(t, []string{"Sample"}, result)
}

func TestRuntimeGenreConfigStore_AddFavoriteGenre_NewAndIdempotent(t *testing.T) {
	store, rt, _ := newRuntimeStore(t)

	result, changed, err := store.AddFavoriteGenre(context.Background(), "Comedy")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []string{"Drama", "Action", "Comedy"}, result)
	assert.Equal(t, result, rt.Deps().CoreDeps.GetConfig().WebUI.Favorites.Genre)

	result, changed, err = store.AddFavoriteGenre(context.Background(), "Drama")
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, []string{"Drama", "Action", "Comedy"}, result)
}

func TestRuntimeGenreConfigStore_RemoveFavoriteGenre_RemovedAndAbsent(t *testing.T) {
	store, rt, _ := newRuntimeStore(t)

	result, changed, err := store.RemoveFavoriteGenre(context.Background(), "Action")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []string{"Drama"}, result)
	assert.Equal(t, result, rt.Deps().CoreDeps.GetConfig().WebUI.Favorites.Genre)

	result, changed, err = store.RemoveFavoriteGenre(context.Background(), "Missing")
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, []string{"Drama"}, result)
}

func TestRuntimeGenreConfigStore_AddIgnoreGenre_NilRuntime(t *testing.T) {
	store := NewRuntimeGenreConfigStore(nil, "config.yaml")
	_, _, err := store.AddIgnoreGenre(context.Background(), "VR")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime is not initialized")
}

func TestRuntimeGenreConfigStore_RemoveIgnoreGenre_NilRuntimeState(t *testing.T) {
	rt := core.NewAPIRuntime(&core.APIDeps{})
	rt.SetConfig(newTestConfig())
	store := NewRuntimeGenreConfigStore(rt, "config.yaml")
	_, _, err := store.RemoveIgnoreGenre(context.Background(), "VR")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime state is not initialized")
}

func TestRuntimeGenreConfigStore_AddFavoriteGenre_SaveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Rename over a directory may succeed on Windows")
	}
	rt := core.NewAPIRuntime(&core.APIDeps{})
	rt.SetConfig(newTestConfig())
	rt.EnsureRuntime()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.MkdirAll(configPath, 0o755))

	store := NewRuntimeGenreConfigStore(rt, configPath)
	_, _, err := store.AddFavoriteGenre(context.Background(), "VR")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save genre config")
}

func TestRuntimeGenreConfigStore_RemoveFavoriteGenre_NilRuntime(t *testing.T) {
	store := NewRuntimeGenreConfigStore(nil, "config.yaml")
	_, _, err := store.RemoveFavoriteGenre(context.Background(), "VR")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime is not initialized")
}

// TestRuntimeGenreConfigStore_ConcurrentAddRemove verifies that concurrent
// POST/DELETE-style mutations do not lose updates: every successful add lands
// in the persisted list and every remove takes effect. Run under -race to
// confirm the ConfigUpdateMu serialization is correct.
func TestRuntimeGenreConfigStore_ConcurrentAddRemove(t *testing.T) {
	store, rt, _ := newRuntimeStore(t)
	// Start from an empty list so additions are deterministic.
	require.NoError(t, store.SetIgnoreGenres(context.Background(), nil))

	const adders = 8
	const perAdder = 25
	var wg sync.WaitGroup
	for i := 0; i < adders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < perAdder; j++ {
				genre := fmt.Sprintf("g-%d-%d", i, j)
				_, _, err := store.AddIgnoreGenre(context.Background(), genre)
				if err != nil {
					t.Errorf("add %s: %v", genre, err)
				}
			}
		}(i)
	}

	// Concurrent removals of genres known not to exist should be no-ops.
	for i := 0; i < adders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perAdder; j++ {
				_, _, _ = store.RemoveIgnoreGenre(context.Background(), "nonexistent")
			}
		}()
	}
	wg.Wait()

	got, err := store.GetIgnoreGenres(context.Background())
	require.NoError(t, err)
	require.Len(t, got, adders*perAdder, "every concurrent add must survive — no lost updates")

	// Final state is observable through the live config too.
	live := rt.Deps().CoreDeps.GetConfig().Metadata.IgnoreGenres
	assert.Len(t, live, adders*perAdder)
}

func TestNoopGenreConfigStore_AddRemove(t *testing.T) {
	s := noopGenreConfigStore{}

	_, _, err := s.AddIgnoreGenre(context.Background(), "VR")
	require.ErrorIs(t, err, ErrGenreConfigStoreNotConfigured)

	_, _, err = s.RemoveIgnoreGenre(context.Background(), "VR")
	require.ErrorIs(t, err, ErrGenreConfigStoreNotConfigured)

	_, _, err = s.AddFavoriteGenre(context.Background(), "VR")
	require.ErrorIs(t, err, ErrGenreConfigStoreNotConfigured)

	_, _, err = s.RemoveFavoriteGenre(context.Background(), "VR")
	require.ErrorIs(t, err, ErrGenreConfigStoreNotConfigured)
}
