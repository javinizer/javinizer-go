package history

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

// codex P3 R5-3: restores stream through a bounded buffer — a trailer-class
// backup restores byte-exactly without whole-file buffering.
func TestRevertRestore_StreamsLargeBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	const size = 6 << 20 // 6 MiB — plain evidence the path works above the buffer size
	big := make([]byte, size)
	for i := range big {
		big[i] = byte(i % 251)
	}

	f := &p3Fixture{fs: fs, repo: repo}
	op, dest := f.addAppliedOp(t, "job-1", "BIG-001", false, "new", p3Replacement{seq: 1, backupBytes: ""})
	// Replace the tiny fixture backup with the big one.
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.a", big, config.FilePerm))
	require.NotNil(t, op)

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	got, err := afero.ReadFile(fs, dest)
	require.NoError(t, err)
	require.Equal(t, len(big), len(got))
	require.Equal(t, big, got, "restored bytes must be byte-identical")
}

// codex P3 R8-1: the orphan-restore stream must be byte-exact (copy, not
// rename now — the staged copy + swap path), and an indeterminate
// destination stat must keep the backup untouched.
func TestSweep_OrphanRestoreByteExact_AndIndeterminateKeeps(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/IND/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/IND", config.DirPerm))
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	require.NoError(t, afero.WriteFile(fs, backup, payload, config.FilePerm))
	backdate(t, fs, backup)

	// Scope the dir via a journal row on an unrelated path.
	raw, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/IND"}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "IND-001", OriginalPath: "/src/ind.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	got, err := afero.ReadFile(fs, dest)
	require.NoError(t, err)
	require.Equal(t, payload, got, "restore is byte-exact")
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "orphan backup removed after a successful streamed restore")

	// Indeterminate destination: wrap the fs so Stat(dest) fails.
	fs2 := &statFailingFs{Fs: afero.NewMemMapFs(), failPath: dest}
	require.NoError(t, fs2.MkdirAll("/out/IND", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs2, backup, payload, config.FilePerm))
	backdate(t, fs2, backup)
	healed, err = NewReplacementSweeper(fs2, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "indeterminate destination — backup kept")
	exists, _ = afero.Exists(fs2, backup)
	require.True(t, exists)
}

type statFailingFs struct {
	afero.Fs
	failPath string
}

func (f *statFailingFs) Stat(name string) (os.FileInfo, error) {
	if strings.Contains(name, "poster.jpg") && !strings.Contains(name, ".dlbak.") {
		return nil, pathErrPermission(name)
	}
	return f.Fs.Stat(name)
}

type pathErrPermission string

func (e pathErrPermission) Error() string { return "permission denied: " + string(e) }
