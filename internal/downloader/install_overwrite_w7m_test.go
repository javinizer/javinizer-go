package downloader

// POSTER-WRITE-HARDENING codex PR#215 (P1): the durable .dlbusy marker must
// be claimed BEFORE the destination's existence classification and held
// through BOTH the create and the replace paths — another process may hold
// the marker while it has the destination renamed aside, so an
// observed-absent destination must never silently take the create path under
// a live owner.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w7mMarkerObserverFs records whether the durable busy marker exists at the
// moment the staged bytes are swapped onto the destination.
type w7mMarkerObserverFs struct {
	afero.Fs
	dest      string
	sawAtSwap bool
}

func (f *w7mMarkerObserverFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) {
		if _, err := f.Fs.Stat(fsutil.ReplacementBusyPath(f.dest)); err == nil {
			f.sawAtSwap = true
		}
	}
	return f.Fs.Rename(oldname, newname)
}

// Marker pre-held + destination missing: the create path must NOT silently
// install under a live foreign owner. The refusal keeps the established busy
// semantics — skip+warn, no error, destination untouched (here: still
// absent) — and the armed ledger proves it is the busy veto, not an
// unarmed-ledger refusal, that fires.
func TestInstallOverwritingW7M_BusyMarkerAbsentDestRefusesCreate(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W7M-BUSY-MISSING/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))
	busyRelease, err := fsutil.AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	defer busyRelease()

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7m-busy-missing", recorder: recorder})
	require.NoError(t, err, "busy refusal keeps the established skip+warn semantics")
	require.True(t, skipped, "active foreign marker vetoes the create path")
	require.True(t, replaced)

	_, statErr := fs.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "dest must NOT be created under a foreign owner")
	got, readErr := afero.ReadFile(fs, staged)
	require.NoError(t, readErr)
	require.Equal(t, "new", string(got), "staged bytes stay staged for the caller to clean up")
	require.Empty(t, recorder.get(), "a refused install must not journal")

	// The downloader must not have consumed or replaced the foreign marker.
	_, markerErr := fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.NoError(t, markerErr)
	busyRelease()
	_, goneErr := fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, goneErr, os.ErrNotExist)
}

// Marker pre-held + destination present: the same veto covers the replace
// path (this is the pre-existing acquire/contention behavior — hoisting must
// not change the busy-error semantics).
func TestInstallOverwritingW7M_BusyMarkerPresentDestRefusesReplace(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W7M-BUSY-PRESENT/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))
	busyRelease, err := fsutil.AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	defer busyRelease()

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7m-busy-present", recorder: recorder})
	require.NoError(t, err)
	require.True(t, skipped)
	require.True(t, replaced)
	got, readErr := afero.ReadFile(fs, dest)
	require.NoError(t, readErr)
	require.Equal(t, "old", string(got), "foreign-owned destination bytes are preserved")
	require.Empty(t, recorder.get())
	_, markerErr := fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.NoError(t, markerErr, "the busy veto leaves the foreign marker alone")
}

// The create path under OUR held marker succeeds: the swap observes the
// marker in place, the staged bytes land, and the deferred release removes
// the marker on return.
func TestInstallOverwritingW7M_CreatePathSucceedsUnderHeldMarker(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W7M-CREATE/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	fs := &w7mMarkerObserverFs{Fs: base, dest: dest}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7m-create-under-marker", recorder: recorder})
	require.NoError(t, err)
	require.False(t, skipped)
	require.False(t, replaced, "an absent destination stays classified as a create")
	require.True(t, fs.sawAtSwap, "the marker is held through the create-path swap")
	got, readErr := afero.ReadFile(base, dest)
	require.NoError(t, readErr)
	require.Equal(t, "new", string(got))
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the deferred release runs on the create-path return")
	require.Empty(t, recorder.get(), "a create journals nothing")
}
