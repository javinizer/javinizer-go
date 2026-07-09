package genre

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
