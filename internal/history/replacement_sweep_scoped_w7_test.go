package history

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING P2 review (bounded CLI pre-revert sweep): scoped
// root computation, dir-scoped sweeping, and context responsiveness. A hung
// or unrelated root must never delay a targeted revert.

// seedCrashWindow journals one armed (install never confirmed) replacement
// whose destination is missing — the sweep must restore backup → dest and
// consume the entry.
func seedCrashWindow(t *testing.T, fs afero.Fs, repo *p3OpRepo, jobID, movieID, dir, hexTag string) (*models.BatchFileOperation, string, string) {
	t.Helper()
	dest := filepath.ToSlash(filepath.Join(dir, "poster.jpg"))
	backup := dest + ".dlbak." + hexTag
	require.NoError(t, fs.MkdirAll(filepath.FromSlash(dir), 0o755))
	writeSweepFile(t, fs, backup, "original-"+movieID, time.Hour)
	op := journalRow(t, repo, jobID, movieID, dest, backup, 1, models.RevertStatusApplied)
	return op, dest, backup
}

func requireLedgerReplacements(t *testing.T, repo *p3OpRepo, id uint) []models.ReplacementEntry {
	t.Helper()
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return gf.Replacements
}

func TestSweepDirs_HealsInScopeAndIgnoresOutOfScope(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()

	opIn, destIn, backupIn := seedCrashWindow(t, fs, repo, "job-target", "IN-001", "/in-scope", p3HexA)
	opOut, destOut, backupOut := seedCrashWindow(t, fs, repo, "job-other", "OUT-001", "/out-of-scope", p3HexB)

	sweeper := NewReplacementSweeper(fs, repo)
	healed, err := sweeper.SweepDirs(context.Background(), []string{"/in-scope"})
	require.NoError(t, err)
	require.Equal(t, 1, healed, "only the in-scope crash window is healed")

	require.Equal(t, "original-IN-001", string(mustRead2(t, fs, destIn)), "in-scope destination restored")
	inExists, err := afero.Exists(fs, backupIn)
	require.NoError(t, err)
	require.False(t, inExists, "in-scope backup consumed")
	require.Empty(t, requireLedgerReplacements(t, repo, opIn.ID), "in-scope entry consumed from the journal")

	// The out-of-scope crash window is untouched in EVERY dimension: bytes,
	// backup, and journal entry survive for a later sweep rooted there.
	outDestExists, err := afero.Exists(fs, destOut)
	require.NoError(t, err)
	require.False(t, outDestExists, "out-of-scope destination is not materialized by a scoped sweep")
	outBackupExists, err := afero.Exists(fs, backupOut)
	require.NoError(t, err)
	require.True(t, outBackupExists, "out-of-scope backup untouched")
	require.Len(t, requireLedgerReplacements(t, repo, opOut.ID), 1, "out-of-scope journal entry untouched")
}

func TestSweepDirs_SkipsUnreadableAndDuplicateDirs(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, fs, repo, "job-1", "DUP-001", "/dup", p3HexA)
	// Non-candidates inside a scanned dir (a subdirectory, a non-marker file)
	// are skipped without disturbing the marker backup.
	require.NoError(t, fs.MkdirAll("/dup/subdir", 0o755))
	writeSweepFile(t, fs, "/dup/plain.txt", "keep", time.Hour)

	healed, err := NewReplacementSweeper(fs, repo).SweepDirs(context.Background(),
		[]string{"/missing-dir", "/dup", "/dup/"})
	require.NoError(t, err)
	require.Equal(t, 1, healed, "unreadable dirs skip silently; duplicate dirs scan once")
	require.Equal(t, "original-DUP-001", string(mustRead2(t, fs, dest)))
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.False(t, exists)
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
	require.Equal(t, "keep", string(mustRead2(t, fs, "/dup/plain.txt")), "non-marker files untouched")
	subExists, err := afero.DirExists(fs, "/dup/subdir")
	require.NoError(t, err)
	require.True(t, subExists, "subdirectories untouched")
}

// TestSweepDirs_IndexErrorSurfaces: an unreadable journal index is an error,
// never a silent zero-sweep (R7-2).
func TestSweepDirs_IndexErrorSurfaces(t *testing.T) {
	broken := &covW2DErrorIndexRepo{p3OpRepo: newP3OpRepo(), err: errors.New("index unavailable")}
	healed, err := NewReplacementSweeper(afero.NewMemMapFs(), broken).SweepDirs(context.Background(), []string{"/anywhere"})
	require.Error(t, err)
	require.Equal(t, 0, healed)
}

// TestSweep_CanceledCtxReturnsBeforeTheScan: the all-roots sweep's early
// cancellation gate answers immediately without touching the index or disk.
func TestSweep_CanceledCtxReturnsBeforeTheScan(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	_, _, backup := seedCrashWindow(t, fs, repo, "job-1", "PRE-001", "/pre-cancel", p3HexA)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, healed)
	exists, ferr := afero.Exists(fs, backup)
	require.NoError(t, ferr)
	require.True(t, exists)
}

func TestSweepDirs_EmptyScopeIsA_NoOp(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	_, _, backup := seedCrashWindow(t, fs, repo, "job-1", "NIL-001", "/nil", p3HexA)

	healed, err := NewReplacementSweeper(fs, repo).SweepDirs(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists, "an empty scope sweeps nothing")
}

func TestSweepDirs_CanceledCtxReturnsPromptlyBeforeScan(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	_, dest, backup := seedCrashWindow(t, fs, repo, "job-1", "CXB-001", "/cxb", p3HexA)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	healed, err := NewReplacementSweeper(fs, repo).SweepDirs(ctx, []string{"/cxb"})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, healed)
	require.Less(t, time.Since(start), 5*time.Second, "a canceled sweep returns promptly, not after the scan")

	exists, ferr := afero.Exists(fs, backup)
	require.NoError(t, ferr)
	require.True(t, exists, "nothing was consumed")
	destExists, ferr := afero.Exists(fs, dest)
	require.NoError(t, ferr)
	require.False(t, destExists)
}

func TestSweepDirs_CancellationBetweenDirsStopsSweep(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	_, destA, _ := seedCrashWindow(t, base, repo, "job-1", "MID-A", "/mid-a", p3HexA)
	_, destB, backupB := seedCrashWindow(t, base, repo, "job-1", "MID-B", "/mid-b", p3HexB)

	ctx, cancel := context.WithCancel(context.Background())
	// Wave-46 restaging: the deadline lands at the FIRST dir's busy claim
	// (mid-heal, past the post-ReadDir gate) instead of inside its ReadDir —
	// cancellation inside the scan itself now stops that dir before any
	// arbitration (see replacement_sweep_abandoned_w46_test.go).
	fs := &w46CancelOnFirstTouchFs{Fs: base, cancel: cancel,
		trigger: map[string]bool{filepath.ToSlash(fsutil.ReplacementBusyPath(destA)): true}}
	healed, err := NewReplacementSweeper(fs, repo).SweepDirs(ctx, []string{"/mid-a", "/mid-b"})
	require.ErrorIs(t, err, context.Canceled, "cancellation between dirs ends the sweep with progress reported")
	require.Equal(t, 1, healed, "the first dir healed before the deadline landed")
	require.Equal(t, "original-MID-A", string(mustRead2(t, base, destA)))
	destBExists, ferr := afero.Exists(base, destB)
	require.NoError(t, ferr)
	require.False(t, destBExists, "the second dir was never scanned")
	backupBExists, ferr := afero.Exists(base, backupB)
	require.NoError(t, ferr)
	require.True(t, backupBExists)
}

func TestSweep_CancellationBetweenDirsStopsFullSweep(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	_, destA, _ := seedCrashWindow(t, base, repo, "job-1", "SWC-A", "/sweep-a", p3HexA)
	_, destB, backupB := seedCrashWindow(t, base, repo, "job-1", "SWC-B", "/sweep-b", p3HexB)

	ctx, cancel := context.WithCancel(context.Background())
	// Wave-46 restaging: cancellation lands at whichever dir's busy claim
	// fires FIRST (the map-ordered scan heals it), not inside that dir's
	// ReadDir — the in-flight dir completes exactly as before.
	fs := &w46CancelOnFirstTouchFs{Fs: base, cancel: cancel, trigger: map[string]bool{
		filepath.ToSlash(fsutil.ReplacementBusyPath(destA)): true,
		filepath.ToSlash(fsutil.ReplacementBusyPath(destB)): true,
	}}
	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.ErrorIs(t, err, context.Canceled, "the full sweep honors cancellation between directories")
	// Directory iteration over the index map is intentionally unordered, and
	// the single-shot cancel fires during whichever directory heals FIRST:
	// exactly one of the two dirs must be healed (the one in progress when
	// the deadline landed) while the other remains completely untouched.
	require.Equal(t, 1, healed, "the in-flight directory completes; the sweep stops before the next one")
	_, destAErr := fs.Stat(destA)
	_, destBErr := fs.Stat(destB)
	aHealed := destAErr == nil
	backupBExists, ferr := afero.Exists(base, backupB)
	require.NoError(t, ferr)
	if aHealed {
		require.Equal(t, "original-SWC-A", string(mustRead2(t, base, destA)), "healing completed before the deadline landed")
		require.True(t, backupBExists, "the not-yet-scanned directory is untouched")
	} else {
		require.True(t, errors.Is(destAErr, afero.ErrFileNotFound), "the not-yet-scanned directory is untouched")
		require.NoError(t, destBErr, "healing completed before the deadline landed on B")
		require.Equal(t, "original-SWC-B", string(mustRead2(t, base, destB)), "B's heal restored the original bytes")
		require.False(t, backupBExists, "B's backup consumed by the completed heal")
	}
}

func TestSweepDestinations_CancellationBetweenGroupsStopsSweep(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	opA, destA, _ := seedCrashWindow(t, base, repo, "job-1", "DSC-A", "/dsc-a", p3HexA)
	opB, destB, backupB := seedCrashWindow(t, base, repo, "job-1", "DSC-B", "/dsc-b", p3HexB)
	_ = opA
	_ = opB

	ctx, cancel := context.WithCancel(context.Background())
	// Wave-46 restaging: the deadline lands at group A's busy claim instead
	// of inside its ReadDir — same healed-then-stop contract.
	fs := &w46CancelOnFirstTouchFs{Fs: base, cancel: cancel,
		trigger: map[string]bool{filepath.ToSlash(fsutil.ReplacementBusyPath(destA)): true}}
	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{destA, destB})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, healed)
	require.Equal(t, "original-DSC-A", string(mustRead2(t, base, destA)))
	backupBExists, ferr := afero.Exists(base, backupB)
	require.NoError(t, ferr)
	require.True(t, backupBExists)
}

func TestSweepRootsOnStartupWithContext(t *testing.T) {
	t.Run("heals only the given roots and reports progress", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		op, dest, _ := seedCrashWindow(t, fs, repo, "job-1", "SRS-001", "/scoped", p3HexA)
		_, outDest, outBackup := seedCrashWindow(t, fs, repo, "job-2", "SRS-002", "/unscoped", p3HexB)

		SweepRootsOnStartupWithContext(context.Background(), fs, repo, []string{"/scoped"})

		require.Equal(t, "original-SRS-001", string(mustRead2(t, fs, dest)))
		require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
		outDestExists, err := afero.Exists(fs, outDest)
		require.NoError(t, err)
		require.False(t, outDestExists)
		outBackupExists, err := afero.Exists(fs, outBackup)
		require.NoError(t, err)
		require.True(t, outBackupExists)
	})

	t.Run("zero-heal run logs nothing extra", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		SweepRootsOnStartupWithContext(context.Background(), fs, repo, []string{"/nothing"})
	})

	t.Run("canceled context logs and continues", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		_, _, backup := seedCrashWindow(t, fs, repo, "job-1", "SRC-001", "/src-cancel", p3HexA)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		SweepRootsOnStartupWithContext(ctx, fs, repo, []string{"/src-cancel"})
		exists, err := afero.Exists(fs, backup)
		require.NoError(t, err)
		require.True(t, exists, "a canceled scoped sweep must not consume anything")
	})
}

func TestOperationSweepRoots(t *testing.T) {
	dirName := func(p string) string { return filepath.Dir(p) }

	opFull := models.BatchFileOperation{
		BatchJobID:      "job-1",
		MovieID:         "FULL-001",
		OriginalPath:    "/src/full-001.mkv",
		NewPath:         "/dst/full-001/full-001.mkv",
		NFOPath:         "/dst/full-001/full-001.nfo",
		OriginalDirPath: "/src/original-dir",
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Delete:   []string{"/dst/full-001/poster.jpg"},
			MoveBack: []models.FileMove{{OriginalPath: "/src/subs/full-001.srt", NewPath: "/dst/full-001/full-001.srt"}},
			Roots:    []string{"/dst/full-001/nested"},
			Replacements: []models.ReplacementEntry{{
				Destination: "/dst/full-001/fanart.jpg",
				Backup:      "/dst/full-001/fanart.jpg.dlbak." + p3HexA,
				DestSeq:     1,
			}},
		}),
	}
	opOverlap := models.BatchFileOperation{ // duplicates FULL-001's dirs plus one unique
		BatchJobID:   "job-1",
		MovieID:      "OVER-001",
		OriginalPath: "/src/full-001.mkv",   // dup of opFull's original dir
		NewPath:      "/dst/full-001/x.mkv", // dup of opFull's new dir
		NFOPath:      "   ",                 // blank — contributes nothing
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Roots: []string{"/dst/over-001"},
		}),
	}
	opBroken := models.BatchFileOperation{ // malformed ledger — columns still count
		BatchJobID:     "job-1",
		MovieID:        "BRK-001",
		OriginalPath:   "/src/broken.mkv",
		NewPath:        "/dst/broken/broken.mkv",
		GeneratedFiles: `{"replacements":broken`,
	}

	roots := OperationSweepRoots([]models.BatchFileOperation{opFull, opOverlap, opBroken})

	// The deduped union: opFull's media/NFO/poster/move-back/fanart(+backup)
	// dirs all collapse to /dst/full-001; /src joins from both ops' originals;
	// the broken ledger still contributes its column dirs.
	expected := []string{
		dirName("/dst/broken/broken.mkv"),
		dirName("/dst/full-001/full-001.mkv"),
		"/dst/full-001/nested",
		"/dst/over-001",
		dirName("/src/full-001.mkv"),
		"/src/original-dir",
		dirName("/src/subs/full-001.srt"),
	}
	sort.Strings(expected)
	require.Equal(t, expected, roots, "unique sorted roots across every ledger and column source")

	require.Nil(t, OperationSweepRoots(nil), "no operations means no roots")
}

func TestOperationSweepRoots_BackupDirDistinctFromDestination(t *testing.T) {
	// Pin that BOTH the replacement destination dir and the backup dir are
	// swept — the downloader may park backups anywhere the destination lock
	// covers.
	op := models.BatchFileOperation{
		BatchJobID:   "job-1",
		MovieID:      "DST-001",
		OriginalPath: "/src/dst-001.mkv",
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination: "/media/poster.jpg",
				Backup:      "/staging/poster.jpg.dlbak." + p3HexA,
				DestSeq:     1,
			}},
		}),
	}
	roots := OperationSweepRoots([]models.BatchFileOperation{op})
	require.Contains(t, roots, filepath.Dir("/media/poster.jpg"))
	require.Contains(t, roots, filepath.Dir("/staging/poster.jpg.dlbak."+p3HexA))
}
