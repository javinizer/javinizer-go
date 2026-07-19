package config

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopLockFactory(string) (func(), error) {
	return func() {}, nil
}

func newMemStorage() *ConfigStorage {
	return NewConfigStorage(afero.NewMemMapFs(), noopLockFactory)
}

func TestBuildSparseSaveContextWithNames_EmptyNames(t *testing.T) {
	ctx, err := BuildSparseSaveContextWithNames(nil)
	require.NoError(t, err)
	require.NotNil(t, ctx.Defaults)
	require.NotNil(t, ctx.Schema)
	assert.Equal(t, 0, len(ctx.KnownScraperNames))
}

func TestBuildSparseSaveContextWithNames_StaticKeyCollisionReturnsError(t *testing.T) {
	_, err := BuildSparseSaveContextWithNames([]string{"user_agent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with static config key")

	_, err = BuildSparseSaveContextWithNames([]string{"priority"})
	require.Error(t, err)
}

func TestBuildSparseSaveContextWithNames_ValidNames(t *testing.T) {
	ctx, err := BuildSparseSaveContextWithNames([]string{"dmm", "r18dev"})
	require.NoError(t, err)
	assert.True(t, ctx.KnownScraperNames["dmm"])
	assert.True(t, ctx.KnownScraperNames["r18dev"])
}

func TestBuildSparseSaveContext_PanicsNever(t *testing.T) {
	ctx := BuildSparseSaveContext()
	require.NotNil(t, ctx.Defaults)
	require.NotNil(t, ctx.Schema)
}

func TestSaveSparse_PackageLevel_TempFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "debug"

	require.NoError(t, SaveSparse(cfg, path, BuildSparseSaveContext()))

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "debug", loaded.Logging.Level)
}

func TestSaveSparse_Method_MissingFileCreatesNew(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "info"

	require.NoError(t, cs.SaveSparse(cfg, path, BuildSparseSaveContext()))

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "info", loaded.Logging.Level)
}

func TestSaveSparse_Method_UnchangedBytesIsNoOp(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "info"

	ctx := BuildSparseSaveContext()
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	dataBefore, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)

	// Save again with no changes.
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	dataAfter, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.Equal(t, dataBefore, dataAfter)
}

func TestSaveSparse_Method_MalformedExistingYAMLReturnsError(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	// Tab-indented YAML is a parse error in yaml.v3.
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte("foo: bar\n\tbad: tab\n"), 0o644))

	cfg := DefaultConfig(nil, nil)
	err := cs.SaveSparse(cfg, path, BuildSparseSaveContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse existing config")
}

func TestSaveSparse_Method_ExistingNonMappingRootReturnsError(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte("- just\n- a\n- list\n"), 0o644))

	cfg := DefaultConfig(nil, nil)
	err := cs.SaveSparse(cfg, path, BuildSparseSaveContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a YAML mapping")
}

type errFs struct {
	afero.Fs
	openErr  error
	mkdirErr error
}

func (f *errFs) Open(name string) (afero.File, error) {
	return nil, f.openErr
}

func (f *errFs) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	return f.Fs.MkdirAll(path, perm)
}

func TestSaveSparse_Method_ReadErrorReturnsError(t *testing.T) {
	base := afero.NewMemMapFs()
	cs := NewConfigStorage(&errFs{Fs: base, openErr: errors.New("boom")}, noopLockFactory)
	cfg := DefaultConfig(nil, nil)
	err := cs.SaveSparse(cfg, "/cfg/config.yaml", BuildSparseSaveContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config")
}

func TestSaveSparse_Method_MkdirAllErrorReturnsError(t *testing.T) {
	base := afero.NewMemMapFs()
	cs := NewConfigStorage(&errFs{Fs: base, mkdirErr: errors.New("mkdir boom")}, noopLockFactory)
	cfg := DefaultConfig(nil, nil)
	err := cs.SaveSparse(cfg, "/cfg/config.yaml", BuildSparseSaveContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create config directory")
}
