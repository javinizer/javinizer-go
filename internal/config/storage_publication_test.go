package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type publicationFaultFs struct {
	afero.Fs
	stage   string
	failure error
	renamed [][2]string
	removed []string
}

func (fs *publicationFaultFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if fs.stage == "create" {
		return nil, fs.failure
	}
	f, err := fs.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &publicationFaultFile{File: f, stage: fs.stage, failure: fs.failure}, nil
}

func (fs *publicationFaultFs) Rename(old, new string) error {
	fs.renamed = append(fs.renamed, [2]string{old, new})
	if fs.stage == "rename" {
		return &os.LinkError{Op: "rename", Old: old, New: new, Err: fs.failure}
	}
	return fs.Fs.Rename(old, new)
}

func (fs *publicationFaultFs) Remove(name string) error {
	fs.removed = append(fs.removed, name)
	return fs.Fs.Remove(name)
}

type publicationFaultFile struct {
	afero.File
	stage   string
	failure error
}

func (f *publicationFaultFile) Write(data []byte) (int, error) {
	if f.stage == "write" {
		return 0, f.failure
	}
	return f.File.Write(data)
}

func (f *publicationFaultFile) Sync() error {
	if f.stage == "sync" {
		return f.failure
	}
	return f.File.Sync()
}

func (f *publicationFaultFile) Close() error {
	err := f.File.Close()
	if f.stage == "close" {
		return f.failure
	}
	return err
}

func TestAtomicReplace_FailuresPreserveCommittedConfig(t *testing.T) {
	for _, stage := range []string{"create", "write", "sync", "close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			previous := []byte(fmt.Sprintf("config_version: %d\n# committed bytes\n", CurrentConfigVersion))
			require.NoError(t, atomicReplaceFile(path, previous, FilePerm))
			before, err := publicationFileInfo(path)
			require.NoError(t, err)
			failure := errors.New("injected " + stage + " failure")
			fs := &publicationFaultFs{Fs: afero.NewOsFs(), stage: stage, failure: failure}

			err = NewConfigStorage(fs, nil).atomicReplace(path, []byte("uncommitted"), FilePerm)
			require.ErrorIs(t, err, failure)
			got, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, previous, got)
			after, statErr := publicationFileInfo(path)
			require.NoError(t, statErr)
			assert.True(t, os.SameFile(before, after), "failed publication must not move or replace the committed file")
			for _, rename := range fs.renamed {
				assert.NotEqual(t, path, rename[0], "never move the committed destination aside")
				assert.Equal(t, path, rename[1])
			}
			for _, removed := range fs.removed {
				assert.NotEqual(t, path, removed, "only the failed temporary file may be removed")
			}
			entries, readDirErr := os.ReadDir(dir)
			require.NoError(t, readDirErr)
			require.Len(t, entries, 1, "failed publication must clean its temporary file and never create a backup")
			assert.Equal(t, "config.yaml", entries[0].Name())
		})
	}
}

type pausedPublicationFs struct {
	afero.Fs
	target  string
	pauseAt int
	calls   int
	ready   chan struct{}
	resume  chan struct{}
	moved   bool
}

func (fs *pausedPublicationFs) Rename(old, new string) error {
	if old == fs.target {
		fs.moved = true
	}
	fs.calls++
	if fs.calls == fs.pauseAt {
		close(fs.ready)
		<-fs.resume
	}
	return fs.Fs.Rename(old, new)
}

func TestAtomicReplace_LeavesDestinationVisibleUntilPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old"), FilePerm))
	fs := &pausedPublicationFs{Fs: afero.NewOsFs(), target: path, pauseAt: 1, ready: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- NewConfigStorage(fs, nil).atomicReplace(path, []byte("new"), FilePerm) }()
	waitPublicationSignal(t, fs.ready)
	old, readErr := os.ReadFile(path)
	close(fs.resume)
	require.NoError(t, waitPublicationResult(t, done))
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(old))
	assert.False(t, fs.moved, "publication must never move the destination to a backup")
	assert.Equal(t, 1, fs.calls, "publish with a single direct rename")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
}

type pausedMissingStatFs struct {
	afero.Fs
	target  string
	paused  atomic.Bool
	missing chan struct{}
	resume  chan struct{}
}

func (fs *pausedMissingStatFs) Stat(name string) (os.FileInfo, error) {
	info, err := fs.Fs.Stat(name)
	if name == fs.target && os.IsNotExist(err) && fs.paused.CompareAndSwap(false, true) {
		close(fs.missing)
		<-fs.resume
	}
	return info, err
}

func TestLoadOrCreate_PreservesSaveAfterMissingStat(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	path := filepath.Join(t.TempDir(), "config.yaml")
	fs := &pausedMissingStatFs{Fs: afero.NewOsFs(), target: path, missing: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan error, 1)
	var loaded *Config
	go func() {
		var err error
		loaded, err = NewConfigStorage(fs, nil).LoadOrCreate(path)
		done <- err
	}()
	waitPublicationSignal(t, fs.missing)
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "warn"
	cfg.Server.Host = "127.0.0.1"
	_, prepareErr := Prepare(cfg)
	require.NoError(t, prepareErr)
	err := Save(cfg, path)
	saved, readErr := os.ReadFile(path)
	close(fs.resume)
	require.NoError(t, waitPublicationResult(t, done))
	require.NoError(t, err)
	require.NoError(t, readErr)
	require.NotNil(t, loaded)
	assert.Equal(t, "warn", loaded.Logging.Level)
	assert.Equal(t, "127.0.0.1", loaded.Server.Host, "init-only defaults must not apply to another writer's config")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, saved, got, "the successful Save must survive byte for byte")
}

func TestLoadOrCreate_HoldsLockThroughInitDefaults(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	path := filepath.Join(t.TempDir(), "config.yaml")
	fs := &pausedPublicationFs{Fs: afero.NewOsFs(), target: path, pauseAt: 2, ready: make(chan struct{}), resume: make(chan struct{})}
	var mu sync.Mutex
	var locks atomic.Int32
	contended := make(chan struct{})
	factory := func(string) (func(), error) {
		if locks.Add(1) == 2 {
			close(contended)
		}
		mu.Lock()
		return mu.Unlock, nil
	}
	created := make(chan error, 1)
	go func() { _, err := NewConfigStorage(fs, factory).LoadOrCreate(path); created <- err }()
	waitPublicationSignal(t, fs.ready)
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "warn"
	cfg.Server.Host = "127.0.0.1"
	saved := make(chan error, 1)
	go func() { saved <- NewConfigStorage(afero.NewOsFs(), factory).Save(cfg, path) }()
	waitPublicationSignal(t, contended)
	select {
	case err := <-saved:
		close(fs.resume)
		_ = waitPublicationResult(t, created)
		t.Fatalf("Save completed before initialization released the lock: %v", err)
	default:
	}
	close(fs.resume)
	require.NoError(t, waitPublicationResult(t, created))
	require.NoError(t, waitPublicationResult(t, saved))
	assert.EqualValues(t, 2, locks.Load(), "initialization must not recursively acquire the sparse-save lock")
	got, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "warn", got.Logging.Level)
	assert.Equal(t, "127.0.0.1", got.Server.Host)
}

func TestLoadOrCreate_ConcurrentInitializersPreserveWinner(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	path := filepath.Join(t.TempDir(), "config.yaml")
	fs := &pausedPublicationFs{Fs: afero.NewOsFs(), target: path, pauseAt: 1, ready: make(chan struct{}), resume: make(chan struct{})}
	first := make(chan error, 1)
	go func() { _, err := NewConfigStorage(fs, nil).LoadOrCreate(path); first <- err }()
	waitPublicationSignal(t, fs.ready)
	secondFs := &pausedMissingStatFs{Fs: afero.NewOsFs(), target: path, missing: make(chan struct{}), resume: make(chan struct{})}
	second := make(chan error, 1)
	go func() { _, err := NewConfigStorage(secondFs, nil).LoadOrCreate(path); second <- err }()
	waitPublicationSignal(t, secondFs.missing)
	close(fs.resume)
	require.NoError(t, waitPublicationResult(t, first))
	// Existing-config preparation may normalize the minimal stub. Establish that
	// normal form before checking that the losing initializer leaves it intact.
	_, err := LoadOrCreate(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "127.0.0.2")
	committed, err := os.ReadFile(path)
	require.NoError(t, err)
	before, err := publicationFileInfo(path)
	require.NoError(t, err)
	close(secondFs.resume)
	require.NoError(t, waitPublicationResult(t, second))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, committed, got)
	after, err := publicationFileInfo(path)
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "the losing initializer must not publish again")
	assert.Equal(t, before.ModTime(), after.ModTime())
	assert.Equal(t, 3, fs.calls, "winner stages the stub and init defaults, then publishes once")
}

func TestLoadOrCreate_InitialConfigIsPublishedOnlyWhenComplete(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	path := filepath.Join(t.TempDir(), "config.yaml")
	fs := &pausedPublicationFs{Fs: afero.NewOsFs(), target: path, pauseAt: 2, ready: make(chan struct{}), resume: make(chan struct{})}
	var mu sync.Mutex
	var locks atomic.Int32
	contended := make(chan struct{})
	factory := func(string) (func(), error) {
		if locks.Add(1) == 2 {
			close(contended)
		}
		mu.Lock()
		return mu.Unlock, nil
	}
	first := make(chan error, 1)
	go func() { _, err := NewConfigStorage(fs, factory).LoadOrCreate(path); first <- err }()
	waitPublicationSignal(t, fs.ready)
	_, partialReadErr := os.ReadFile(path)
	second := make(chan error, 1)
	go func() { _, err := NewConfigStorage(afero.NewOsFs(), factory).LoadOrCreate(path); second <- err }()
	waitPublicationSignal(t, contended)
	close(fs.resume)
	require.NoError(t, waitPublicationResult(t, first))
	require.NoError(t, waitPublicationResult(t, second))
	assert.ErrorIs(t, partialReadErr, os.ErrNotExist, "a partial init stub must never become the canonical config")
	got, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", got.Server.Host, "a concurrent initializer must not normalize a partial snapshot over the winner")
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "successful initialization must leave no private staging files")
	assert.Equal(t, "config.yaml", entries[0].Name())
}

func TestCreateFromEmbedded_StageFailuresLeaveNoPublishedConfig(t *testing.T) {
	for _, stage := range []string{"create", "close"} {
		t.Run(stage, func(t *testing.T) {
			base := afero.NewMemMapFs()
			failure := errors.New("injected staging " + stage + " failure")
			fs := &publicationFaultFs{Fs: base, stage: stage, failure: failure}
			_, err := NewConfigStorage(fs, noopLockFactory).LoadOrCreate("/config.yaml")
			require.ErrorIs(t, err, failure)
			assert.Contains(t, err.Error(), "failed to save default config")
			entries, err := afero.ReadDir(base, "/")
			require.NoError(t, err)
			assert.Empty(t, entries, "failed staging must clean its reserved private name")
		})
	}
}

func TestCreateFromEmbedded_PublicationFailuresCleanPrivateStage(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	for _, allowedRenames := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("after-%d-renames", allowedRenames), func(t *testing.T) {
			base := afero.NewMemMapFs()
			fs := &countingFs{Fs: base, openFailAfter: -1, renameFailAfter: allowedRenames}
			_, err := NewConfigStorage(fs, noopLockFactory).LoadOrCreate("/config.yaml")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "rename boom")
			entries, err := afero.ReadDir(base, "/")
			require.NoError(t, err)
			assert.Empty(t, entries, "no incomplete canonical config or owned staging/temp file may remain")
		})
	}
}

func TestCreateFromEmbedded_LockFailureDoesNotPublish(t *testing.T) {
	base := afero.NewMemMapFs()
	failure := errors.New("injected lock failure")
	cs := NewConfigStorage(base, func(string) (func(), error) { return nil, failure })
	_, err := cs.LoadOrCreate("/config.yaml")
	require.ErrorIs(t, err, failure)
	_, statErr := base.Stat("/config.yaml")
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestCreateFromEmbedded_RecheckErrorReleasesLock(t *testing.T) {
	failure := errors.New("injected recheck failure")
	locked := false
	cs := NewConfigStorage(&statErrFs{Fs: afero.NewMemMapFs(), statErr: failure}, func(string) (func(), error) {
		locked = true
		return func() { locked = false }, nil
	})
	_, err := cs.createFromEmbedded("/config.yaml")
	require.ErrorIs(t, err, failure)
	assert.Contains(t, err.Error(), "failed to stat config file")
	assert.False(t, locked)
}

func TestCreateFromEmbedded_ExistingConfigStillPrepared(t *testing.T) {
	base := afero.NewMemMapFs()
	path := "/config.yaml"
	original := fmt.Sprintf("config_version: %d\ndatabase:\n  type: ' SQLITE '\n", CurrentConfigVersion)
	require.NoError(t, afero.WriteFile(base, path, []byte(original), FilePerm))
	var locked bool
	cs := NewConfigStorage(base, func(string) (func(), error) {
		if locked {
			return nil, errors.New("recursive lock")
		}
		locked = true
		return func() { locked = false }, nil
	})
	cfg, err := cs.createFromEmbedded(path)
	require.NoError(t, err)
	assert.Equal(t, "sqlite", cfg.Database.Type)
	assert.False(t, locked)
	got, err := afero.ReadFile(base, path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "SQLITE")
	assert.Contains(t, string(got), "operation_mode: organize")
}

func TestLoadOrCreate_ReadOnlyExistingConfig(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("requires Unix permission enforcement")
	}
	path := filepath.Join(t.TempDir(), "config # 日本語.yaml")
	cfg := DefaultConfig(nil, nil)
	_, err := Prepare(cfg)
	require.NoError(t, err)
	require.NoError(t, Save(cfg, path))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	dir := filepath.Dir(path)
	require.NoError(t, os.Chmod(path, 0o400))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700); _ = os.Chmod(path, FilePerm) })
	got, err := LoadOrCreate(path)
	require.NoError(t, err, "loading an unchanged config must not require a writable directory")
	assert.Equal(t, cfg.Server.Host, got.Server.Host)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestLoadOrCreate_BasePathFilesystem(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	dir := t.TempDir()
	cs := NewConfigStorage(afero.NewBasePathFs(afero.NewOsFs(), dir), noopLockFactory)
	name := "config # 日本語.yaml"
	cfg, err := cs.LoadOrCreate(name)
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	got, err := Load(filepath.Join(dir, name))
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", got.Server.Host)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, name, entries[0].Name())
}

func TestLoadOrCreate_LongConfigBasename(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	// Use a relative filesystem path backed by native OS files to exercise
	// private staging without expanding the long canonical filename component.
	dir := t.TempDir()
	name := strings.Repeat("c", 210) + ".yaml"
	cs := NewConfigStorage(afero.NewBasePathFs(afero.NewOsFs(), dir), noopLockFactory)
	cfg, err := cs.LoadOrCreate(name)
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	got, err := cs.Load(name)
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", got.Server.Host)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, name, entries[0].Name())
}

type reusedInitializationStageFs struct {
	afero.Fs
	target string
	reused string
}

func (fs *reusedInitializationStageFs) Rename(old, new string) error {
	if err := fs.Fs.Rename(old, new); err != nil {
		return err
	}
	if new == fs.target {
		fs.reused = old
		return afero.WriteFile(fs.Fs, old, []byte("new owner"), FilePerm)
	}
	return nil
}

func TestCreateFromEmbedded_DoesNotRemoveReusedStageName(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &reusedInitializationStageFs{Fs: base, target: "/config.yaml"}
	_, err := NewConfigStorage(fs, noopLockFactory).LoadOrCreate(fs.target)
	require.NoError(t, err)
	require.NotEmpty(t, fs.reused)
	got, err := afero.ReadFile(base, fs.reused)
	require.NoError(t, err)
	assert.Equal(t, "new owner", string(got), "cleanup must stop when the owned stage has been published")
	_, err = base.Stat(fs.target)
	require.NoError(t, err)
}

func waitPublicationSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the scheduled filesystem operation")
	}
}

func waitPublicationResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for config publication (possible recursive lock)")
		return nil
	}
}

// File.Stat captures Windows file identity immediately; os.Stat may defer that
// lookup until SameFile, after a buggy replacement has already changed the path.
func publicationFileInfo(path string) (os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, statErr := f.Stat()
	closeErr := f.Close()
	if statErr != nil {
		return nil, statErr
	}
	return info, closeErr
}
