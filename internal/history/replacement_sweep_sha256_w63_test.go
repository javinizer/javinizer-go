package history

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Wave-63 (codex P2, PR#215 finding F2): an owned backup replaced by
// same-size+same-mtime foreign bytes survived the size+mtime gates (entry +
// copiedFrom) and the journal entry got consumed. The sweep's clean-pending
// removal gate now hashes the opened handle and compares to the journaled
// BackupSHA256; a content mismatch refuses (quarantine never starts, entry
// live, foreign preserved). A matching digest authorizes the quarantine/delete
// as before. An empty sha (legacy) keeps the wave-25 size+mtime posture.

func w63SweepEntry(t *testing.T, fs afero.Fs, backup string, backupBytes []byte) *models.ReplacementEntry {
	t.Helper()
	info, err := lstatRestoreSource(fs, backup)
	require.NoError(t, err)
	return &models.ReplacementEntry{
		Backup:        backup,
		BackupSize:    info.Size(),
		BackupModUnix: info.ModTime().Unix(),
		BackupSHA256:  w63ShaHex(backupBytes),
	}
}

func TestQuarantineReplacementBackup_W63_SHA256MismatchRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w63f/poster.jpg.dlbak." + p3HexA
	original := []byte("AAAAAAAAAAAAAAAA")
	forged := []byte("BBBBBBBBBBBBBBBB") // same length, different content
	w26WriteBackup(t, base, backup, string(original))
	entry := w63SweepEntry(t, base, backup, original)

	// Forge: same length, same mtime — the shape that survived wave-25.
	info, err := lstatRestoreSource(base, backup)
	require.NoError(t, err)
	mtime := info.ModTime()
	require.NoError(t, afero.WriteFile(base, backup, forged, 0o644))
	require.NoError(t, base.Chtimes(backup, mtime, mtime))

	hold, err := quarantineReplacementBackupForRemoval(base, backup, "w63 unit", entry, nil)
	require.Nil(t, hold, "the quarantine never starts on a sha256 mismatch")
	var refused *BackupRemovalRefusedError
	require.ErrorAs(t, err, &refused, "the sha256 mismatch refuses the forged backup typed")
	require.Contains(t, refused.Reason, "sha256 mismatch")
	require.Equal(t, "BBBBBBBBBBBBBBBB", string(mustRead2(t, base, backup)),
		"the foreign occupant is preserved byte-intact at the journaled name")
	require.Empty(t, w26DirQuarNames(t, base, "/w63f"), "no quarantine name was ever claimed")
}

func TestQuarantineReplacementBackup_W63_CorrectSHA256Removes(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w63c/poster.jpg.dlbak." + p3HexA
	backupBytes := []byte("the original set-aside")
	w26WriteBackup(t, base, backup, string(backupBytes))
	entry := w63SweepEntry(t, base, backup, backupBytes)

	require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(base, backup, "w63 unit", entry, nil))
	exists, _ := afero.Exists(base, backup)
	require.False(t, exists, "the verified-owned backup was quarantined and removed")
	require.Empty(t, w26DirQuarNames(t, base, "/w63c"), "the quarantine was unlinked")
}

// A read failure mid-hash refuses the removal (the hash-stream I/O wedge):
// the handle is closed, the quarantine never starts, and the backup stays
// byte-intact — the entry stays live for retry/inspection.
func TestQuarantineReplacementBackup_W63_SHA256ReadFailRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w63r/poster.jpg.dlbak." + p3HexA
	backupBytes := []byte("the original set-aside")
	w26WriteBackup(t, base, backup, string(backupBytes))
	entry := w63SweepEntry(t, base, backup, backupBytes)

	prev := restoreOpenReplacementSource
	restoreOpenReplacementSource = func(fsys afero.Fs, name string) (afero.File, error) {
		inner, err := fsys.Open(name)
		if err != nil {
			return nil, err
		}
		return w36ReadFailFile{File: inner, err: errors.New("w63 read wedge")}, nil
	}
	defer func() { restoreOpenReplacementSource = prev }()

	hold, err := quarantineReplacementBackupForRemoval(base, backup, "w63 unit", entry, nil)
	require.Nil(t, hold, "the quarantine never starts when the sha256 hash read fails")
	require.Error(t, err, "a read failure during the sha256 hash refuses the removal")
	require.Contains(t, err.Error(), "hash backup")
	require.Contains(t, err.Error(), "before removal")
	require.Equal(t, "the original set-aside", string(mustRead2(t, base, backup)),
		"the backup is preserved byte-intact")
	require.Empty(t, w26DirQuarNames(t, base, "/w63r"), "no quarantine name was ever claimed")
}

func TestQuarantineReplacementBackup_W63_AbsentSHARidesWave25(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w63l/poster.jpg.dlbak." + p3HexA
	original := []byte("AAAAAAAAAAAAAAAA")
	forged := []byte("BBBBBBBBBBBBBBBB") // same length, same mtime — forgeable
	w26WriteBackup(t, base, backup, string(original))
	info, err := lstatRestoreSource(base, backup)
	require.NoError(t, err)
	mtime := info.ModTime()
	// Legacy entry: size+mtime stamped, NO sha (armed before wave-63).
	entry := &models.ReplacementEntry{Backup: backup, BackupSize: info.Size(), BackupModUnix: mtime.Unix()}
	require.NoError(t, afero.WriteFile(base, backup, forged, 0o644))
	require.NoError(t, base.Chtimes(backup, mtime, mtime))

	require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(base, backup, "w63 unit", entry, nil),
		"absent sha keeps the wave-25 size+mtime posture — a forged same-size+mtime file rides through")
	exists, _ := afero.Exists(base, backup)
	require.False(t, exists, "the legacy gate proceeded to quarantine+remove (no sha check)")
}
