package history

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// codex P3 R4-3: an install-CONFIRMED entry whose destination went missing
// afterwards means somebody deleted the media — the sweep must NOT resurrect
// it (and must keep the journaled backup so revert still can).
func TestSweep_ConfirmedInstall_MissingDest_NotResurrected(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/DEL/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/DEL", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(backup, old, old))

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1, Installed: true},
	}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "DEL-001", OriginalPath: "/src/del.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "no crash window after a confirmed install")
	exists, _ := afero.Exists(fs, dest)
	require.False(t, exists, "deleted-afterwards media stays deleted")
	exists, _ = afero.Exists(fs, backup)
	require.True(t, exists, "journaled backup retained for an explicit revert")
}

// codex P3 R4-1: the orphan classification must re-read ownership under the
// destination lock — a row journaled after the sweep's index snapshot must
// never see its backup removed.
func TestSweep_OrphanFreshRecheckUnderLock_KeepsJustJournaledBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/FRESH/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/FRESH", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("new"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(backup, old, old))

	// The journal row exists in the repo, but the sweep's index snapshot does
	// NOT include it — simulating an index built before RecordReplacement.
	raw, _ := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1},
	}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "FRESH-1", OriginalPath: "/src/f.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	sweeper := NewReplacementSweeper(fs, repo)
	staleIdx := &replacementLedgerIndex{
		journaled: map[string]*models.BatchFileOperation{},
		dirs:      map[string]bool{"/out/FRESH": true},
	}
	info, err := fs.Stat(backup)
	require.NoError(t, err)
	got := sweeper.sweepOne(ctx, staleIdx, "/out/FRESH", info)
	require.Equal(t, 0, got, "freshly journaled backup survives the orphan branch")
	exists, _ := afero.Exists(fs, backup)
	require.True(t, exists)
}

// codex P3 R4-2: media lands in the organizer's nested leaf dir — the sweep
// must descend bounded levels below the recorded base root.
func TestSweep_NestedRoot_BackupDiscoveredThreeLevelsDeep(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/base/Movie (2020)/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/base/Movie (2020)", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("nested-old"), config.FilePerm))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(backup, old, old))

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/base"}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "NST-001", OriginalPath: "/src/nst.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "nested-old", string(mustRead2(t, fs, dest)))
	fmt.Println("") // keep fmt import referenced across build variants
}
