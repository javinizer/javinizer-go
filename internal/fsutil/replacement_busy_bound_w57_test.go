package fsutil

// POSTER-WRITE-HARDENING wave-57B (codex P2, PR#215) — the claim-cleanup
// bound-unlink chain (discardBusyMarkerClaim): after the predictably-named
// marker is re-proven ours at the cleanup lookup, the verified object vacates
// onto a fresh claimed terminal name (claimTakeAsideVacName), is re-bound to
// the claimed identity through TakeAside's Prove, and ONLY that terminal name
// is unlinked (BoundAside.Unlink). The pre-wave-57B shape unlinked the
// predictable marker name by pathname — a racer swapping a foreign marker
// onto the canonical name inside the verify→Remove window had ITS bytes
// deleted. The bound chain closes that window end to end; every doubt
// preserves bytes and refuses closed.
//
// This suite drives each ERROR leg of the new chain from the claim-cleanup
// (AcquireReplacementBusy → discardBusyMarkerClaim):
//
//   - the terminal-name reservation failure (claimTakeAsideVacName): the
//     vacate-name entropy draw refuses, the marker is left byte-intact at the
//     predictable name, and the original claim failure surfaces;
//   - the TakeAside Prove failure: a foreign swap landing on the marker name
//     between the cleanup's asideLstat and the no-replace src→scratch publish
//     moves the PLANT onto the scratch name, the post-move Prove identity
//     check refuses it typed, the preservational rewind restores the plant
//     onto the marker name byte-intact, and the claim-cleanup warns and
//     leaves it;
//   - the bound unlink's terminal-remove failure: a wedged terminal Remove
//     rewinds the object onto the scratch name byte-intact (rerideTerminal),
//     the claim-cleanup warns and preserves it, and the original claim
//     failure surfaces — never a silent strand.
//
// Every leg keeps the foreign/plant bytes byte-intact: the bound chain never
// deletes a name it cannot prove, exactly the property the pre-wave-57B
// pathname Remove could not offer once a swap raced the verify→Remove gap.

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

// w57ProveSwapFs replays a foreign swap onto the marker name BETWEEN the
// claim-cleanup's asideLstat (which proved the marker was ours) and the
// TakeAside's no-replace src→scratch publish: the publish rename moves the
// PLANT onto the fresh vacated scratch name, the post-move Prove identity
// check catches it (dev/inode diverges from the claimed marker on the real
// OsFs), and the preservational rewind restores the plant onto the marker
// name byte-intact — never deleted. Mirrors w59SwapOnVacateRenameFs's shape,
// keyed on the marker path (the claim-cleanup's src) instead of the scratch.
type w57ProveSwapFs struct {
	afero.Fs
	marker string
	plant  []byte
	done   atomic.Bool
}

func (f *w57ProveSwapFs) Rename(oldname, newname string) error {
	if !f.done.Load() && oldname == f.marker && strings.Contains(newname, ".vac.") {
		f.done.Store(true)
		foreign := f.marker + ".foreign"
		if err := afero.WriteFile(f.Fs, foreign, f.plant, 0o600); err != nil {
			return err
		}
		if err := f.Fs.Rename(foreign, oldname); err != nil { // foreign now at the marker name
			return err
		}
	}
	return f.Fs.Rename(oldname, newname)
}

// The claim-cleanup's first bound-unlink step reserves a fresh vacated
// terminal name (claimTakeAsideVacName). A failed entropy draw (the
// vacate-name token) refuses the cleanup closed: the marker is left
// byte-intact at the predictable name for manual cleanup, and the original
// claim failure still surfaces — never a pathname Remove of a name the flow
// can no longer prove.
//
// (Distinct from w14A's incidental reservation-Close coverage: this drives
// the token-draw refusal deterministically through the entropy seam, so the
// claim-cleanup's cerr leg is exercised by a focused, intentional test.)
func TestReplacementBusyW57_ClaimCleanupVacNameReserveFailureKeepsMarker(t *testing.T) {
	base, dir := w47OSDir(t)
	dest := filepath.Join(dir, "poster.jpg")
	path := ReplacementBusyPath(dest)
	writeErr := errors.New("claim write wedged")
	fs := &w47ClaimWedgeFs{Fs: base, writeErr: writeErr}

	// Wedge the vacate-name entropy seam so claimTakeAsideVacName cannot draw
	// a terminal token — the claim cleanup's cerr leg.
	prev := takeAsideVacRandReader
	takeAsideVacRandReader = &w43FailReader{err: errors.New("vacate entropy wedged")}
	t.Cleanup(func() { takeAsideVacRandReader = prev })

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, writeErr, "the original claim write failure still surfaces")
	require.Contains(t, logs.String(), "could not reserve its terminal name",
		"the claim cleanup refused closed when its terminal-name reservation failed")
	_, statErr := base.Stat(path)
	require.NoError(t, statErr,
		"the marker is left byte-intact at the predictable name for manual cleanup — never a pathname Remove")
}

// A foreign swap landing on the marker name inside the claim-cleanup's
// asideLstat→TakeAside window fails the post-move Prove identity check: the
// take-aside refuses typed, the foreign plant is rewound onto the marker name
// byte-intact (never deleted), and the claim-cleanup warns and leaves it —
// the original claim failure still surfaces. Pre-wave-57B the pathname Remove
// ran after a swap the asideLstat could not bind and deleted the foreign
// marker.
func TestReplacementBusyW57_ClaimCleanupTakeAsideProveFailureKeepsOccupant(t *testing.T) {
	base, dir := w47OSDir(t)
	dest := filepath.Join(dir, "poster.jpg")
	path := ReplacementBusyPath(dest)
	plant := w47PlantBytes()
	writeErr := errors.New("claim write wedged")
	swap := &w57ProveSwapFs{Fs: base, marker: path, plant: plant}
	fs := &w47ClaimWedgeFs{Fs: swap, writeErr: writeErr}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, writeErr, "the original claim write failure still surfaces")
	require.True(t, swap.done.Load(),
		"the src→scratch publish wedge must have fired inside TakeAside")
	require.Contains(t, logs.String(), "take-aside failed",
		"the claim cleanup warned when the take-aside's Prove identity check refused the swapped occupant")
	// The foreign plant is rewound onto the marker name byte-intact — the
	// bound chain never deleted it.
	got, gerr := afero.ReadFile(base, path)
	require.NoError(t, gerr, "the swapped occupant is preserved at the marker name")
	require.Equal(t, plant, got, "the foreign plant keeps its bytes byte-intact")
}

// The claim-cleanup's bound unlink (BoundAside.Unlink) removes the proven
// marker only through a fresh claimed terminal name. A wedged terminal remove
// refuses the unlink: the object is rewound onto the scratch name byte-intact
// (rerideTerminal), the claim-cleanup warns and preserves it, and the
// original claim failure still surfaces — never a silent strand.
func TestReplacementBusyW57_ClaimCleanupUnlinkFailurePreservesOccupant(t *testing.T) {
	base, dir := w47OSDir(t)
	dest := filepath.Join(dir, "poster.jpg")
	path := ReplacementBusyPath(dest)
	writeErr := errors.New("claim write wedged")
	removeErr := errors.New("terminal unlink wedged")
	fs := &w59TerminalRemoveFailFs{
		Fs:   &w47ClaimWedgeFs{Fs: base, writeErr: writeErr},
		err:  removeErr,
		fail: 2, // 1 = dropVacatedReservation's bound cleanup (F2), 2 = hold.Unlink's marker terminal
	}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, writeErr, "the original claim write failure still surfaces")
	require.Equal(t, 2, fs.fails,
		"both bound unlinks' terminal removes must have been armed and refused")
	require.Contains(t, logs.String(), "could not be bound-unlinked",
		"the claim cleanup warned when the vacated reservation's bound cleanup refused (F2)")
	require.Contains(t, logs.String(), "unlink refused",
		"the claim cleanup warned when the marker's bound unlink refused")
	// The marker was taken aside onto the scratch name and the unlink
	// refused — the predictable marker name is free (the take-aside moved
	// it off), and the object survives byte-intact at the inert scratch
	// sibling for manual cleanup.
	_, statErr := base.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"the predictable marker name is free — the take-aside moved the object off it")
	var vacated string
	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".vac.") {
			vacated = filepath.Join(dir, e.Name())
		}
	}
	require.NotEmpty(t, vacated,
		"the object survives byte-intact at the inert scratch sibling for manual cleanup")
}
