package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type replacementFaultFS struct {
	afero.Fs
	lstat    func(string) (os.FileInfo, bool, error)
	rename   func(string, string) error
	open     func(string) (afero.File, error)
	openFile func(string, int, os.FileMode) (afero.File, error)
}

func (f *replacementFaultFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.lstat != nil {
		return f.lstat(name)
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}
func (f *replacementFaultFS) Rename(oldname, newname string) error {
	if f.rename != nil {
		return f.rename(oldname, newname)
	}
	return f.Fs.Rename(oldname, newname)
}
func (f *replacementFaultFS) Open(name string) (afero.File, error) {
	if f.open != nil {
		return f.open(name)
	}
	return f.Fs.Open(name)
}
func (f *replacementFaultFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.openFile != nil {
		return f.openFile(name, flag, perm)
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type installedFactsRecorder struct{ replacementBatchRecorder }

func (r *installedFactsRecorder) ConfirmReplacementInstalled(context.Context, string, string, string, models.ReplacementBackupFacts) error {
	r.confirmed++
	return r.confirmErr
}

type readlinkFaultFS struct {
	*replacementFaultFS
	err error
}

func (f *readlinkFaultFS) ReadlinkIfPossible(string) (string, error) { return "", f.err }
func (f *readlinkFaultFS) SymlinkIfPossible(oldname, newname string) error {
	if linker, ok := f.Fs.(afero.Linker); ok {
		return linker.SymlinkIfPossible(oldname, newname)
	}
	return afero.ErrNoSymlink
}

func baseLstat(fs afero.Fs, name string) (os.FileInfo, bool, error) {
	if ls, ok := fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := fs.Stat(name)
	return info, false, err
}

func replacementBatchTestPathEqual(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func cleanupReplacementBatchLocks(t *testing.T, batch *ReplacementBatch) {
	t.Helper()
	t.Cleanup(func() {
		for _, leg := range batch.legs {
			leg.release()
		}
	})
}

func requireReplacementBatchFaultObserved(t *testing.T, observed <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-observed:
	case <-timer.C:
		t.Fatal("replacement batch fault hook did not match the cleaned destination path")
	}
}

func TestReplacementBatchRejectsInspectionAndReservationFaultsWithoutLosingDestination(t *testing.T) {
	sentinel := errors.New("filesystem fault")
	t.Run("preflight inspection", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &replacementFaultFS{Fs: base, lstat: func(string) (os.FileInfo, bool, error) { return nil, false, sentinel }}
		batch, err := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		require.NoError(t, err)
		require.ErrorIs(t, batch.Preflight([]string{"/dest"}), sentinel)
	})
	t.Run("busy marker", func(t *testing.T) {
		base := afero.NewMemMapFs()
		destination := string(os.PathSeparator) + "unused" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "dest"
		require.NoError(t, afero.WriteFile(base, filepath.Clean(destination), []byte("old"), 0o644))
		observed := make(chan struct{}, 1)
		fs := &replacementFaultFS{Fs: base, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
			if replacementBatchTestPathEqual(name, fsutil.ReplacementBusyPath(destination)) {
				observed <- struct{}{}
				return nil, sentinel
			}
			return base.OpenFile(name, flag, perm)
		}}
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		cleanupReplacementBatchLocks(t, batch)
		_, err := batch.BeforePublish(t.Context(), destination, true)
		requireReplacementBatchFaultObserved(t, observed)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(mustReadReplacementBatch(t, base, filepath.Clean(destination))))
	})
	t.Run("reclassification inspection", func(t *testing.T) {
		base := afero.NewMemMapFs()
		destination := string(os.PathSeparator) + "unused" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "dest"
		require.NoError(t, afero.WriteFile(base, filepath.Clean(destination), []byte("old"), 0o644))
		calls := 0
		observed := make(chan struct{}, 1)
		fs := &replacementFaultFS{Fs: base}
		fs.lstat = func(name string) (os.FileInfo, bool, error) {
			if replacementBatchTestPathEqual(name, destination) {
				calls++
				if calls == 2 {
					observed <- struct{}{}
					return nil, false, sentinel
				}
			}
			return baseLstat(base, name)
		}
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		cleanupReplacementBatchLocks(t, batch)
		_, err := batch.BeforePublish(t.Context(), destination, true)
		requireReplacementBatchFaultObserved(t, observed)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(mustReadReplacementBatch(t, base, filepath.Clean(destination))))
	})
	t.Run("destination changes to directory", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/dest", []byte("old"), 0o644))
		calls := 0
		fs := &replacementFaultFS{Fs: base}
		fs.lstat = func(name string) (os.FileInfo, bool, error) {
			if replacementBatchTestPathEqual(name, "/dest") {
				calls++
				if calls == 2 {
					require.NoError(t, base.Remove(name))
					require.NoError(t, base.Mkdir(name, 0o755))
				}
			}
			return baseLstat(base, name)
		}
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		cleanupReplacementBatchLocks(t, batch)
		_, err := batch.BeforePublish(t.Context(), "/dest", true)
		require.ErrorContains(t, err, "not a regular file")
		info, statErr := base.Stat("/dest")
		require.NoError(t, statErr)
		require.True(t, info.IsDir())
	})
	t.Run("backup reservation create", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/dest", []byte("old"), 0o644))
		fs := &replacementFaultFS{Fs: base, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
			if strings.Contains(name, ".dlbak.") {
				return nil, sentinel
			}
			return base.OpenFile(name, flag, perm)
		}}
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		cleanupReplacementBatchLocks(t, batch)
		_, err := batch.BeforePublish(t.Context(), "/dest", true)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(mustReadReplacementBatch(t, base, "/dest")))
	})
	t.Run("backup reservation identity", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/dest", []byte("old"), 0o644))
		fs := &replacementFaultFS{Fs: base}
		fs.lstat = func(name string) (os.FileInfo, bool, error) {
			info, used, err := baseLstat(base, name)
			if strings.Contains(name, ".dlbak.") && err == nil {
				return nil, used, sentinel
			}
			return info, used, err
		}
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		cleanupReplacementBatchLocks(t, batch)
		_, err := batch.BeforePublish(t.Context(), "/dest", true)
		require.ErrorContains(t, err, "claim staged replacement backup")
		require.Equal(t, "old", string(mustReadReplacementBatch(t, base, "/dest")))
	})
	t.Run("reserved handoff", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/dest", []byte("old"), 0o644))
		fs := &replacementFaultFS{Fs: base, rename: func(string, string) error { return sentinel }}
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		cleanupReplacementBatchLocks(t, batch)
		_, err := batch.BeforePublish(t.Context(), "/dest", true)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(mustReadReplacementBatch(t, base, "/dest")))
	})
}

func TestReplacementBatchRetainsRecoveryObjectsWhenJournalOrRestoreFails(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/dest", []byte("old"), 0o644))
	sentinel := errors.New("restore refused")
	failRestore := false
	fs := &replacementFaultFS{Fs: base}
	fs.rename = func(oldname, newname string) error {
		if failRestore && strings.Contains(oldname, ".dlbak.") && replacementBatchTestPathEqual(newname, "/dest") {
			return sentinel
		}
		return base.Rename(oldname, newname)
	}
	rec := &replacementBatchRecorder{recordErr: errors.New("journal unavailable")}
	batch, _ := NewReplacementBatch(fs, "op", rec)
	failRestore = true
	_, err := batch.BeforePublish(t.Context(), "/dest", true)
	require.ErrorContains(t, err, "backup retained")
	require.ErrorIs(t, err, rec.recordErr)
	_, statErr := base.Stat("/dest")
	require.True(t, os.IsNotExist(statErr))
}

func TestReplacementBatchObservesPartialPublishOnlyOnce(t *testing.T) {
	fs := afero.NewMemMapFs()
	batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
	batch.ObservePublishResult("/unknown")
	_, err := batch.BeforePublish(t.Context(), "/created", false)
	require.NoError(t, err)
	batch.ObservePublishResult("/created")
	require.NoError(t, afero.WriteFile(fs, "/created", []byte("partial"), 0o644))
	batch.ObservePublishResult("/created")
	batch.ObservePublishResult("/created")
	require.NoError(t, batch.Rollback(t.Context()))
	_, err = fs.Stat("/created")
	require.True(t, os.IsNotExist(err))
}

func TestReplacementBatchInstalledFactsFailureLeavesJournalActionable(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/dest", []byte("old"), 0o644))
	failInspect := false
	inspectCalls := 0
	sentinel := errors.New("installed output unreadable")
	fs := &replacementFaultFS{Fs: base}
	fs.lstat = func(name string) (os.FileInfo, bool, error) {
		if failInspect && replacementBatchTestPathEqual(name, "/dest") {
			inspectCalls++
			if inspectCalls == 2 {
				return nil, false, sentinel
			}
		}
		return baseLstat(base, name)
	}
	rec := &installedFactsRecorder{}
	batch, _ := NewReplacementBatch(fs, "op", rec)
	_, err := batch.BeforePublish(t.Context(), "/dest", true)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(base, "/dest", []byte("new"), 0o644))
	failInspect = true
	err = batch.ConfirmPublish(t.Context(), "/dest")
	require.ErrorIs(t, err, sentinel)
	require.Zero(t, rec.confirmed)
	failInspect = false
	// Rollback removes the identity-bound unconfirmed install and restores the
	// still-journaled destination bytes.
	require.NoError(t, batch.Rollback(t.Context()))
	require.Equal(t, "old", string(mustReadReplacementBatch(t, base, "/dest")))
}

func TestReplacementBatchDirectOriginTrackingFailsClosed(t *testing.T) {
	t.Run("marker unavailable", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/dest", []byte("new"), 0o644))
		fs := &replacementFaultFS{Fs: base, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
			if replacementBatchTestPathEqual(name, fsutil.ReplacementBusyPath("/dest")) {
				return nil, errors.New("marker unavailable")
			}
			return base.OpenFile(name, flag, perm)
		}}
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		require.ErrorContains(t, batch.SetRollbackOrigin("/dest", "/source"), "marker unavailable")
		require.Equal(t, "new", string(mustReadReplacementBatch(t, base, "/dest")))
	})
	t.Run("nonregular installed output", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.Mkdir("/dest", 0o755))
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		require.ErrorContains(t, batch.SetRollbackOrigin("/dest", "/source"), "no regular installed output")
	})
	t.Run("armed but not installed", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		batch, _ := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
		cleanupReplacementBatchLocks(t, batch)
		_, err := batch.BeforePublish(t.Context(), "/dest", false)
		require.NoError(t, err)
		require.ErrorContains(t, batch.SetRollbackOrigin("/dest", "/source"), "no installed output")
		require.NoError(t, batch.Rollback(t.Context()))
	})
}

func TestReplacementBatchRollbackReportsMarkerAndRestoreFailures(t *testing.T) {
	t.Run("marker reacquire", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &replacementFaultFS{Fs: base, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
			if replacementBatchTestPathEqual(name, fsutil.ReplacementBusyPath("/dest")) {
				return nil, errors.New("marker reacquire")
			}
			return base.OpenFile(name, flag, perm)
		}}
		batch := &ReplacementBatch{fs: fs, legs: []*replacementBatchLeg{{destination: "/dest"}}}
		require.ErrorContains(t, batch.Rollback(t.Context()), "marker reacquire")
	})
	t.Run("backup restore", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/backup", []byte("old"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/dest", []byte("foreign"), 0o644))
		batch := &ReplacementBatch{fs: base, opID: "op", recorder: &replacementBatchRecorder{}, legs: []*replacementBatchLeg{{destination: "/dest", backup: "/backup", replaced: true}}}
		require.ErrorContains(t, batch.Rollback(t.Context()), "restore staged replacement")
		require.Equal(t, "foreign", string(mustReadReplacementBatch(t, base, "/dest")))
		require.Equal(t, "old", string(mustReadReplacementBatch(t, base, "/backup")))
	})
}

type releaseCollisionRecorder struct {
	replacementBatchRecorder
	fs afero.Fs
}

func (r *releaseCollisionRecorder) ReleaseReplacement(_ context.Context, _, _, backup string) error {
	r.released++
	if err := afero.WriteFile(r.fs, backup, []byte("foreign"), 0o644); err != nil {
		return err
	}
	return errors.New("journal release unavailable")
}

func TestReplacementBatchMarksRestorePendingWhenJournalReleaseCannotRearm(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("old"), 0o644))
	rec := &releaseCollisionRecorder{fs: fs}
	batch, err := NewReplacementBatch(fs, "op", rec)
	require.NoError(t, err)
	_, err = batch.BeforePublish(t.Context(), "/dest", true)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("new"), 0o644))
	require.NoError(t, batch.ConfirmPublish(t.Context(), "/dest"))
	err = batch.Rollback(t.Context())
	require.ErrorContains(t, err, "journal release unavailable")
	require.Equal(t, "old", string(mustReadReplacementBatch(t, fs, "/dest")))
	leg := batch.find("/dest")
	require.Equal(t, "foreign", string(mustReadReplacementBatch(t, fs, leg.backup)))
}

func TestInstalledFactsPropagateInspectionFailure(t *testing.T) {
	sentinel := errors.New("identity inspection failed")
	fs := &replacementFaultFS{Fs: afero.NewMemMapFs(), lstat: func(string) (os.FileInfo, bool, error) {
		return nil, false, sentinel
	}}
	_, err := captureInstalledReplacementFacts(fs, "/dest")
	require.ErrorIs(t, err, sentinel)
}

func TestInstalledSymlinkFactsRequireReadableLink(t *testing.T) {
	if _, ok := interface{}(afero.NewOsFs()).(afero.Linker); !ok {
		t.Skip("symlinks unsupported")
	}
	dir := t.TempDir()
	target := dir + "/target"
	link := dir + "/link"
	require.NoError(t, os.WriteFile(target, []byte("target"), 0o644))
	require.NoError(t, os.Symlink(target, link))
	base := afero.NewOsFs()
	withoutReader := &replacementFaultFS{Fs: base}
	_, err := captureInstalledReplacementFacts(withoutReader, link)
	require.ErrorContains(t, err, "cannot read installed symlink")
	readErr := errors.New("readlink failed")
	_, err = captureInstalledReplacementFacts(&readlinkFaultFS{replacementFaultFS: &replacementFaultFS{Fs: base}, err: readErr}, link)
	require.ErrorIs(t, err, readErr)
}
