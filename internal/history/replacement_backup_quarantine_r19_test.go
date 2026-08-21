package history

// POSTER-WRITE-HARDENING wave-r19 (codex P2, PR#215 finding F3) —
// releaseBackupQuarantineReservation closed its verify→Remove window by
// carrying the reservation's identity from claim time and re-proving it
// TWICE at unlink adjacency (SameFile-lstat pair) before the Remove. These
// tests drive every refusal leg of that pair: an indeterminate first proof,
// and a vanish / indeterminate / foreign swap landing between the two proofs
// — each retains the occupant byte-intact with a warn, never a pathname
// Remove of an unproven object. The happy path (both proofs equal, Remove
// runs) is exercised by the wave-42 handoff failure legs.

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// wR19ReleaseFs scripts the Nth no-follow Lstat of the quarantine name so a
// racer's vanish / indeterminate answer / foreign swap can be replayed inside
// the SameFile-lstat pair's verify→unlink window. Every other name passes
// through to the underlying filesystem.
type wR19ReleaseFs struct {
	afero.Fs
	quarantine string
	script     func(call int) (os.FileInfo, error)
	calls      int
}

func (f *wR19ReleaseFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.quarantine {
		f.calls++
		info, err := f.script(f.calls)
		return info, true, err
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// r19QuarFixture creates a 0-byte quarantine placeholder at name and returns
// its captured identity (the claim the caller carries from claim time).
func r19QuarFixture(t *testing.T, fs afero.Fs, name string) os.FileInfo {
	t.Helper()
	fh, err := fs.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	info, err := fh.Stat()
	require.NoError(t, err)
	require.NoError(t, fh.Close())
	return info
}

func r19CaptureLogs(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var logs bytes.Buffer
	restore := logging.SetOutput(&logs)
	return &logs, func() { restore() }
}

// An indeterminate FIRST proof (the reservation could not be inspected
// before its release) refuses the cleanup with a warn — the occupant is left
// byte-intact, never a pathname Remove of an unproven object.
func TestReleaseBackupQuarantineR19_FirstProofIndeterminateRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	const quar = "/r19-ind1/q.dlq.token"
	require.NoError(t, base.MkdirAll("/r19-ind1", 0o755))
	claim := r19QuarFixture(t, base, quar)
	sentinel := errors.New("r19 first lookup wedged")
	fs := &wR19ReleaseFs{Fs: base, quarantine: quar, script: func(call int) (os.FileInfo, error) {
		return nil, sentinel // the first (and only) proof is indeterminate
	}}
	logs, restore := r19CaptureLogs(t)
	t.Cleanup(restore)

	releaseBackupQuarantineReservation(fs, quar, claim)
	require.Contains(t, logs.String(), "could not be inspected before its release",
		"the indeterminate first proof refused the cleanup with a warn")
	info, err := base.Stat(quar)
	require.NoError(t, err, "the unproven occupant is retained byte-intact")
	require.Zero(t, info.Size())
}

// A vanish landing between the two proofs completes the release (the name is
// already free — nothing foreign was ever at risk).
func TestReleaseBackupQuarantineR19_SecondProofVanishedReleases(t *testing.T) {
	base := afero.NewMemMapFs()
	const quar = "/r19-van2/q.dlq.token"
	require.NoError(t, base.MkdirAll("/r19-van2", 0o755))
	claim := r19QuarFixture(t, base, quar)
	fs := &wR19ReleaseFs{Fs: base, quarantine: quar, script: func(call int) (os.FileInfo, error) {
		if call == 1 {
			return claim, nil // first proof passes
		}
		// the placeholder vanished between the proofs through nobody's unlink
		require.NoError(t, base.Remove(quar))
		return nil, afero.ErrFileNotFound
	}}
	logs, restore := r19CaptureLogs(t)
	t.Cleanup(restore)

	releaseBackupQuarantineReservation(fs, quar, claim)
	require.Empty(t, logs.String(), "a vanish between the proofs completed the release — no warn")
	_, err := base.Stat(quar)
	require.ErrorIs(t, err, os.ErrNotExist, "the vanished name needs no unlink")
}

// An indeterminate SECOND proof refuses the cleanup with a warn — the
// occupant is left byte-intact.
func TestReleaseBackupQuarantineR19_SecondProofIndeterminateRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	const quar = "/r19-ind2/q.dlq.token"
	require.NoError(t, base.MkdirAll("/r19-ind2", 0o755))
	claim := r19QuarFixture(t, base, quar)
	sentinel := errors.New("r19 re-proof wedged")
	fs := &wR19ReleaseFs{Fs: base, quarantine: quar, script: func(call int) (os.FileInfo, error) {
		if call == 1 {
			return claim, nil
		}
		return nil, sentinel // indeterminate at the adjacency re-proof
	}}
	logs, restore := r19CaptureLogs(t)
	t.Cleanup(restore)

	releaseBackupQuarantineReservation(fs, quar, claim)
	require.Contains(t, logs.String(), "refused at the adjacency re-proof",
		"the indeterminate second proof refused the cleanup with a warn")
	info, err := base.Stat(quar)
	require.NoError(t, err, "the unproven occupant is retained byte-intact")
	require.Zero(t, info.Size())
}

// A foreign swap landing between the two proofs refuses the cleanup with a
// warn — the swapped occupant is never unlinked by this flow.
func TestReleaseBackupQuarantineR19_SecondProofForeignSwapRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	const quar = "/r19-swap2/q.dlq.token"
	require.NoError(t, base.MkdirAll("/r19-swap2", 0o755))
	claim := r19QuarFixture(t, base, quar)
	// A real foreign occupant (non-zero size) swapped onto the name between
	// the first proof and the adjacency re-proof.
	foreign := []byte("foreign occupant between the proofs")
	fs := &wR19ReleaseFs{Fs: base, quarantine: quar, script: func(call int) (os.FileInfo, error) {
		if call == 1 {
			return claim, nil // first proof: still our 0-byte placeholder
		}
		// a foreign writer swapped the placeholder for its own occupant
		require.NoError(t, base.Remove(quar))
		require.NoError(t, afero.WriteFile(base, quar, foreign, 0o600))
		return base.Stat(quar)
	}}
	logs, restore := r19CaptureLogs(t)
	t.Cleanup(restore)

	releaseBackupQuarantineReservation(fs, quar, claim)
	require.Contains(t, logs.String(), "changed identity between the proof and the unlink",
		"the foreign swap between the proofs refused the cleanup with a warn")
	got, err := afero.ReadFile(base, quar)
	require.NoError(t, err, "the foreign occupant is retained byte-intact")
	require.Equal(t, foreign, got)
}
