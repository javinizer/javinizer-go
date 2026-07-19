package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failFileFs struct {
	afero.Fs
	writeErr error
	syncErr  error
	closeErr error
}

func (f *failFileFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &failFile{File: file, writeErr: f.writeErr, syncErr: f.syncErr, closeErr: f.closeErr}, nil
}

type failFile struct {
	afero.File
	writeErr, syncErr, closeErr error
}

func (f *failFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.File.Write(p)
}

func (f *failFile) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.File.Sync()
}

func (f *failFile) Close() error {
	if f.closeErr != nil {
		return f.closeErr
	}
	return f.File.Close()
}

type statErrFs struct {
	afero.Fs
	statErr error
}

func (f *statErrFs) Stat(name string) (os.FileInfo, error) {
	return nil, f.statErr
}

type openFileErrFs struct {
	afero.Fs
	err error
}

func (f *openFileErrFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	return nil, f.err
}

func TestCovSave_AcquireLockError(t *testing.T) {
	cs := NewConfigStorage(afero.NewMemMapFs(), errLockFactory)
	err := cs.Save(DefaultConfig(nil, nil), "/cfg/c.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock boom")
}

func TestCovSave_AtomicReplaceError(t *testing.T) {
	base := afero.NewMemMapFs()
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: -1, renameFailAfter: 0}, noopLockFactory)
	cfg := DefaultConfig(nil, nil)
	cfg.Server.Port = 19090
	err := cs.Save(cfg, "/cfg/c.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write config file")
}

func TestCovSave_ReadErrorNonNotExistFallback(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/cfg", DirPerm))
	cs := NewConfigStorage(&errFs{Fs: base, openErr: errors.New("read boom")}, noopLockFactory)
	cfg := DefaultConfig(nil, nil)
	cfg.Server.Port = 17777
	err := cs.Save(cfg, "/cfg/c.yaml")
	require.NoError(t, err)
	got, err := afero.ReadFile(base, "/cfg/c.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(got), "17777")
}

func TestCovAtomicReplace_OpenFileError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/cfg", DirPerm))
	cs := NewConfigStorage(&openFileErrFs{Fs: base, err: errors.New("openfile boom")}, noopLockFactory)
	err := cs.atomicReplace("/cfg/c.yaml", []byte("x"), 0o600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temp config file")
}

func TestCovAtomicReplace_WriteError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/cfg", DirPerm))
	cs := NewConfigStorage(&failFileFs{Fs: base, writeErr: errors.New("write boom")}, noopLockFactory)
	err := cs.atomicReplace("/cfg/c.yaml", []byte("x"), 0o600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write temp config file")
}

func TestCovAtomicReplace_SyncError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/cfg", DirPerm))
	cs := NewConfigStorage(&failFileFs{Fs: base, syncErr: errors.New("sync boom")}, noopLockFactory)
	err := cs.atomicReplace("/cfg/c.yaml", []byte("x"), 0o600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to sync temp config file")
}

func TestCovAtomicReplace_RenameError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/cfg", DirPerm))
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: -1, renameFailAfter: 0}, noopLockFactory)
	err := cs.atomicReplace("/cfg/c.yaml", []byte("x"), 0o600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to atomically replace config file")
}

func TestCovReplaceFileOnWindows_BackupRenameError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/dest.yaml", []byte("old"), 0o644))
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: -1, renameFailAfter: 0}, noopLockFactory)
	err := cs.replaceFileOnWindows("/dest.yaml", "/tmp.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create backup")
}

func TestCovReplaceFileOnWindows_RestoreError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/dest.yaml", []byte("old"), 0o644))
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: -1, renameFailAfter: 1}, noopLockFactory)
	err := cs.replaceFileOnWindows("/dest.yaml", "/missing-tmp.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rollback failed")
}

func TestCovReplaceFileOnWindows_StatError(t *testing.T) {
	base := afero.NewMemMapFs()
	cs := NewConfigStorage(&statErrFs{Fs: base, statErr: errors.New("stat boom")}, noopLockFactory)
	err := cs.replaceFileOnWindows("/dest.yaml", "/tmp.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat destination")
}

func TestCovUpdate_LockAcquireError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix directory permissions")
	}
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := Update(filepath.Join(dir, "c.yaml"), func(c *Config) { c.Server.Port = 1 })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire config lock")
}

func TestCovUpdate_LoadLockedError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	err := Update(path, func(c *Config) { c.Server.Port = 1 })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestCovLoadLocked_ReadError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	_, err := loadLocked(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestCovLoadLocked_MissingFileReturnsDefaults(t *testing.T) {
	cfg, err := loadLocked(filepath.Join(t.TempDir(), "missing.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
}

func TestCovWriteLocked_MalformedExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server: ["), 0o644))
	cfg := DefaultConfig(nil, nil)
	cfg.Server.Port = 12345
	require.NoError(t, writeLocked(path, cfg))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "12345")
}

func TestCovWriteLocked_UnreadableExisting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 1\n"), 0o644))
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	cfg := DefaultConfig(nil, nil)
	err := writeLocked(path, cfg)
	require.NoError(t, err)
}

func TestCovWriteLocked_AtomicReplaceError(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	err := writeLocked(filepath.Join(t.TempDir(), "nodir", "c.yaml"), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write config file")
}

func TestCovWriteLocked_NoOpWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	cfg := DefaultConfig(nil, nil)
	cfg.Server.Port = 2222
	require.NoError(t, writeLocked(path, cfg))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, writeLocked(path, cfg))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

func TestCovLoadOrCreate_StatError(t *testing.T) {
	cs := NewConfigStorage(&statErrFs{Fs: afero.NewMemMapFs(), statErr: errors.New("stat boom")}, noopLockFactory)
	_, err := cs.LoadOrCreate("/cfg/c.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat config file")
}

func TestCovLoadOrCreate_MigrationSaveError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/cfg/c.yaml", []byte("server:\n  port: 8088\n"), 0o644))
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: -1, renameFailAfter: 0}, noopLockFactory)
	_, err := cs.LoadOrCreate("/cfg/c.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save migrated config")
}

func TestCovLoadOrCreate_MigrationReloadError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/cfg/c.yaml", []byte("server:\n  port: 8088\n"), 0o644))
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: 2, renameFailAfter: -1}, noopLockFactory)
	_, err := cs.LoadOrCreate("/cfg/c.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to re-load migrated config")
}

func TestCovSyncDir_SyncError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires /dev/null")
	}
	if _, err := os.Stat("/dev/null"); err != nil {
		t.Skip("/dev/null unavailable")
	}
	err := syncDir("/dev/null")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to sync directory")
}

func TestCovParseConfigLockMetadata_EmptyPart(t *testing.T) {
	pid, ts, ok := parseConfigLockMetadata("pid=123,,time=456")
	require.True(t, ok)
	assert.Equal(t, 123, pid)
	assert.Equal(t, int64(456), ts)

	_, _, ok = parseConfigLockMetadata("=,=")
	assert.False(t, ok)
}
