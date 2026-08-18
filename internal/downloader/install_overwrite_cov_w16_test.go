package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-16 (coverage) — the create path's
// no-replace publish loop is BOUNDED (createPublishMaxAttempts) so an
// adversarial foreign writer claiming and releasing the destination across
// every publish fails the install instead of spinning forever. No real
// filesystem produces that churn deterministically at the loop bound, so the
// racer below is a virtual-fs harness: classification always reports a free
// destination while every publish rename refuses exists-class — the exact
// createPublishMaxAttempts exhaustion leg.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w16ChurnRacerFs plays a foreign writer whose destination entry keeps
// vanishing between publishes: Lstat always reports the destination absent
// (the create leg keeps retrying the create publish) and every rename into
// the destination refuses exists-class (the publish collides). The bounded
// loop must give up with the typed churn error after
// createPublishMaxAttempts publishes, never spin.
type w16ChurnRacerFs struct {
	afero.Fs
	dest string
}

func (f *w16ChurnRacerFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Clean(name) == filepath.Clean(f.dest) {
		return nil, false, os.ErrNotExist
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(name)
}

func (f *w16ChurnRacerFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) {
		return &os.PathError{Op: "rename", Path: newname, Err: os.ErrExist}
	}
	return f.Fs.Rename(oldname, newname)
}

func TestInstallOverwritingW16_CreatePublishChurnBoundsOut(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/W16-CHURN"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	fs := &w16ChurnRacerFs{Fs: base, dest: dest}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w16-churn", recorder: recorder})
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the bound carries the collision class")
	require.ErrorContains(t, err, "foreign writers claimed the destination")
	require.False(t, skipped)
	require.True(t, replaced)

	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, staged)),
		"the install failed before the staged bytes installed")
	require.Empty(t, recorder.get(), "no backup was ever journaled")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the busy marker is released even on the churn bound")
}
