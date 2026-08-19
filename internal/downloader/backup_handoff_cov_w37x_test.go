package downloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// W37x: releaseClaimedReservation's proven-ours arm must actually release the
// placeholder (covers the Remove at the top leg of the switch).
func TestReleaseClaimedReservationW37X_ReleasesProvenPlaceholder(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")

	// Claim exactly as the production claim does.
	f, err := fs.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	claim, err := fs.Stat(backup)
	require.NoError(t, err)

	releaseClaimedReservation(fs, backup, claim)

	_, err = os.Lstat(backup)
	require.True(t, os.IsNotExist(err), "proven placeholder released")
}

// W37x: vanished reservation is a silent no-op (no error surface, no log).
func TestReleaseClaimedReservationW37X_VanishedIsNoOp(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "gone.dlbak.0123456789abcdef")
	releaseClaimedReservation(fs, backup, nil)
}

// W37x: foreign occupant is never removed.
func TestReleaseClaimedReservationW37X_ForeignOccupantPreserved(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")
	require.NoError(t, os.WriteFile(backup, []byte("foreign-bytes"), 0o644))

	releaseClaimedReservation(fs, backup, nil)

	got, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "foreign-bytes", string(got), "foreign occupant byte-intact")
}
