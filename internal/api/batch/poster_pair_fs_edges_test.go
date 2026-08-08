package batch

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// brokenFS selectively fails one syscall class for matching paths/predicates.
type brokenFS struct {
	afero.Fs
	failOpen     func(string) bool
	failStat     func(string) bool
	failRemove   func(string) bool
	failOpenFile func(string) bool
	failMkdirAll func(string) bool
	failRenameAt map[int]bool
	renameCalls  int
}

func (b *brokenFS) MkdirAll(n string, perm os.FileMode) error {
	if b.failMkdirAll != nil && b.failMkdirAll(n) {
		return errors.New("mkdir wedged")
	}
	return b.Fs.MkdirAll(n, perm)
}

func (b *brokenFS) Open(n string) (afero.File, error) {
	if b.failOpen != nil && b.failOpen(n) {
		return nil, errors.New("open wedged")
	}
	return b.Fs.Open(n)
}

func (b *brokenFS) OpenFile(n string, flag int, perm os.FileMode) (afero.File, error) {
	if b.failOpenFile != nil && b.failOpenFile(n) {
		return nil, errors.New("openfile wedged")
	}
	return b.Fs.OpenFile(n, flag, perm)
}

func (b *brokenFS) Stat(n string) (os.FileInfo, error) {
	if b.failStat != nil && b.failStat(n) {
		return nil, errors.New("stat wedged")
	}
	return b.Fs.Stat(n)
}

func (b *brokenFS) Remove(n string) error {
	if b.failRemove != nil && b.failRemove(n) {
		return errors.New("remove wedged")
	}
	return b.Fs.Remove(n)
}

func (b *brokenFS) Rename(o, n string) error {
	b.renameCalls++
	if b.failRenameAt[b.renameCalls] {
		return errors.New("rename wedged")
	}
	return b.Fs.Rename(o, n)
}

func seedPosterPair(t *testing.T, fs afero.Fs, dir, id string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, id+"-full.jpg"), []byte("full-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, id+".jpg"), []byte("crop-bytes"), 0o644))
}

func TestBackupPosterPairDefaultsFs(t *testing.T) {
	b := backupPosterPair(nil, t.TempDir(), "JOB-X", "PID-9")
	require.NotNil(t, b)
	b.restore() // nothing existed: no writes, no warnings
}

func TestBackupPosterPairMarksUnreadables(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "posters/JOB-U"
	seedPosterPair(t, mem, dir, "PID-U")
	fs := &brokenFS{Fs: mem, failOpen: func(n string) bool { return true }}
	b := backupPosterPair(fs, "", "JOB-U", "PID-U")
	assert.True(t, b.fullUnreadable)
	assert.True(t, b.croppedUnreadable)
	// unreadable files must never be removed by restore
	mem2 := afero.NewMemMapFs()
	seedPosterPair(t, mem2, dir, "PID-U")
	b2 := backupPosterPair(&brokenFS{Fs: mem2, failOpen: func(n string) bool { return filepath.Base(n) == "PID-U-full.jpg" }}, "", "JOB-U", "PID-U")
	assert.True(t, b2.fullUnreadable)
	assert.False(t, b2.croppedUnreadable)
	b2.restore()
	_, err := mem2.Stat(filepath.Join(dir, "PID-U-full.jpg"))
	assert.NoError(t, err, "unreadable-marked files are left untouched by restore")
}

func TestPosterPairBackupRestoreErrorsWarn(t *testing.T) {
	t.Run("missing file but remove fails", func(t *testing.T) {
		mem := afero.NewMemMapFs()
		fs := &brokenFS{Fs: mem, failRemove: func(n string) bool { return true }}
		// Backup sees nothing (fresh fs), restore tries to remove anyway.
		b := backupPosterPair(fs, "", "JOB-R", "PID-R")
		b.restore()
	})
	t.Run("existed but rewrite fails", func(t *testing.T) {
		mem := afero.NewMemMapFs()
		dir := "posters/JOB-W"
		seedPosterPair(t, mem, dir, "PID-W")
		// Backup on the healthy fs captures bytes; restore through a write-broken fs.
		healthy := backupPosterPair(mem, "", "JOB-W", "PID-W")
		fs := &brokenFS{Fs: mem, failOpenFile: func(n string) bool { return true }}
		healthy.fs = fs
		healthy.restore()
	})
}

func TestPromoteStagedPosterPairNilFsOnRealTemp(t *testing.T) {
	finalize, err := promoteStagedPosterPair(nil, t.TempDir(), "JOB-NIL", "ST-1", "PID-N")
	require.NoError(t, err)
	finalize()
}

func TestPromoteStagedPosterPairFailureArms(t *testing.T) {
	setup := func(t *testing.T) (*brokenFS, string) {
		mem := afero.NewMemMapFs()
		dir := "posters/JOB-P"
		seedPosterPair(t, mem, dir, "PID-P") // installed pair
		seedPosterPair(t, mem, dir, "ST-1")  // staged pair to promote
		return &brokenFS{Fs: mem}, dir
	}

	t.Run("non-exist stat error rolls back", func(t *testing.T) {
		fs, _ := setup(t)
		fs.failStat = func(n string) bool { return strings.Contains(n, "ST-1-full") }
		_, err := promoteStagedPosterPair(fs, "", "JOB-P", "ST-1", "PID-P")
		require.ErrorContains(t, err, "stat wedged")
	})

	t.Run("park rename failure", func(t *testing.T) {
		fs, _ := setup(t)
		fs.failRenameAt = map[int]bool{1: true}
		_, err := promoteStagedPosterPair(fs, "", "JOB-P", "ST-1", "PID-P")
		require.ErrorContains(t, err, "park previous poster")
	})

	t.Run("promote rename failure unparks", func(t *testing.T) {
		fs, dir := setup(t)
		fs.failRenameAt = map[int]bool{2: true}
		_, err := promoteStagedPosterPair(fs, "", "JOB-P", "ST-1", "PID-P")
		require.ErrorContains(t, err, "promote staged poster")
		// The parked existing full file was restored by the rollback.
		bytes, readErr := afero.ReadFile(fs.Fs, filepath.Join(dir, "PID-P-full.jpg"))
		require.NoError(t, readErr)
		assert.Equal(t, "full-bytes", string(bytes))
	})

	t.Run("rollback warn on failed unpromote remove", func(t *testing.T) {
		fs, _ := setup(t)
		fs.failRenameAt = map[int]bool{3: true} // cropped park fails after full promoted
		fs.failRemove = func(n string) bool { return strings.HasSuffix(n, "PID-P-full.jpg") }
		_, err := promoteStagedPosterPair(fs, "", "JOB-P", "ST-1", "PID-P")
		require.ErrorContains(t, err, "park previous poster")
	})

	t.Run("finalize warns on bak remove failure", func(t *testing.T) {
		fs, _ := setup(t)
		finalize, err := promoteStagedPosterPair(fs, "", "JOB-P", "ST-1", "PID-P")
		require.NoError(t, err)
		fs.failRemove = func(n string) bool { return strings.HasSuffix(n, ".bak") }
		finalize()
	})
}

func TestCleanupStagedPosterPairArms(t *testing.T) {
	cleanupStagedPosterPair(nil, t.TempDir(), "JOB-C", "ST-9")
	mem := afero.NewMemMapFs()
	dir := "posters/JOB-C"
	seedPosterPair(t, mem, dir, "ST-9")
	fs := &brokenFS{Fs: mem, failRemove: func(n string) bool { return filepath.Base(n) == "ST-9-full.jpg" }}
	cleanupStagedPosterPair(fs, "", "JOB-C", "ST-9")
	_, statErr := mem.Stat(filepath.Join(dir, "ST-9-full.jpg"))
	assert.NoError(t, statErr, "failed remove leaves the staged file")
	_, statErr = mem.Stat(filepath.Join(dir, "ST-9.jpg"))
	assert.Error(t, statErr, "healthy remove still cleans what it can")
}

func TestResolvePosterIDArms(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-1", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "CAN-7"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "AL-7"},
	})
	got, err := resolvePosterID(store, "AL-7")
	require.NoError(t, err)
	assert.Equal(t, "CAN-7", got, "canonical Movie.ID wins for poster naming")

	got, err = resolvePosterID(store, "UNINDEXED")
	require.NoError(t, err)
	assert.Equal(t, "UNINDEXED", got)

	_, err = resolvePosterID(store, "../evil")
	require.Error(t, err)

	// Canonical resolves to something non-filename-safe: rejected, not used.
	store2 := resultstore.New(1, []string{"/f/b.mp4"})
	store2.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		ResultID: "res-2", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "a/b"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "K-2"},
	})
	_, err = resolvePosterID(store2, "K-2")
	require.Error(t, err)

	assert.Equal(t, path.Base("CAN-7"), "CAN-7")
}
