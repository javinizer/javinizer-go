package history

// POSTER-WRITE-HARDENING wave-63 (codex P2 PR#215) — BackupSize/BackupModUnix
// are forgeable: a foreign file of the same length with a coerced unix-second
// mtime impersonates the owned set-aside and used to land at dest. The
// arm-time BackupSHA256 now binds the restore copy to the exact bytes
// journaled: a content mismatch refuses before the publish (dest untouched,
// entry live, foreign preserved), and a content match restores as before.
// Legacy entries armed before this wave carry no sha and keep the wave-25
// size+mtime+dev/ino posture (pinned by the w25 suite).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func w63ShaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// w63ArmedOp arms an entry with full wave-63 facts (size+mtime+sha256) stamped
// from the on-disk backup, mirroring the downloader's arm-time capture.
func w63ArmedOp(t *testing.T, fs afero.Fs, repo *p3OpRepo, movieID string, destBytes, backupBytes []byte) (*models.BatchFileOperation, string, string) {
	t.Helper()
	dir := "/dst/" + movieID
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak.aaaaaaaaaaaaaaaa"
	require.NoError(t, fs.MkdirAll(dir, config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, destBytes, config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, backupBytes, config.FilePerm))
	info, err := lstatRestoreSource(fs, backup)
	require.NoError(t, err)
	op := &models.BatchFileOperation{
		BatchJobID:    "job-" + movieID,
		MovieID:       movieID,
		OriginalPath:  "/src/" + movieID + ".mkv",
		NewPath:       dir + "/" + movieID + ".mkv",
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination:   dest,
				Backup:        backup,
				DestSeq:       1,
				Installed:     true,
				BackupSize:    info.Size(),
				BackupModUnix: info.ModTime().Unix(),
				BackupSHA256:  w63ShaHex(backupBytes),
			}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op, dest, backup
}

// The finding's central case: a foreign file swapped onto the backup name
// with the SAME length and a coerced same-second mtime passes the wave-25
// size+mtime gate, but the sha256 mismatch refuses before any byte reaches
// dest — dest untouched, foreign occupant preserved, entry stays live.
func TestRestoreReplacementJournalW63_SHA256MismatchRefuses(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	original := []byte("AAAAAAAAAAAAAAAA")
	forged := []byte("BBBBBBBBBBBBBBBB") // same length, different content
	op, dest, backup := w63ArmedOp(t, fs, repo, "W63M", []byte("new poster bytes!"), original)

	// Forge the substitution: same length, same mtime — the forgeable shape.
	info, err := lstatRestoreSource(fs, backup)
	require.NoError(t, err)
	mtime := info.ModTime()
	require.NoError(t, afero.WriteFile(fs, backup, forged, config.FilePerm))
	require.NoError(t, fs.Chtimes(backup, mtime, mtime))

	_, err = NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRestoreSourceRefused, "the sha256 mismatch refuses the substituted backup before publish")
	require.Contains(t, err.Error(), "backup sha256 mismatch")

	require.Equal(t, "new poster bytes!", string(mustRead2(t, fs, dest)),
		"dest stays untouched — the substituted bytes never landed")
	require.Equal(t, "BBBBBBBBBBBBBBBB", string(mustRead2(t, fs, backup)),
		"the foreign occupant at the backup name is preserved byte-intact")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the journal entry stays live")
	require.False(t, entries[0].RestorePending, "no copy published — no cleanup marker is warranted")
}

// A matching sha256 restores the backup over the destination and consumes the
// entry exactly like the pre-wave-63 flow.
func TestRestoreReplacementJournalW63_CorrectSHA256Restores(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	backupBytes := []byte("the original set-aside")
	op, dest, backup := w63ArmedOp(t, fs, repo, "W63C", []byte("new poster bytes!"), backupBytes)

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "the original set-aside", string(mustRead2(t, fs, dest)),
		"backup bytes restored over the destination")
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "the verified-owned backup was unlinked")
	require.Empty(t, w25JournalEntries(t, repo, op.ID), "the journaled entry was consumed")
}
