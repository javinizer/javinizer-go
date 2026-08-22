package downloader

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Wave-63 (codex P2, PR#215 finding F1): size+mtime are forgeable — a
// substituted file with matching size+mtime rode the wave-62 gate and was
// streamed into the media directory. The rollback copy now tees the stream
// through sha256 and compares to the armed digest; a content mismatch refuses
// typed ErrTakeAsideForeign before the publish (dest untouched, foreign
// preserved). A matching digest restores as before. An absent sha (legacy)
// keeps the wave-62 size+mtime posture — a forged same-size+mtime file rides
// through exactly like pre-wave-63.

func TestCopyBackupToDestBoundFacts_W63_SHA256MismatchRefuses(t *testing.T) {
	fs := afero.NewMemMapFs()
	original := []byte("AAAAAAAAAAAAAAAA")
	forged := []byte("BBBBBBBBBBBBBBBB") // same length, different content
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("installed"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/backup", original, 0o644))
	info, err := fs.Stat("/backup")
	require.NoError(t, err)
	mtime := info.ModTime()
	facts := models.ReplacementBackupFacts{Size: info.Size(), ModUnix: mtime.Unix(), SHA256: w63ShaHex(original)}

	// Forge: same length, same mtime — the forgeable shape that rode wave-62.
	require.NoError(t, afero.WriteFile(fs, "/backup", forged, 0o644))
	require.NoError(t, fs.Chtimes("/backup", mtime, mtime))

	_, err = copyBackupToDestBoundFacts(fs, "/backup", "/dest", &facts)
	require.ErrorIs(t, err, fsutil.ErrTakeAsideForeign, "the sha256 mismatch refuses the forged backup before publish")
	require.Contains(t, err.Error(), "sha256 mismatch")
	require.Equal(t, "installed", string(mustReadDownloaderW7(t, fs, "/dest")), "dest stays untouched")
	require.Equal(t, "BBBBBBBBBBBBBBBB", string(mustReadDownloaderW7(t, fs, "/backup")), "the foreign occupant is preserved byte-intact")
}

func TestCopyBackupToDestBoundFacts_W63_CorrectSHA256Restores(t *testing.T) {
	fs := afero.NewMemMapFs()
	backup := []byte("the original set-aside")
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("installed"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/backup", backup, 0o644))
	info, err := fs.Stat("/backup")
	require.NoError(t, err)
	facts := models.ReplacementBackupFacts{Size: info.Size(), ModUnix: info.ModTime().Unix(), SHA256: w63ShaHex(backup)}

	_, err = copyBackupToDestBoundFacts(fs, "/backup", "/dest", &facts)
	require.NoError(t, err)
	require.Equal(t, "the original set-aside", string(mustReadDownloaderW7(t, fs, "/dest")), "the backup restored over the destination")
}

func TestCopyBackupToDestBoundFacts_W63_AbsentSHARidesWave62(t *testing.T) {
	fs := afero.NewMemMapFs()
	original := []byte("AAAAAAAAAAAAAAAA")
	forged := []byte("BBBBBBBBBBBBBBBB") // same length, same mtime — forgeable
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("installed"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/backup", original, 0o644))
	info, err := fs.Stat("/backup")
	require.NoError(t, err)
	mtime := info.ModTime()
	// Legacy facts: size+mtime stamped, NO sha (armed before wave-63).
	facts := models.ReplacementBackupFacts{Size: info.Size(), ModUnix: mtime.Unix()}

	require.NoError(t, afero.WriteFile(fs, "/backup", forged, 0o644))
	require.NoError(t, fs.Chtimes("/backup", mtime, mtime))

	_, err = copyBackupToDestBoundFacts(fs, "/backup", "/dest", &facts)
	require.NoError(t, err, "absent sha keeps the wave-62 size+mtime posture — a forged same-size+mtime file rides through")
	require.Equal(t, "BBBBBBBBBBBBBBBB", string(mustReadDownloaderW7(t, fs, "/dest")), "the copy proceeded with no sha gate")
}
