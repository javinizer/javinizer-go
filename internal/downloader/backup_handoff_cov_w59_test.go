package downloader

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Wave-59: the adjacency reproofs in releaseClaimedReservation. The outer
// overwriteBackupReservationStillOurs proof is a SNAPSHOT — wave-59 re-proves
// the reservation identity at Lstat adjacency to the bound unlink TWICE more
// (first reproof, second reproof) so a racer that swaps the reservation
// between the proof and the unlink — or a wedged lookup — is refused and the
// occupant is left byte-intact. The happy path (both reproofs pass → Remove)
// is already covered by TestReleaseClaimedReservationW37X_ReleasesProvenPlaceholder
// (the count-15 leg); these cover each reproof's REFUSAL arms:
//   - first reproof indeterminate (Lstat error)
//   - first reproof mismatch (foreign occupant)
//   - second reproof indeterminate (Lstat error)
//   - second reproof mismatch (foreign occupant)
//
// w59ReproofLstatFs arms a single Lstat ordinal of the reservation name:
//   - #1: overwriteBackupReservationStillOurs (the outer proof, must pass)
//   - #2: the first adjacency reproof
//   - #3: the second adjacency reproof
//
// so the armed reproof refuses while the earlier proofs still pass. The prior
// w59 attempt planted the foreign occupant BEFORE the call, which made the
// outer proof itself refuse (the line-178 leg) and never reached these
// reproof arms at all.

func TestReleaseClaimedReservationW59_FirstReproofIndeterminatePreserves(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")

	f, err := fs.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	claim, err := fs.Stat(backup)
	require.NoError(t, err)

	// Fail the FIRST adjacency reproof's Lstat (ordinal #2): the outer proof
	// (#1) still sees the claimed 0-byte placeholder, but the reproof can no
	// longer prove the identity at adjacency — indeterminate, so the bound
	// unlink refuses and the placeholder is left byte-intact.
	wfs := &w59ReproofLstatFs{
		Fs:      fs,
		victim:  backup,
		failAt:  2,
		failErr: errors.New("simulated indeterminate adjacency lookup"),
	}
	releaseClaimedReservation(wfs, backup, claim)

	got, err := os.ReadFile(backup)
	require.NoError(t, err, "the reservation placeholder survives the indeterminate refusal")
	require.Empty(t, got, "nothing was written and nothing was unlinked")
}

func TestReleaseClaimedReservationW59_FirstReproofMismatchPreserves(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")

	f, err := fs.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	claim, err := fs.Stat(backup)
	require.NoError(t, err)

	// Plant a FOREIGN occupant at the FIRST adjacency reproof (ordinal #2):
	// the outer proof (#1) still names the claimed placeholder, but the
	// reproof sees a size-divergent object — mismatch, refuse + preserve.
	wfs := &w59ReproofLstatFs{
		Fs:     fs,
		victim: backup,
		swapAt: 2,
		swap:   []byte("foreign-occupant"),
	}
	releaseClaimedReservation(wfs, backup, claim)

	got, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "foreign-occupant", string(got), "the foreign occupant survives the reproof refusal")
}

func TestReleaseClaimedReservationW59_SecondReproofIndeterminatePreserves(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")

	f, err := fs.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	claim, err := fs.Stat(backup)
	require.NoError(t, err)

	// The first reproof (#2) passes; the SECOND adjacency reproof's Lstat
	// (ordinal #3) is indeterminate — refuse at the second proof, preserve.
	wfs := &w59ReproofLstatFs{
		Fs:      fs,
		victim:  backup,
		failAt:  3,
		failErr: errors.New("simulated indeterminate second adjacency lookup"),
	}
	releaseClaimedReservation(wfs, backup, claim)

	got, err := os.ReadFile(backup)
	require.NoError(t, err, "the reservation placeholder survives the second indeterminate refusal")
	require.Empty(t, got, "nothing was written and nothing was unlinked")
}

func TestReleaseClaimedReservationW59_SecondReproofMismatchPreserves(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")

	f, err := fs.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	claim, err := fs.Stat(backup)
	require.NoError(t, err)

	// The first reproof (#2) passes; the SECOND adjacency reproof (#3) sees a
	// foreign occupant — size diverges from the claim, refuse + preserve.
	wfs := &w59ReproofLstatFs{
		Fs:     fs,
		victim: backup,
		swapAt: 3,
		swap:   []byte("foreign-occupant-2"),
	}
	releaseClaimedReservation(wfs, backup, claim)

	got, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "foreign-occupant-2", string(got), "the foreign occupant survives the second reproof refusal")
}

// w59ReproofLstatFs wraps an Fs and mutates the Lstat answer for a single
// victim name at a specific call ordinal. releaseClaimedReservation issues
// three Lstats of the reservation name: #1 the outer proof
// (overwriteBackupReservationStillOurs), #2 the first adjacency reproof, #3
// the second adjacency reproof. A test arms failAt (return err on that
// ordinal — indeterminate lookup) OR swapAt (write foreign bytes before that
// ordinal's Lstat returns — foreign-occupant mismatch) so the armed reproof
// refuses and preserves the occupant while the earlier proofs still pass.
// Every other operation delegates to the embedded Fs.
type w59ReproofLstatFs struct {
	afero.Fs
	victim  string
	calls   int
	failAt  int
	failErr error
	swapAt  int
	swap    []byte
}

func (f *w59ReproofLstatFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.victim {
		f.calls++
		if f.failAt > 0 && f.calls == f.failAt {
			return nil, false, f.failErr
		}
		if f.swapAt > 0 && f.calls == f.swapAt {
			if werr := afero.WriteFile(f.Fs, name, f.swap, 0o644); werr != nil {
				return nil, false, werr
			}
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}
