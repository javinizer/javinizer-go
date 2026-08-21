package fsutil

// POSTER-WRITE-HARDENING wave-47 (codex P2, PR#215) — wedge coverage for the
// busy-marker ACQUIRE/RETURN side joining the wave-38 release-side binding:
// every unlink of the predictable `<dest>.dlbusy` name or of a takeover
// sibling now re-proves the occupant's identity at unlink adjacency
// (releaseClaimedBusyObject / discardBusyMarkerClaim), the takeover bytes
// ride home NO-REPLACE (never over an occupant whose identity was not
// re-proven), and the takeover read observes content+identity through ONE
// descriptor (replacementBusyObserveTakeover). These tests replay plants,
// vanishes, and indeterminate answers into every remaining verify→mutate
// window — foreign bytes must survive byte-intact in every arm.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// w47OSDir returns a real-OsFs test root: every identity-binding arm (a
// swapped plant MUST fail the dev/inode or size/mtime comparisons) runs on
// the host filesystem — afero's MemMapFs hands LIVE FileInfo views with no
// kernel identity, which cannot distinguish a plant from the claimed object
// (the documented virtual leg; see DiscardFailedExclusiveStaging).
func w47OSDir(t *testing.T) (afero.Fs, string) {
	t.Helper()
	return afero.NewOsFs(), t.TempDir()
}

// w47StatWedgeFs replays ONE mutation into the first Stat call whose name
// matches match() — the deterministic form of a directory writer winning a
// verify→remove window.
type w47StatWedgeFs struct {
	afero.Fs
	match func(string) bool
	fire  func(afero.Fs) error
	fired atomic.Bool
}

func (f *w47StatWedgeFs) Stat(name string) (os.FileInfo, error) {
	if f.match(name) && f.fired.CompareAndSwap(false, true) {
		if err := f.fire(f.Fs); err != nil {
			return nil, err
		}
	}
	return f.Fs.Stat(name)
}

// w47StatFailOnceFs fails the first matched Stat with err — the indeterminate
// lookup leg.
type w47StatFailOnceFs struct {
	afero.Fs
	match func(string) bool
	err   error
	fired atomic.Bool
}

func (f *w47StatFailOnceFs) Stat(name string) (os.FileInfo, error) {
	if f.match(name) && f.fired.CompareAndSwap(false, true) {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

// w47ConfirmFired is the test-side arm check shared by every wedge below.
func (f *w47StatWedgeFs) w47ConfirmFired(t *testing.T, what string) {
	t.Helper()
	require.True(t, f.fired.Load(), "the wedge must have replayed %s", what)
}

// w47RacerOnRemoveFs installs a racer's marker at target immediately after
// the wave-59 bound unlink's vacate rename frees it (the release→restore
// window of the takeover return): the vacate Rename(target, ".vac.") moves
// the placeholder off the name, and the racer claims the freed name before
// the restore's no-replace publish can land.
type w47RacerOnRemoveFs struct {
	afero.Fs
	target string
	racer  []byte
	fired  atomic.Bool
}

func (f *w47RacerOnRemoveFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && oldname == f.target && strings.Contains(newname, ".vac.") && f.fired.CompareAndSwap(false, true) {
		_ = afero.WriteFile(f.Fs, f.target, f.racer, 0o600)
	}
	return err
}

// w47ClaimWedgeFs drives AcquireReplacementBusy's claim legs: the O_EXCL
// marker claim returns a file with the configured write/stat behavior, and
// the optional Stat wedge replays a mid-cleanup mutation of the marker name.
type w47ClaimWedgeFs struct {
	afero.Fs
	writeErr error
	statErr  error
	plant    []byte // non-nil: swap the marker for these bytes at the cleanup lookup
	vanish   bool   // remove the marker at the cleanup lookup instead
	lookup   atomic.Bool
}

func (f *w47ClaimWedgeFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if flag&os.O_EXCL != 0 && strings.HasSuffix(name, ReplacementBusySuffix) {
		return &w47ClaimWedgeFile{File: file, fs: f}, nil
	}
	return file, nil
}

func (f *w47ClaimWedgeFs) Stat(name string) (os.FileInfo, error) {
	if strings.HasSuffix(name, ReplacementBusySuffix) && (f.plant != nil || f.vanish) && f.lookup.CompareAndSwap(false, true) {
		if f.vanish {
			if err := f.Fs.Remove(name); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		} else if err := afero.WriteFile(f.Fs, name, f.plant, 0o600); err != nil {
			return nil, err
		}
	}
	return f.Fs.Stat(name)
}

type w47ClaimWedgeFile struct {
	afero.File
	fs *w47ClaimWedgeFs
}

func (f *w47ClaimWedgeFile) WriteString(s string) (int, error) {
	if f.fs.writeErr != nil {
		return 0, f.fs.writeErr
	}
	return f.File.WriteString(s)
}

func (f *w47ClaimWedgeFile) Stat() (os.FileInfo, error) {
	if f.fs.statErr != nil {
		return nil, f.fs.statErr
	}
	return f.File.Stat()
}

// w47ObserveWedgeFs fails the takeover file's Stat or Read through the
// observe descriptor.
type w47ObserveWedgeFs struct {
	afero.Fs
	statErr error
	readErr error
}

func (f *w47ObserveWedgeFs) Open(name string) (afero.File, error) {
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, ".takeover-") {
		return &w47ObserveWedgeFile{File: file, fs: f}, nil
	}
	return file, nil
}

type w47ObserveWedgeFile struct {
	afero.File
	fs *w47ObserveWedgeFs
}

func (f *w47ObserveWedgeFile) Stat() (os.FileInfo, error) {
	if f.fs.statErr != nil {
		return nil, f.fs.statErr
	}
	return f.File.Stat()
}

func (f *w47ObserveWedgeFile) Read(b []byte) (int, error) {
	if f.fs.readErr != nil {
		return 0, f.fs.readErr
	}
	return f.File.Read(b)
}

// w47TakeoverMatch matches the dynamically-named takeover sibling.
func w47TakeoverMatch(name string) bool { return strings.Contains(name, ".takeover-") }

// w47PlantBytes returns marker-legal bytes that differ in size (and content)
// from any takeover/marker the tests plant — the observable "foreign swap".
func w47PlantBytes() []byte {
	return []byte("pid=4242424242,time=999999999999999999,foreign-plant-bytes")
}

// Reclaim: a foreign swap landing on the takeover name inside the
// read→remove window is REFUSED — the plant keeps its bytes, the acquire
// fails typed, and nothing is consumed.
func TestReplacementBusyW47_ReclaimPlantInRemoveWindowPreserved(t *testing.T) {
	base, dir := w47OSDir(t)
	dest := filepath.Join(dir, "poster.jpg")
	writeW28StaleMarker(t, base, dest)
	setW28DeadProbe(t)

	plant := w47PlantBytes()
	// swap wedge: rewrite the takeover file in the remove-adjacency lookup.
	swap := &w47StatWedgeFs{
		Fs:    base,
		match: w47TakeoverMatch,
		fire: func(f afero.Fs) error {
			// locate the single takeover file in the directory and plant over it
			entries, rerr := afero.ReadDir(f, dir)
			if rerr != nil {
				return rerr
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), ".takeover-") {
					return afero.WriteFile(f, filepath.Join(dir, e.Name()), plant, 0o600)
				}
			}
			return errors.New("no takeover file found")
		},
	}

	_, err := AcquireReplacementBusy(swap, dest)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTakeAsideForeign,
		"the bound reclaim refuses a swapped takeover instead of deleting it")
	require.True(t, swap.fired.Load(), "the remove-window wedge must have fired")

	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	foundPlant := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".takeover-") {
			foundPlant = true
			got, gerr := afero.ReadFile(base, filepath.Join(dir, e.Name()))
			require.NoError(t, gerr)
			require.Equal(t, plant, got, "the foreign plant keeps its bytes byte-intact")
		}
	}
	require.True(t, foundPlant, "the takeover name (with the plant) is retained, never unlinked")
}

// Reclaim: a takeover that vanishes on its own before the bound unlink is
// the completed cleanup — the acquire proceeds to claim a fresh marker.
func TestReplacementBusyW47_ReclaimVanishedTakeoverCompletesAcquire(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w47-reclaim-vanish/poster.jpg"
	require.NoError(t, base.MkdirAll("/out/w47-reclaim-vanish", 0o755))
	writeW28StaleMarker(t, base, dest)
	setW28DeadProbe(t)

	vanish := &w47StatWedgeFs{
		Fs:    base,
		match: w47TakeoverMatch,
		fire: func(f afero.Fs) error {
			entries, rerr := afero.ReadDir(f, "/out/w47-reclaim-vanish")
			if rerr != nil {
				return rerr
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), ".takeover-") {
					return f.Remove("/out/w47-reclaim-vanish/" + e.Name())
				}
			}
			return errors.New("no takeover file found")
		},
	}

	release, err := AcquireReplacementBusy(vanish, dest)
	require.NoError(t, err, "a takeover that vanished before the bound unlink is the completed cleanup")
	release()
	_, statErr := base.Stat(ReplacementBusyPath(dest))
	require.ErrorIs(t, statErr, os.ErrNotExist, "the released fresh marker cleans up")
}

// Reclaim: an indeterminate remove-adjacency lookup fails the acquire closed
// without touching the takeover name.
func TestReplacementBusyW47_ReclaimIndeterminateLookupFailsClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/w47-reclaim-statfail/poster.jpg"
	require.NoError(t, base.MkdirAll("/out/w47-reclaim-statfail", 0o755))
	writeW28StaleMarker(t, base, dest)
	setW28DeadProbe(t)

	statErr := errors.New("takeover lookup wedged")
	wedge := &w47StatFailOnceFs{Fs: base, match: w47TakeoverMatch, err: statErr}
	_, err := AcquireReplacementBusy(wedge, dest)
	require.ErrorIs(t, err, statErr)

	entries, rerr := afero.ReadDir(base, "/out/w47-reclaim-statfail")
	require.NoError(t, rerr)
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".takeover-") {
			found = true
		}
	}
	require.True(t, found, "the takeover name is retained on an indeterminate binding answer")
}

// The takeover observe legs: Stat/Read failures through the ONE descriptor
// fail the acquire through the established read-error wrap (never a guess
// about the name's content).
func TestReplacementBusyW47_TakeoverObserveLegsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		statErr error
		readErr error
	}{
		{name: "stat", statErr: errors.New("takeover fstat wedged")},
		{name: "read", readErr: errors.New("takeover read wedged")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			dest := "/out/w47-observe-" + tc.name + "/poster.jpg"
			require.NoError(t, base.MkdirAll("/out/w47-observe-"+tc.name, 0o755))
			writeW28StaleMarker(t, base, dest)
			setW28DeadProbe(t)

			wedge := &w47ObserveWedgeFs{Fs: base, statErr: tc.statErr, readErr: tc.readErr}
			_, err := AcquireReplacementBusy(wedge, dest)
			require.Error(t, err)
			require.Contains(t, err.Error(), "read replacement busy takeover marker")
		})
	}
}

// Claim-cleanup bindings (discardBusyMarkerClaim): a foreign swap of the
// predictable marker name inside the failed-claim cleanup window is kept
// byte-intact, a vanished name completes the cleanup, an indeterminate
// lookup keeps the occupant, and a handle-identity failure keeps both — with
// the warning logged in every keep arm.
func TestReplacementBusyW47_ClaimFailureCleanupLegs(t *testing.T) {
	writeErr := errors.New("claim write wedged")

	t.Run("foreign swap preserved", func(t *testing.T) {
		base, dir := w47OSDir(t)
		dest := filepath.Join(dir, "poster.jpg")
		path := ReplacementBusyPath(dest)
		plant := w47PlantBytes()
		fs := &w47ClaimWedgeFs{Fs: base, writeErr: writeErr, plant: plant}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, writeErr)
		require.True(t, fs.lookup.Load(), "the cleanup lookup wedge must have fired")
		got, gerr := afero.ReadFile(base, path)
		require.NoError(t, gerr)
		require.Equal(t, plant, got, "the swapped-in foreign marker keeps its bytes")
		require.Contains(t, logs.String(), "foreign")
	})

	t.Run("vanished completes the cleanup", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/w47-claim-vanish/poster.jpg"
		require.NoError(t, base.MkdirAll("/out/w47-claim-vanish", 0o755))
		path := ReplacementBusyPath(dest)
		fs := &w47ClaimWedgeFs{Fs: base, writeErr: writeErr, vanish: true}

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, writeErr)
		_, statErr := base.Stat(path)
		require.ErrorIs(t, statErr, os.ErrNotExist, "the vanished claim completed its own cleanup")
	})

	t.Run("indeterminate lookup keeps the occupant", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/w47-claim-ind/poster.jpg"
		require.NoError(t, base.MkdirAll("/out/w47-claim-ind", 0o755))
		path := ReplacementBusyPath(dest)
		plant := w47PlantBytes()
		fs := &w47ClaimWedgeFs{Fs: base, writeErr: writeErr, plant: plant}
		// First cleanup lookup replays an IO failure instead of a swap.
		fs.lookup.Store(true) // disarm the swap arm for this leg
		failArm := &w47StatFailOnceFs{
			Fs:    fs,
			match: func(name string) bool { return strings.HasSuffix(name, ReplacementBusySuffix) },
			err:   errors.New("cleanup lookup wedged"),
		}
		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		_, err := AcquireReplacementBusy(failArm, dest)
		require.ErrorIs(t, err, writeErr)
		_, statErr := base.Stat(path)
		require.NoError(t, statErr, "an indeterminate binding answer keeps the claim on disk for manual cleanup")
		require.Contains(t, logs.String(), "manual cleanup")
	})

	t.Run("handle identity failure keeps the marker", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/w47-claim-stat/poster.jpg"
		require.NoError(t, base.MkdirAll("/out/w47-claim-stat", 0o755))
		path := ReplacementBusyPath(dest)
		statErr := errors.New("claim handle stat wedged")
		fs := &w47ClaimWedgeFs{Fs: base, writeErr: writeErr, statErr: statErr}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, writeErr, "the write failure still surfaces")
		_, serr := base.Stat(path)
		require.NoError(t, serr, "an unprovable identity keeps the claim on disk for manual cleanup")
		require.Contains(t, logs.String(), "manual cleanup")
	})

	t.Run("claim stat failure surfaces", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/w47-claim-statleg/poster.jpg"
		require.NoError(t, base.MkdirAll("/out/w47-claim-statleg", 0o755))
		statErr := errors.New("claim pre-close stat wedged")
		fs := &w47ClaimWedgeFs{Fs: base, statErr: statErr}

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, statErr)
		require.ErrorContains(t, err, "stat replacement busy marker")
	})
}

// w47PlaceholderStatFs drives the takeover-return placeholder identity
// capture legs: the O_EXCL placeholder returns a file whose Stat (and
// optionally Close) fails.
type w47PlaceholderStatFs struct {
	afero.Fs
	statErr  error
	closeErr error
}

func (f *w47PlaceholderStatFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if flag&os.O_EXCL != 0 && strings.HasSuffix(name, ReplacementBusySuffix) {
		return &w47PlaceholderStatFile{File: file, fs: f}, nil
	}
	return file, nil
}

type w47PlaceholderStatFile struct {
	afero.File
	fs *w47PlaceholderStatFs
}

func (f *w47PlaceholderStatFile) Stat() (os.FileInfo, error) {
	if f.fs.statErr != nil {
		return nil, f.fs.statErr
	}
	return f.File.Stat()
}

func (f *w47PlaceholderStatFile) Close() error {
	_ = f.File.Close()
	return f.fs.closeErr
}

// Takeover return: a placeholder whose identity cannot be captured is never
// released by pathname alone — both occupants stay for manual cleanup, and
// the close error dominates surfacing exactly like the pre-shape w17 leg.
func TestReplacementBusyW47_ReturnTakeoverPlaceholderStatFailure(t *testing.T) {
	statErr := errors.New("placeholder stat wedged")
	closeErr := errors.New("placeholder close wedged")

	t.Run("stat failure alone", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dir := "/out/w47-return-stat"
		dest := dir + "/poster.jpg"
		require.NoError(t, base.MkdirAll(dir, 0o755))
		path := ReplacementBusyPath(dest)
		takeover := path + ".takeover-test"
		content := []byte("pid=123,time=456")
		require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		fs := &w47PlaceholderStatFs{Fs: base, statErr: statErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, statErr)
		require.ErrorContains(t, err, "stat replacement busy restore placeholder")

		info, serr := base.Stat(path)
		require.NoError(t, serr, "the unprovable placeholder is never released by pathname alone")
		require.Zero(t, info.Size())
		got, gerr := afero.ReadFile(base, takeover)
		require.NoError(t, gerr)
		require.Equal(t, content, got, "the displaced bytes stay recoverable at the takeover name")
		require.Contains(t, logs.String(), "manual cleanup")
	})

	t.Run("stat failure with close failure surfaces the close error", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dir := "/out/w47-return-statclose"
		dest := dir + "/poster.jpg"
		require.NoError(t, base.MkdirAll(dir, 0o755))
		path := ReplacementBusyPath(dest)
		takeover := path + ".takeover-test"
		content := []byte("pid=123,time=456")
		require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))

		fs := &w47PlaceholderStatFs{Fs: base, statErr: statErr, closeErr: closeErr}
		err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
		require.ErrorIs(t, err, closeErr)
		require.ErrorContains(t, err, "close replacement busy restore placeholder")
	})
}

// Takeover return: a foreign occupant swapped onto the placeholder's name
// before the identity release is NEVER restored over — the takeover bytes
// ride to quarantine, the foreign marker keeps the path, and the takeover
// name is freed by the bound remove.
func TestReplacementBusyW47_ReturnTakeoverPlaceholderSwapQuarantines(t *testing.T) {
	base, dir := w47OSDir(t)
	dest := filepath.Join(dir, "poster.jpg")
	path := ReplacementBusyPath(dest)
	takeover := path + ".takeover-test"
	content := []byte("pid=123,time=456")
	require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))

	foreign := w47PlantBytes()
	swap := &w47StatWedgeFs{
		Fs:    base,
		match: func(name string) bool { return name == path },
		fire: func(f afero.Fs) error {
			return afero.WriteFile(f, path, foreign, 0o600)
		},
	}

	err := replacementBusyReturnTakeover(swap, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.NoError(t, err, "the takeover bytes are preserved in quarantine — the failure is the foreign claim's, not ours")
	require.True(t, swap.fired.Load(), "the placeholder-swap wedge must have fired")

	got, gerr := afero.ReadFile(base, path)
	require.NoError(t, gerr)
	require.Equal(t, foreign, got, "the foreign occupant was never restored over")
	_, serr := base.Stat(takeover)
	require.ErrorIs(t, serr, os.ErrNotExist, "the takeover name is freed by the bound remove")

	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	foundQuarantine := false
	for _, e := range entries {
		if strings.Contains(e.Name(), replacementBusyQuarantineMark) {
			foundQuarantine = true
			q, qerr := afero.ReadFile(base, filepath.Join(dir, e.Name()))
			require.NoError(t, qerr)
			require.Equal(t, content, q, "the displaced bytes survive quarantined, byte-intact")
		}
	}
	require.True(t, foundQuarantine, "a quarantine sibling preserved the takeover bytes")
}

// Takeover return: a racer claiming the name inside the release→restore
// window WINS (typed no-replace refusal): its marker keeps the path, and the
// takeover bytes ride to quarantine instead of clobbering it.
func TestReplacementBusyW47_ReturnTakeoverRacerWinQuarantines(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w47-return-racer"
	dest := dir + "/poster.jpg"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	path := ReplacementBusyPath(dest)
	takeover := path + ".takeover-test"
	content := []byte("pid=123,time=456")
	require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))

	racer := w47PlantBytes()
	wedge := &w47RacerOnRemoveFs{Fs: base, target: path, racer: racer}

	err := replacementBusyReturnTakeover(wedge, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.NoError(t, err, "the racer's live marker wins; the takeover bytes are quarantined")
	require.True(t, wedge.fired.Load(), "the racer window wedge must have fired")

	got, gerr := afero.ReadFile(base, path)
	require.NoError(t, gerr)
	require.Equal(t, racer, got, "the racer's marker is never clobbered by the restore")
	_, serr := base.Stat(takeover)
	require.ErrorIs(t, serr, os.ErrNotExist)

	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	foundQuarantine := false
	for _, e := range entries {
		if strings.Contains(e.Name(), replacementBusyQuarantineMark) {
			foundQuarantine = true
			q, qerr := afero.ReadFile(base, filepath.Join(dir, e.Name()))
			require.NoError(t, qerr)
			require.Equal(t, content, q)
		}
	}
	require.True(t, foundQuarantine)
}

// w47RefusalCloseFailFs combines a failing placeholder Close with a racer
// planted inside the release→restore window: the publish refuses (typed
// collision), the takeover bytes still ride to quarantine, and the close
// error keeps dominating the surfacing (the w17 contract).
type w47RefusalCloseFailFs struct {
	afero.Fs
	closeErr error
	target   string
	racer    []byte
	raced    atomic.Bool
}

func (f *w47RefusalCloseFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if flag&os.O_EXCL != 0 && name == f.target {
		return &w47RefusalCloseFailFile{File: file, fs: f}, nil
	}
	return file, nil
}

func (f *w47RefusalCloseFailFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && oldname == f.target && strings.Contains(newname, ".vac.") && f.raced.CompareAndSwap(false, true) {
		_ = afero.WriteFile(f.Fs, f.target, f.racer, 0o600)
	}
	return err
}

type w47RefusalCloseFailFile struct {
	afero.File
	fs *w47RefusalCloseFailFs
}

func (f *w47RefusalCloseFailFile) Close() error {
	_ = f.File.Close()
	return f.fs.closeErr
}

func TestReplacementBusyW47_ReturnTakeoverRefusalCloseFailSurfacesClose(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w47-return-refusalclose"
	dest := dir + "/poster.jpg"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	path := ReplacementBusyPath(dest)
	takeover := path + ".takeover-test"
	content := []byte("pid=123,time=456")
	require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))

	closeErr := errors.New("placeholder close wedged")
	racer := w47PlantBytes()
	wedge := &w47RefusalCloseFailFs{Fs: base, closeErr: closeErr, target: path, racer: racer}

	err := replacementBusyReturnTakeover(wedge, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.ErrorIs(t, err, closeErr,
		"the close error keeps dominating even when the restore refused on a window racer")
	require.True(t, wedge.raced.Load(), "the racer wedge must have fired")

	got, gerr := afero.ReadFile(base, path)
	require.NoError(t, gerr)
	require.Equal(t, racer, got, "the racer's marker keeps its bytes")
	_, terr := base.Stat(takeover)
	require.ErrorIs(t, terr, os.ErrNotExist, "the takeover bytes rode to quarantine")

	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	foundQuarantine := false
	for _, e := range entries {
		if strings.Contains(e.Name(), replacementBusyQuarantineMark) {
			foundQuarantine = true
		}
	}
	require.True(t, foundQuarantine)
}

// Takeover return: a wedged placeholder unlink routes the takeover bytes to
// quarantine (the placeholder stays — the documented manual-cleanup
// residual) rather than renaming over a name the flow can no longer prove.
func TestReplacementBusyW47_ReturnTakeoverWedgedReleaseQuarantines(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w47-return-wedge"
	dest := dir + "/poster.jpg"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	path := ReplacementBusyPath(dest)
	takeover := path + ".takeover-test"
	content := []byte("pid=123,time=456")
	require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))

	removeErr := errors.New("placeholder unlink wedged")
	// Wave-59: releaseClaimedBusyObject delegates to the wave-44 bound
	// unlink, so the wedged remove targets the fresh ".vac." terminal name
	// the vacate rename armed — w59TerminalRemoveFailFs learns that name
	// (size-agnostic: the placeholder is 0 bytes).
	wedge := &w59TerminalRemoveFailFs{Fs: base, err: removeErr, fail: 1}

	err := replacementBusyReturnTakeover(wedge, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.NoError(t, err)

	info, serr := base.Stat(path)
	require.NoError(t, serr, "the un-unlinkable placeholder stays for manual cleanup")
	require.Zero(t, info.Size())
	_, terr := base.Stat(takeover)
	require.ErrorIs(t, terr, os.ErrNotExist)

	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	foundQuarantine := false
	for _, e := range entries {
		if strings.Contains(e.Name(), replacementBusyQuarantineMark) {
			foundQuarantine = true
		}
	}
	require.True(t, foundQuarantine)
}

// Quarantine leg: a foreign swap under the takeover name before the
// post-quarantine bound remove is refused — the quarantine keeps the bytes,
// the plant keeps its name.
func TestReplacementBusyW47_QuarantineBoundRemoveRefusesSwap(t *testing.T) {
	base, dir := w47OSDir(t)
	dest := filepath.Join(dir, "poster.jpg")
	path := ReplacementBusyPath(dest)
	takeover := path + ".takeover-test"
	content := []byte("pid=123,time=456")
	// The occupied fixture drives the flow straight into the quarantine leg.
	require.NoError(t, afero.WriteFile(base, path, []byte("pid=999,time=789"), 0o600))
	require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))

	plant := w47PlantBytes()
	swap := &w47StatWedgeFs{
		Fs:    base,
		match: w47TakeoverMatch,
		fire: func(f afero.Fs) error {
			return afero.WriteFile(f, takeover, plant, 0o600)
		},
	}

	err := replacementBusyReturnTakeover(swap, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorContains(t, err, "remove replacement busy takeover marker after quarantine")
	require.True(t, swap.fired.Load())

	got, gerr := afero.ReadFile(base, takeover)
	require.NoError(t, gerr)
	require.Equal(t, plant, got, "the plant under the takeover name keeps its bytes")
	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	foundQuarantine := false
	for _, e := range entries {
		if strings.Contains(e.Name(), replacementBusyQuarantineMark) {
			foundQuarantine = true
			q, qerr := afero.ReadFile(base, filepath.Join(dir, e.Name()))
			require.NoError(t, qerr)
			require.Equal(t, content, q, "the displaced bytes stay preserved in quarantine")
		}
	}
	require.True(t, foundQuarantine)
}
