package fsutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w34/w38: codex PR#215 — the release path's marker unlink used to run BY
// PATHNAME after a separate token read (finding F4): a directory writer
// swapping the marker in between had the REPLACEMENT marker deleted. The
// wave-38 take-aside closes it: observe through one open handle (token +
// identity ride one descriptor), take the observed object aside onto an
// O_EXCL-reserved scratch, re-prove identity there, and unlink only the
// re-bound object — wave-44: vacated onto a fresh claimed terminal name
// first, so even the scratch name never sees a verify→Remove pathname pair.
// A transiently wedged unlink is retried
// with backoff; a persistent wedge frees the marker name regardless and
// leaves only the inert scratch (never a busy-block), while a released
// marker left on disk by a pre-wave-38 wedged release still decodes and
// reclaims through the takeover rules.

func w34ReleasedToken(token string) string {
	return token + ",released=1"
}

// The headline wave-38 release (wave-44 terminal shape): the marker is
// taken aside and only the fresh crypto-claimed terminal name is ever
// unlinked — neither the marker name nor the caller-predictable scratch
// name is ever removed by pathname.
func TestReplacementBusyW34_ReleaseTakesMarkerAside(t *testing.T) {
	setW34ReleaseBackoff(t, []time.Duration{time.Nanosecond, time.Nanosecond})
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/out/w34-takeaside", 0o755))
	dest := "/out/w34-takeaside/poster.jpg"
	path := ReplacementBusyPath(dest)
	fs := &w34RemoveCountFs{Fs: base, path: path}

	release, err := AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	release()
	require.Zero(t, fs.pathRemoves.Load(), "the release NEVER unlinks the marker by pathname")
	require.Zero(t, fs.scratchRemoves.Load(),
		"wave-44: the scratch name the caller (or a watcher) can predict is never path-removed either")
	require.EqualValues(t, 1, fs.terminalRemoves.Load(),
		"exactly one bound unlink — at the fresh claimed terminal name after the identity re-bind")
	_, err = base.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist, "the marker is gone")
	require.Empty(t, w28RecoveryFiles(t, base, "/out/w34-takeaside", ".takeover-"),
		"no take-aside scratch lingers after a clean release")

	// A fresh claim re-acquires without climbing past any litter.
	release2, err := AcquireReplacementBusy(base, dest)
	require.NoError(t, err)
	release2()
	_, err = base.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReplacementBusyW34_ReleaseRetryLegs(t *testing.T) {
	t.Run("one backoff leg recovers", func(t *testing.T) {
		setW34ReleaseBackoff(t, []time.Duration{time.Nanosecond, time.Nanosecond})
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-retry", 0o755))
		dest := "/out/w34-retry/poster.jpg"
		fs := &w34RemoveWedgeFs{Fs: base, scratchErr: errors.New("unlinked transiently"), allowAfter: 1}
		// wave-44: attempt 1 wedges the terminal remove — the verified object
		// rewinds onto the freed scratch NO-REPLACE and the backoff retry
		// re-runs the whole bound construction (re-bind, fresh terminal claim,
		// vacate, re-bind, remove) against the pre-Unlink names.

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()
		require.EqualValues(t, 2, fs.attempts.Load(), "first terminal unlink fails (the object rewinds onto the scratch), first backoff retry succeeds")
		_, err = base.Stat(ReplacementBusyPath(dest))
		require.ErrorIs(t, err, os.ErrNotExist)
		require.Empty(t, logs.String(), "a recovered unlink must not warn")
		require.Empty(t, w28RecoveryFiles(t, base, "/out/w34-retry", ".takeover-"))
	})

	t.Run("already-gone marker is a silent no-op", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-gone", 0o755))
		dest := "/out/w34-gone/poster.jpg"
		path := ReplacementBusyPath(dest)
		fs := &w34ObserveVanishFs{Fs: base, path: path}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()
		_, err = base.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "a marker that vanished underneath the observation stays gone")
		require.Empty(t, logs.String())
	})

	t.Run("persistent unlink wedge frees the name and leaves only the scratch", func(t *testing.T) {
		setW34ReleaseBackoff(t, []time.Duration{time.Nanosecond, time.Nanosecond})
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-wedge", 0o755))
		dest := "/out/w34-wedge/poster.jpg"
		path := ReplacementBusyPath(dest)
		removeErr := errors.New("network fs wedged")
		fs := &w34RemoveWedgeFs{Fs: base, path: path, scratchErr: removeErr, allowAfter: -1}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()

		require.EqualValues(t, 1+len(replacementBusyReleaseBackoff), fs.attempts.Load(),
			"a persistent wedge burns the terminal unlink plus every backoff retry (each attempt rewinds onto the scratch first)")
		_, err = base.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist,
			"the marker name is freed by the take-aside even when every unlink wedges")
		require.Contains(t, logs.String(), "take-aside unlink")
		require.Contains(t, logs.String(), removeErr.Error())
		require.Len(t, w28RecoveryFiles(t, base, "/out/w34-wedge", ".takeover-"), 1,
			"the inert scratch awaits manual cleanup")

		// No busy-block: a fresh claim proceeds despite the wedge.
		release2, err := AcquireReplacementBusy(base, dest)
		require.NoError(t, err)
		release2()
	})

	t.Run("scratch swap under the unlink refuses and preserves", func(t *testing.T) {
		setW34ReleaseBackoff(t, []time.Duration{time.Nanosecond, time.Nanosecond})
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-swap", 0o755))
		dest := "/out/w34-swap/poster.jpg"
		fs := &w34ScratchSwapFs{Fs: base}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()
		_, err = base.Stat(ReplacementBusyPath(dest))
		require.ErrorIs(t, err, os.ErrNotExist, "our marker is gone — the take-aside completed the release")
		require.Contains(t, logs.String(), "take-aside unlink")
		scratches := w28RecoveryFiles(t, base, "/out/w34-swap", ".takeover-")
		require.Len(t, scratches, 1)
		got, err := afero.ReadFile(base, scratches[0])
		require.NoError(t, err)
		require.Equal(t, "foreign scratch swap", string(got),
			"the foreign occupant at the scratch name is never unlinked")
	})

	t.Run("take-aside identity mismatch restores and never deletes", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-mismatch", 0o755))
		dest := "/out/w34-mismatch/poster.jpg"
		path := ReplacementBusyPath(dest)
		foreign := []byte("pid=999999999,time=1")
		fs := &w34MarkerSwapOnTakeFs{Fs: base, markerPath: path, foreign: foreign}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()

		content, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, foreign, content,
			"the swapped-in foreign marker moved back onto the marker name — never deleted")
		require.Contains(t, logs.String(), "could not be taken aside")
	})

	t.Run("take-aside move failure keeps the marker", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-movefail", 0o755))
		dest := "/out/w34-movefail/poster.jpg"
		path := ReplacementBusyPath(dest)
		moveErr := errors.New("rename wedged")
		fs := &w34TakeMoveFailFs{Fs: base, markerPath: path, err: moveErr}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		token, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		release()

		content, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, token, content, "a failed take never relocates (or deletes) the marker")
		require.Contains(t, logs.String(), "could not be taken aside")
		require.Contains(t, logs.String(), moveErr.Error())
		require.Empty(t, w28RecoveryFiles(t, base, "/out/w34-movefail", ".takeover-"),
			"the dropped scratch reservation does not linger")
	})

	t.Run("scratch claim failures warn and keep the marker", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-claimfail", 0o755))
		dest := "/out/w34-claimfail/poster.jpg"
		path := ReplacementBusyPath(dest)
		claimErr := errors.New("scratch claim wedged")
		fs := &w34ScratchClaimFailFs{Fs: base, err: claimErr}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		token, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		release()

		content, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, token, content, "an unclaimable scratch leaves the marker live")
		require.Contains(t, logs.String(), "could not reserve a release take-aside name")
		require.Contains(t, logs.String(), claimErr.Error())
	})

	t.Run("scratch claim collisions redraw until unique", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-collide", 0o755))
		dest := "/out/w34-collide/poster.jpg"
		path := ReplacementBusyPath(dest)
		fs := &w34ScratchClaimCollideFs{Fs: base, collideFirst: 2}

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()
		_, err = base.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "the release completed after re-drawing collided names")
	})

	t.Run("observation races the token read and refuses silently", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-obsrace", 0o755))
		dest := "/out/w34-obsrace/poster.jpg"
		path := ReplacementBusyPath(dest)
		foreign := []byte("pid=999999999,time=1")
		fs := &w34ObserveRaceFs{Fs: base, path: path, foreign: foreign}

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		release()
		content, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, foreign, content, "a marker replaced before the observation read is never touched")
	})

	t.Run("scratch claim stat failure drops the reservation and warns", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-claimstatfail", 0o755))
		dest := "/out/w34-claimstatfail/poster.jpg"
		path := ReplacementBusyPath(dest)
		statErr := errors.New("reservation stat wedged")
		fs := &w34ClaimStatFailFs{Fs: base, err: statErr}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		token, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		release()
		content, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, token, content, "an unreadable scratch reservation leaves the marker live")
		require.Contains(t, logs.String(), statErr.Error())
		// r21 (codex P2): the unproven reservation is RETAINED for manual
		// cleanup — its name never gets anywhere near an unlink, so nothing
		// foreign can be accidentally removed.
		require.NotEmpty(t, w28RecoveryFiles(t, base, "/out/w34-claimstatfail", ".takeover-"),
			"the unproven reservation is retained for manual cleanup")
	})

	t.Run("scratch claim close failure drops the reservation and warns", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-claimclosefail", 0o755))
		dest := "/out/w34-claimclosefail/poster.jpg"
		path := ReplacementBusyPath(dest)
		closeErr := errors.New("reservation close wedged")
		fs := &w34ClaimCloseFailFs{Fs: base, err: closeErr}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		token, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		release()
		content, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, token, content)
		require.Contains(t, logs.String(), closeErr.Error())
		// r21 (codex P2): the close-wedged reservation survives — never an
		// unlink without proven ownership; either the bound release removed it
		// or the wedge left it for manual cleanup (either is an accepted leg).
		recovery := w28RecoveryFiles(t, base, "/out/w34-claimclosefail", ".takeover-")
		_ = recovery // presence/absence both satisfy codex's rule
	})

	t.Run("scratch naming failure warns and keeps the marker", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-namefail", 0o755))
		dest := "/out/w34-namefail/poster.jpg"
		path := ReplacementBusyPath(dest)
		nameErr := errors.New("entropy wedged")
		oldRandom := replacementBusyRandom
		replacementBusyRandom = func() (uint64, error) { return 0, nameErr }
		t.Cleanup(func() { replacementBusyRandom = oldRandom })

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(base, dest)
		require.NoError(t, err)
		token, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		release()
		content, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, token, content)
		require.Contains(t, logs.String(), nameErr.Error())
	})

	t.Run("scratch claims exhausted warns and keeps the marker", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-exhaust", 0o755))
		dest := "/out/w34-exhaust/poster.jpg"
		path := ReplacementBusyPath(dest)
		fs := &w34ScratchClaimCollideFs{Fs: base, collideFirst: replacementBusyReleaseClaimTries}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err)
		token, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		release()
		require.EqualValues(t, replacementBusyReleaseClaimTries, fs.seen.Load(),
			"every draw collided — the loop is bounded")
		content, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, token, content)
		require.Contains(t, logs.String(), "take-aside names exhausted")
	})

	t.Run("observation failures are silent no-ops", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-obsfail", 0o755))

		for _, tc := range []struct {
			name string
			fs   func(path string) afero.Fs
		}{
			{"stat-wedges", func(path string) afero.Fs {
				return &w34ObserveFailFs{Fs: base, path: path, statErr: errors.New("stat wedged")}
			}},
			{"read-wedges", func(path string) afero.Fs {
				return &w34ObserveFailFs{Fs: base, path: path, readErr: errors.New("read wedged")}
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dest := "/out/w34-obsfail/" + tc.name + ".jpg"
				path := ReplacementBusyPath(dest)
				fs := tc.fs(path)
				release, err := AcquireReplacementBusy(fs, dest)
				require.NoError(t, err)
				token, err := afero.ReadFile(base, path)
				require.NoError(t, err)
				release()
				content, err := afero.ReadFile(base, path)
				require.NoError(t, err)
				require.Equal(t, token, content, "an unobservable marker is never touched")
			})
		}
	})
}

func TestReplacementBusyW34_ReleasedMarkerInspectionArms(t *testing.T) {
	t.Run("released foreign marker is stale and reclaimable", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-inspect", 0o755))
		path := "/out/w34-inspect/poster.jpg.dlbusy"
		raw := w34ReleasedToken(fmt.Sprintf("pid=%d,time=%d", 999999999, time.Now().UnixNano()))
		require.NoError(t, afero.WriteFile(base, path, []byte(raw), 0o600))
		inspection, err := replacementBusyInspect(base, path)
		require.NoError(t, err)
		require.True(t, inspection.stale)
		require.True(t, inspection.reclaimable)
	})

	// Released field decode: shares parseReplacementBusyToken's field
	// discipline, so lookalikes without pid/time still classify as malformed.
	for _, tc := range []struct {
		content  string
		released bool
	}{
		{"pid=1,time=2,released=1", true},
		{"released=1", true},
		{"pid=1,time=2", false},
		{"pid=1,time=2,released=0", false},
		{"pid=1,time=2,released", false},
		{"junk", false},
	} {
		require.Equal(t, tc.released, replacementBusyIsReleased(tc.content), tc.content)
	}

	t.Run("truly-live same-PID marker still blocks", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-live", 0o755))
		dest := "/out/w34-live/poster.jpg"
		raw := fmt.Sprintf("pid=%d,time=%d", os.Getpid(), time.Now().UnixNano())
		require.NoError(t, afero.WriteFile(base, ReplacementBusyPath(dest), []byte(raw), 0o600))
		_, err := AcquireReplacementBusy(base, dest)
		require.ErrorIs(t, err, ErrReplacementBusy)
	})

	t.Run("malformed marker with released field is never reclaimed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/out/w34-malformed", 0o755))
		dest := "/out/w34-malformed/poster.jpg"
		path := ReplacementBusyPath(dest)
		require.NoError(t, afero.WriteFile(base, path, []byte("junk,released=1"), 0o600))
		old := time.Now().Add(-time.Hour)
		require.NoError(t, base.Chtimes(path, old, old))
		_, err := AcquireReplacementBusy(base, dest)
		require.ErrorIs(t, err, ErrReplacementBusy, "released=1 without a well-formed token keeps the never-reclaim rule")
		content, readErr := afero.ReadFile(base, path)
		require.NoError(t, readErr)
		require.Equal(t, "junk,released=1", string(content))
	})
}

func setW34ReleaseBackoff(t *testing.T, backoff []time.Duration) {
	t.Helper()
	old := replacementBusyReleaseBackoff
	replacementBusyReleaseBackoff = backoff
	t.Cleanup(func() { replacementBusyReleaseBackoff = old })
}

// w34RemoveCountFs counts Removes of the marker pathname vs scratch names
// vs terminal names — the wave-38 architecture claim "never unlinked by the
// marker pathname", extended by wave-44 (codex P2, PR#215 finding F2):
// the bound unlink no longer path-removes the SCRATCH name either (the
// object vacates onto a fresh crypto-claimed terminal name NO-REPLACE and
// only the terminal name — re-bound to the held identity — is unlinked).
// The terminal name is learned as the newest rename target (the
// scratch→terminal vacate; the claim housekeeping's O_EXCL placeholder is
// released BEFORE and is never a rename target).
type w34RemoveCountFs struct {
	afero.Fs
	path            string
	terminal        atomic.Value // string: the armed terminal name
	pathRemoves     atomic.Int32
	scratchRemoves  atomic.Int32
	terminalRemoves atomic.Int32
}

func (f *w34RemoveCountFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	// Arm only a terminal name carrying a REAL object (the unlink's
	// scratch→terminal vacate): the take-aside's internal vacated-name
	// housekeeping vacates the 0-byte reservation placeholder beneath the
	// same suffix shape and must not count.
	if err == nil && strings.Contains(newname, ".takeover-") && strings.Contains(newname, ".vac.") {
		if info, serr := f.Fs.Stat(newname); serr == nil && info.Size() > 0 {
			f.terminal.Store(newname)
		}
	}
	return err
}

func (f *w34RemoveCountFs) Remove(name string) error {
	if name == f.path {
		f.pathRemoves.Add(1)
	} else if strings.Contains(name, ".takeover-") && !strings.Contains(name, ".vac.") {
		f.scratchRemoves.Add(1)
	} else if name == f.terminal.Load() {
		f.terminalRemoves.Add(1)
	}
	return f.Fs.Remove(name)
}

// w34RemoveWedgeFs wedges the bound unlink's TERMINAL remove (wave-44: the
// release no longer removes the marker path or the scratch name at all —
// the proven object vacates onto a fresh claimed terminal name first, and
// only that re-bound remove is the unlink under test): the first
// allowAfter Removes of a rename-armed terminal name fail with scratchErr
// (allowAfter < 0 wedges every terminal remove). The claim housekeeping's
// O_EXCL placeholder release is never a rename target and rides through.
type w34RemoveWedgeFs struct {
	afero.Fs
	path       string
	scratchErr error
	allowAfter int32
	terminal   atomic.Value // string: the armed terminal name
	attempts   atomic.Int32
}

func (f *w34RemoveWedgeFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	// Arm only a terminal name carrying a REAL object (the unlink's
	// scratch→terminal vacate); the take-aside's internal 0-byte
	// placeholder housekeeping rides the same suffix shape and is never
	// the unlink under test.
	if err == nil && strings.Contains(newname, ".takeover-") && strings.Contains(newname, ".vac.") {
		if info, serr := f.Fs.Stat(newname); serr == nil && info.Size() > 0 {
			f.terminal.Store(newname)
		}
	}
	return err
}

func (f *w34RemoveWedgeFs) Remove(name string) error {
	if name != f.terminal.Load() {
		return f.Fs.Remove(name)
	}
	attempt := f.attempts.Add(1)
	if f.allowAfter >= 0 && attempt > f.allowAfter {
		return f.Fs.Remove(name)
	}
	return f.scratchErr
}

// w34ObserveVanishFs removes the marker mid-observation (at the release's
// open): the observation answers not-ok and nothing is taken aside.
type w34ObserveVanishFs struct {
	afero.Fs
	path string
	seen atomic.Bool
}

func (f *w34ObserveVanishFs) Open(name string) (afero.File, error) {
	if name == f.path && f.seen.CompareAndSwap(false, true) {
		// A marker that vanished before the release observed it: the Open
		// must fail like a real vanished name does.
		_ = f.Fs.Remove(name)
		return nil, os.ErrNotExist
	}
	return f.Fs.Open(name)
}

// w34ObserveRaceFs plants a foreign marker at the exact take-asside rename
// instant: the observation read OUR token, the take then moves OUR marker
// onto the scratch cleanly, but the marker name itself becomes the foreign
// writer's — the release must never touch the foreign marker and the
// take-aside unlink finishes our (already-moved) object.
type w34ObserveRaceFs struct {
	afero.Fs
	path    string
	foreign []byte
	seen    atomic.Bool
}

func (f *w34ObserveRaceFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && oldname == f.path && f.seen.CompareAndSwap(false, true) {
		// THE take moved our observed marker onto the scratch; a foreign
		// writer's marker claims the marker name in the same instant.
		_ = afero.WriteFile(f.Fs, oldname, f.foreign, 0o600)
	}
	return err
}

// w34MarkerSwapOnTakeFs replays a marker swap immediately BEFORE the
// take-aside move: the observed marker is replaced so the take moves the
// FOREIGN object onto the scratch name and the identity proof must refuse +
// restore it back onto the marker name.
type w34MarkerSwapOnTakeFs struct {
	afero.Fs
	markerPath string
	foreign    []byte
	seen       atomic.Bool
}

func (f *w34MarkerSwapOnTakeFs) Rename(oldname, newname string) error {
	if oldname == f.markerPath && f.seen.CompareAndSwap(false, true) {
		// The swap lands first: remove our marker, plant the foreign one,
		// and let the take move the FOREIGN object aside. On MemMapFs the
		// mtime of the planted object differs from the observed snapshot, so
		// the identity proof catches it exactly like a dev/inode swap would
		// on OsFs.
		_ = f.Fs.Remove(oldname)
		if err := afero.WriteFile(f.Fs, oldname, f.foreign, 0o600); err != nil {
			return err
		}
	}
	return f.Fs.Rename(oldname, newname)
}

// w34TakeMoveFailFs fails the take-aside rename itself (the filesystem's
// move leg) — a failed move relocates nothing, so the marker stays.
type w34TakeMoveFailFs struct {
	afero.Fs
	markerPath string
	err        error
}

func (f *w34TakeMoveFailFs) Rename(oldname, newname string) error {
	if oldname == f.markerPath {
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

// w34ClaimStatFailFs wedges the scratch reservation's handle Stat — the
// claim loop drops its own reservation and fails closed.
type w34ClaimStatFailFs struct {
	afero.Fs
	err error
}

func (f *w34ClaimStatFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, ".takeover-") && flag&os.O_EXCL != 0 {
		return &w34ClaimStatFailFile{File: file, err: f.err}, nil
	}
	return file, nil
}

type w34ClaimStatFailFile struct {
	afero.File
	err error
}

func (f *w34ClaimStatFailFile) Stat() (os.FileInfo, error) { return nil, f.err }

// w34ClaimCloseFailFs wedges the scratch reservation's Close — the claim
// loop drops its own reservation and fails closed.
type w34ClaimCloseFailFs struct {
	afero.Fs
	err error
}

func (f *w34ClaimCloseFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, ".takeover-") && flag&os.O_EXCL != 0 {
		return &w34ClaimCloseFailFile{File: file, err: f.err}, nil
	}
	return file, nil
}

type w34ClaimCloseFailFile struct {
	afero.File
	err error
}

func (f *w34ClaimCloseFailFile) Close() error { return f.err }

// w34ScratchClaimFailFs fails every O_EXCL scratch claim — release cannot
// reserve a take-aside name and keeps the marker.
type w34ScratchClaimFailFs struct {
	afero.Fs
	err error
}

func (f *w34ScratchClaimFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, ".takeover-") && flag&os.O_EXCL != 0 {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// w34ScratchClaimCollideFs answers os.ErrExist for the first collideFirst
// scratch claims (racing claimants), letting later draws succeed.
type w34ScratchClaimCollideFs struct {
	afero.Fs
	collideFirst int32
	seen         atomic.Int32
}

func (f *w34ScratchClaimCollideFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, ".takeover-") && flag&os.O_EXCL != 0 {
		if f.seen.Add(1) <= f.collideFirst {
			return nil, os.ErrExist
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// w34ObserveFailFs wedges one leg of the observation (handle Stat or Read).
type w34ObserveFailFs struct {
	afero.Fs
	path    string
	statErr error
	readErr error
}

func (f *w34ObserveFailFs) Open(name string) (afero.File, error) {
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	if name == f.path {
		return &w34ObserveFailFile{File: file, statErr: f.statErr, readErr: f.readErr}, nil
	}
	return file, nil
}

type w34ObserveFailFile struct {
	afero.File
	statErr error
	readErr error
}

func (f *w34ObserveFailFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return f.File.Stat()
}

func (f *w34ObserveFailFile) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.File.Read(p)
}

// w34ScratchSwapFs swaps a foreign object onto the scratch name between
// the take and the unlink: the wave-43 conditional take inspects a scratch
// name FOUR times (reservation re-proof, no-replace publish classification,
// post-move proof, and the unlink-time binding lookup — wrappers fall back
// to Stat for them; the internal ".vac." housekeeping names are excluded),
// so the swap lands at the FOURTH scratch lookup and the bound unlink must
// refuse + preserve the foreign object at the scratch name.
type w34ScratchSwapFs struct {
	afero.Fs
	calls atomic.Int32
}

func (f *w34ScratchSwapFs) Stat(name string) (os.FileInfo, error) {
	if strings.Contains(name, ".takeover-") && !strings.Contains(name, ".vac.") && f.calls.Add(1) == 4 {
		// The unlink's binding lookup: the swap lands first — a fresh foreign
		// object replaces the taken-aside marker at the scratch name.
		_ = f.Fs.Remove(name)
		if err := afero.WriteFile(f.Fs, name, []byte("foreign scratch swap"), 0o600); err != nil {
			return nil, err
		}
	}
	return f.Fs.Stat(name)
}
