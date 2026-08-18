package history

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// K3T regression proofs for codex P2 review 4960491781 (poster-write-hardening
// PR #215): SweepDestinations must group every requested destination of ONE
// folder into a single scan group so a crash-window backup for any destination
// in the folder arbitrates in the same pre-revert pass — the revert's
// destination-conflict checks must never meet an unrestored sibling. The group
// key is the probe-aware sweepSlash(dir): folded spellings of one insensitive
// folder collapse into one scan, while distinct-case folders keep separate
// scans on case-sensitive roots. The enumeration path keeps the first-seen
// recorded spelling so afero.ReadDir resolves on-disk.

// k3tJournaled returns the live journaled replacement count for op id.
func k3tJournaled(t *testing.T, repo *p3OpRepo, id uint) int {
	t.Helper()
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return len(gf.Replacements)
}

// k3tScanCountingFs records every directory Open so a test can prove the
// shared folder was enumerated exactly ONCE per sweep pass (afero.ReadDir
// enumerates through fs.Open, and every other sweep step opens files only).
type k3tScanCountingFs struct {
	afero.Fs
	mu       sync.Mutex
	dirOpens []string
}

func (w *k3tScanCountingFs) Open(name string) (afero.File, error) {
	f, err := w.Fs.Open(name)
	if err == nil {
		if fi, sErr := f.Stat(); sErr == nil && fi.IsDir() {
			w.mu.Lock()
			w.dirOpens = append(w.dirOpens, filepath.ToSlash(filepath.Clean(name)))
			w.mu.Unlock()
		}
	}
	return f, err
}

func (w *k3tScanCountingFs) dirOpenCount(dirSlash string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, d := range w.dirOpens {
		if d == dirSlash {
			n++
		}
	}
	return n
}

// (a) Two armed crash-window entries for destinations in the SAME folder are
// both restored and consumed by one sweep pass; the folder is scanned once
// with its recorded-case spelling.
func TestSweepDestinationsCovK3T_SharedFolderRestoresAndConsumesBothInOnePass(t *testing.T) {
	base := &k3tScanCountingFs{Fs: afero.NewMemMapFs()}
	fs := afero.Fs(base)
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/K3T-SHARED"
	coverDest := dir + "/cover.jpg"
	posterDest := dir + "/poster.jpg"
	coverBackup := coverDest + ".dlbak." + p3HexA
	posterBackup := posterDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, coverBackup, "old-cover", time.Hour)
	writeSweepFile(t, fs, posterBackup, "old-poster", time.Hour)
	coverOp := journalRow(t, repo, "job-k3t", "K3T-SHA-C", coverDest, coverBackup, 1, models.RevertStatusApplied)
	posterOp := journalRow(t, repo, "job-k3t", "K3T-SHA-P", posterDest, posterBackup, 2, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{coverDest, posterDest})
	require.NoError(t, err)
	require.Equal(t, 2, healed, "one pass must arbitrate every destination of the shared folder")
	require.Equal(t, "old-cover", string(mustRead2(t, fs, coverDest)), "cover bytes restored")
	require.Equal(t, "old-poster", string(mustRead2(t, fs, posterDest)), "poster bytes restored")
	for _, backup := range []string{coverBackup, posterBackup} {
		_, err := fs.Stat(backup)
		require.ErrorIs(t, err, os.ErrNotExist, "consumed backup removed: %s", backup)
	}
	require.Zero(t, k3tJournaled(t, repo, coverOp.ID), "cover row ends consumed")
	require.Zero(t, k3tJournaled(t, repo, posterOp.ID), "poster row ends consumed")
	require.Equal(t, 1, base.dirOpenCount(dir),
		"the shared folder must be enumerated exactly once for both destinations")
}

// (b) insensitive half: case-folded spellings of ONE destination folder share
// a single scan group — the scan runs under the first-seen recorded spelling
// while entries journaled under the folded spelling still arbitrate.
func TestSweepDestinationsCovK3T_FoldedSpellingsShareOneScanGroupOnInsensitiveRoot(t *testing.T) {
	w10SetCaseProbe(t, false) // insensitive/tolerant destination root
	base := &k3tScanCountingFs{Fs: afero.NewMemMapFs()}
	fs := afero.Fs(base)
	repo := newP3OpRepo()
	ctx := context.Background()
	enumDir := "/Out/K3T-FOLD" // only this spelling exists on disk
	altDir := "/out/k3t-fold"  // folded twin spelling (journaled on poster's row)
	require.Equal(t, sweepSlash(enumDir), sweepSlash(altDir), "fold probe: spellings share one group key")
	coverDest := enumDir + "/cover.jpg"
	posterDestRecorded := enumDir + "/poster.jpg"
	posterDestFolded := altDir + "/poster.jpg"
	coverBackup := coverDest + ".dlbak." + p3HexA
	posterBackup := posterDestRecorded + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(enumDir, 0o755))
	writeSweepFile(t, fs, coverBackup, "old-cover", time.Hour)
	writeSweepFile(t, fs, posterBackup, "old-poster", time.Hour)
	coverOp := journalRow(t, repo, "job-k3t", "K3T-FLD-C", coverDest, coverBackup, 1, models.RevertStatusApplied)
	// Poster's row journals the FOLDED destination spelling with the recorded-case
	// backup spelling — folded journal comparison must still arbitrate it.
	posterOp := journalRow(t, repo, "job-k3t", "K3T-FLD-P", posterDestFolded, posterBackup, 2, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).
		SweepDestinations(ctx, []string{coverDest, posterDestFolded})
	require.NoError(t, err)
	require.Equal(t, 2, healed, "folded spellings of one insensitive folder scan as one group")
	require.Equal(t, "old-cover", string(mustRead2(t, fs, coverDest)))
	require.Equal(t, "old-poster", string(mustRead2(t, fs, posterDestRecorded)),
		"restore lands under the recorded-case spelling")
	exists, err := afero.DirExists(fs, altDir)
	require.NoError(t, err)
	require.False(t, exists, "the folded twin spelling was never enumerated or created")
	require.Zero(t, k3tJournaled(t, repo, coverOp.ID))
	require.Zero(t, k3tJournaled(t, repo, posterOp.ID))
	require.Equal(t, 1, base.dirOpenCount(enumDir), "one scan group ⇒ one enumeration")
}

// (b) sensitive half: distinct-case folders stay SEPARATE scan groups on a
// case-sensitive root — a mis-cased spelling neither enumerates nor arbitrates
// its differently-cased twin, and two real folders are each scanned for their
// own destination.
func TestSweepDestinationsCovK3T_DistinctCaseFoldersStaySeparateOnSensitiveRoot(t *testing.T) {
	w10SetCaseProbe(t, true) // case-sensitive destination root
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	upperDir := "/out/K3T-SEN" // poster lives here
	lowerDir := "/out/k3t-sen" // cover lives here — both exist (memfs is case-sensitive)
	require.NotEqual(t, sweepSlash(upperDir), sweepSlash(lowerDir), "sensitive root: case-distinct group keys")
	posterDest := upperDir + "/poster.jpg"
	coverDest := lowerDir + "/cover.jpg"
	posterBackup := posterDest + ".dlbak." + p3HexA
	coverBackup := coverDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(upperDir, 0o755))
	require.NoError(t, fs.MkdirAll(lowerDir, 0o755))
	writeSweepFile(t, fs, posterBackup, "old-poster", time.Hour)
	writeSweepFile(t, fs, coverBackup, "old-cover", time.Hour)
	posterOp := journalRow(t, repo, "job-k3t", "K3T-SEN-P", posterDest, posterBackup, 1, models.RevertStatusApplied)
	coverOp := journalRow(t, repo, "job-k3t", "K3T-SEN-C", coverDest, coverBackup, 2, models.RevertStatusApplied)

	// A mis-cased request shares NO group with the on-disk folder: its enum dir
	// never resolves, so nothing arbitrates and every flow stays untouched.
	healed, err := NewReplacementSweeper(fs, repo).
		SweepDestinations(ctx, []string{"/OUT/K3T-SEN/poster.jpg"})
	require.NoError(t, err)
	require.Zero(t, healed, "mis-cased spelling must stay outside the real folder's group")
	for _, backup := range []string{posterBackup, coverBackup} {
		exists, exErr := afero.Exists(fs, backup)
		require.NoError(t, exErr)
		require.True(t, exists, "no fold-driven arbitration on a sensitive root: %s", backup)
	}
	require.Equal(t, 1, k3tJournaled(t, repo, posterOp.ID), "poster entry still armed")
	require.Equal(t, 1, k3tJournaled(t, repo, coverOp.ID), "cover entry still armed")
	exists, err := afero.DirExists(fs, "/OUT")
	require.NoError(t, err)
	require.False(t, exists, "mis-cased scan created nothing")

	// Exact-case requests enumerate each folder for its own destination. Were
	// the two folders folded into one group, only the first spelling would scan
	// and one backup would leak past the pass — healed must be 2.
	healed, err = NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{posterDest, coverDest})
	require.NoError(t, err)
	require.Equal(t, 2, healed, "each case-distinct folder scans for its own destination")
	require.Equal(t, "old-poster", string(mustRead2(t, fs, posterDest)))
	require.Equal(t, "old-cover", string(mustRead2(t, fs, coverDest)))
	exists, err = afero.Exists(fs, lowerDir+"/poster.jpg")
	require.NoError(t, err)
	require.False(t, exists, "poster restored only under its recorded case")
	require.Zero(t, k3tJournaled(t, repo, posterOp.ID))
	require.Zero(t, k3tJournaled(t, repo, coverOp.ID))
}

// (c) A grouped folder with an armed entry for only ONE of its destinations:
// the entry's flow heals; every other flow in the folder stays untouched
// (present destination bytes; an orphan marker backup for the second
// destination is retained, journaled nowhere).
func TestSweepDestinationsCovK3T_GroupedDirWithOneEntryLeavesOtherFlowsUntouched(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/K3T-PARTIAL"
	coverDest := dir + "/cover.jpg"
	posterDest := dir + "/poster.jpg"
	coverBackup := coverDest + ".dlbak." + p3HexA
	posterOrphan := posterDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, coverBackup, "old-cover", time.Hour)
	writeSweepFile(t, fs, posterDest, "current-poster", time.Hour)
	writeSweepFile(t, fs, posterOrphan, "unproven-owner", time.Hour)
	coverOp := journalRow(t, repo, "job-k3t", "K3T-PAR-C", coverDest, coverBackup, 1, models.RevertStatusApplied)
	// No row journals poster: only ONE of the group's destinations has an entry.

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{coverDest, posterDest})
	require.NoError(t, err)
	require.Equal(t, 1, healed, "only the armed crash-window flow heals")
	require.Equal(t, "old-cover", string(mustRead2(t, fs, coverDest)))
	_, err = fs.Stat(coverBackup)
	require.ErrorIs(t, err, os.ErrNotExist, "cover backup consumed")
	require.Zero(t, k3tJournaled(t, repo, coverOp.ID))
	require.Equal(t, "current-poster", string(mustRead2(t, fs, posterDest)),
		"entry-less destination bytes untouched")
	require.Equal(t, "unproven-owner", string(mustRead2(t, fs, posterOrphan)),
		"orphan marker backup retained — marker shape is not ownership proof")
	exists, err := afero.Exists(fs, fsutil.ReplacementBusyPath(posterDest))
	require.NoError(t, err)
	require.False(t, exists, "busy arbitration left no marker behind on the untouched flow")
}

// (d) DestSeq reverse order is honored within a group: the revert sweeps
// destinations newest-first (descending DestSeq), so request poster (DestSeq 2)
// before cover (DestSeq 1); both entries — journaled on ONE row — must restore
// and consume in a single grouped scan of the shared folder.
func TestSweepDestinationsCovK3T_ReverseDestSeqOrderWithinOneGroup(t *testing.T) {
	base := &k3tScanCountingFs{Fs: afero.NewMemMapFs()}
	fs := afero.Fs(base)
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/K3T-REVSEQ"
	coverDest := dir + "/cover.jpg"
	posterDest := dir + "/poster.jpg"
	coverBackup := coverDest + ".dlbak." + p3HexA
	posterBackup := posterDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, coverBackup, "old-cover", time.Hour)
	writeSweepFile(t, fs, posterBackup, "old-poster", time.Hour)
	op := &models.BatchFileOperation{
		BatchJobID: "job-k3t", MovieID: "K3T-REV", OriginalPath: "/src/K3T-REV.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
			{Destination: coverDest, Backup: coverBackup, DestSeq: 1},
			{Destination: posterDest, Backup: posterBackup, DestSeq: 2},
		}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	// Reverse DestSeq order (newest destination first), one shared folder.
	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{posterDest, coverDest})
	require.NoError(t, err)
	require.Equal(t, 2, healed, "request order never narrows the group: both destinations heal")
	require.Equal(t, "old-poster", string(mustRead2(t, fs, posterDest)))
	require.Equal(t, "old-cover", string(mustRead2(t, fs, coverDest)))
	require.Zero(t, k3tJournaled(t, repo, op.ID),
		"both entries consumed from the one row across the grouped pass")
	require.Equal(t, 1, base.dirOpenCount(dir),
		"DestSeq-reversed requests still enumerate the shared folder exactly once")
}

// (e) Regression: a single-destination targeted sweep behaves exactly as
// before grouping — the crash-window restore heals and consumes, an unlisted
// destination's journaled backup in the same folder is never arbitrated, and
// foreign lookalike names stay out of scope.
func TestSweepDestinationsCovK3T_SingleDestinationRegressionUnchanged(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/K3T-ONE"
	posterDest := dir + "/poster.jpg"
	posterBackup := posterDest + ".dlbak." + p3HexA
	fanartDest := dir + "/fanart.jpg"
	fanartBackup := fanartDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, posterBackup, "old-poster", time.Hour)
	writeSweepFile(t, fs, fanartBackup, "old-fanart", time.Hour)
	writeSweepFile(t, fs, dir+"/cover.jpg.dlbak.SHORT", "foreign-short", time.Hour)
	writeSweepFile(t, fs, dir+"/cover.jpg.dlbak."+p3HexC+".zz", "foreign-tail", time.Hour)
	posterOp := journalRow(t, repo, "job-k3t", "K3T-ONE-P", posterDest, posterBackup, 1, models.RevertStatusApplied)
	fanartOp := journalRow(t, repo, "job-k3t", "K3T-ONE-F", fanartDest, fanartBackup, 2, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{posterDest})
	require.NoError(t, err)
	require.Equal(t, 1, healed, "single-destination pass heals exactly its own flow")
	require.Equal(t, "old-poster", string(mustRead2(t, fs, posterDest)))
	_, err = fs.Stat(posterBackup)
	require.ErrorIs(t, err, os.ErrNotExist, "consumed backup removed")
	require.Zero(t, k3tJournaled(t, repo, posterOp.ID), "listed entry consumed")
	require.Equal(t, "old-fanart", string(mustRead2(t, fs, fanartBackup)),
		"unlisted destination's armed backup is out of the targeted scope")
	require.Equal(t, 1, k3tJournaled(t, repo, fanartOp.ID), "unlisted sibling entry remains armed")
	_, err = fs.Stat(fanartDest)
	require.ErrorIs(t, err, os.ErrNotExist, "sibling destination is not restored by the pass")
	for _, foreign := range []string{dir + "/cover.jpg.dlbak.SHORT", dir + "/cover.jpg.dlbak." + p3HexC + ".zz"} {
		exists, exErr := afero.Exists(fs, foreign)
		require.NoError(t, exErr)
		require.True(t, exists, "foreign lookalike untouched: %s", foreign)
	}
}
