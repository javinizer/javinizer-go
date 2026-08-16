package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// codex P3 R6-1: per-destination sequences are not comparable ACROSS
// destinations — an op keyed high only on cover (seq 10) sorts ahead of an
// op with poster seq 2 and gets dependency-rejected by the newer poster
// entry; the run must retry it after the blocker is consumed.
func TestRevert_DependencyBlockedRetriesAfterBlockerConsumed(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	poster := "/dst/BLK/poster.jpg"
	cover := "/dst/BLK/cover.jpg"
	require.NoError(t, fs.MkdirAll("/dst/BLK", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, poster, []byte("poster-newest"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, cover, []byte("cover-newest"), config.FilePerm))
	// Older's backups; newer replaced only the poster.
	require.NoError(t, afero.WriteFile(fs, poster+".dlbak.1", []byte("poster-old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, poster+".dlbak.2", []byte("poster-mid"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, cover+".dlbak.10", []byte("cover-old"), config.FilePerm))

	mkOp := func(movieID string, entries []models.ReplacementEntry) *models.BatchFileOperation {
		newPath := "/dst/" + movieID + "/" + movieID + ".mkv"
		require.NoError(t, fs.MkdirAll("/dst/"+movieID, config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, newPath, []byte("video"), config.FilePerm))
		raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: entries})
		require.NoError(t, err)
		op := &models.BatchFileOperation{
			BatchJobID: "job-1", MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv", NewPath: newPath,
			OperationType: models.OperationTypeMove, GeneratedFiles: string(raw),
			RevertStatus: models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(ctx, op))
		return op
	}
	older := mkOp("BLK-OLD", []models.ReplacementEntry{
		{Destination: poster, Backup: poster + ".dlbak.1", DestSeq: 1},
		{Destination: cover, Backup: cover + ".dlbak.10", DestSeq: 10},
	})
	newer := mkOp("BLK-NEW", []models.ReplacementEntry{
		{Destination: poster, Backup: poster + ".dlbak.2", DestSeq: 2},
	})

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 2, res.Succeeded,
		"dependency-rejected op retried after its blocker's journal was consumed")
	require.Equal(t, "poster-old", string(mustRead2(t, fs, poster)),
		"full unwind: poster ends at the oldest bytes")
	require.Equal(t, "cover-old", string(mustRead2(t, fs, cover)))
	for _, op := range []*models.BatchFileOperation{older, newer} {
		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	}
}

// codex P3 R6-2: a deleted primary anchor must not strand the replacement
// journal — media still restores (copy-mode hasthe only recovery path).
func TestRevert_AnchorMissing_StillRestoresReplacementJournal(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	f := &p3Fixture{fs: fs, repo: repo}
	op, dest := f.addAppliedOp(t, "job-1", "ANC-001", false, "new-poster", p3Replacement{seq: 1, backupBytes: "original-poster"})
	// Sabotage: the video (anchor) was deleted out-of-band.
	require.NoError(t, fs.Remove(op.NewPath))

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Skipped, "anchor-missing still skips the row-level revert")
	require.Equal(t, "original-poster", string(mustRead2(t, fs, dest)),
		"the media journal restored anyway — never stranded by the anchor")
	exists, _ := afero.Exists(fs, dest+".dlbak.a")
	require.False(t, exists, "backup consumed")
}

// codex P3 R7-1: the newer/journal fresh check runs against the LIVE row set
// under the destination lock — a row created mid-run still blocks an older
// operation's restore.
func TestCheckDestBlocking_LiveJournal(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/dst/LIVE/poster.jpg"
	require.NoError(t, fs.MkdirAll("/dst/LIVE", config.DirPerm))

	r := NewReverter(fs, repo)
	self := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "LIVE-OLD",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: mustJSON(t, models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
			{Destination: dest, Backup: dest + ".dlbak.1", DestSeq: 1},
		}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, self))
	require.NoError(t, r.checkDestBlocking(ctx, self, dest, 1), "own entries never block")

	// A NEWER row appears — possibly journaled after any preflight.
	newer := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "LIVE-NEW",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: mustJSON(t, models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
			{Destination: dest, Backup: dest + ".dlbak.2", DestSeq: 2},
		}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, newer))
	err := r.checkDestBlocking(ctx, self, dest, 1)
	require.Error(t, err)
	var nad *NewerAppliedDestError
	require.ErrorAs(t, err, &nad)
	require.Equal(t, newer.ID, nad.NewerOpID)

	// Reverted newer rows stop blocking (entries consumed in practice).
	require.NoError(t, repo.UpdateRevertStatus(ctx, newer.ID, models.RevertStatusReverted))
	require.NoError(t, r.checkDestBlocking(ctx, self, dest, 1))
}

func mustJSON(t *testing.T, gf models.GeneratedFilesJSON) string {
	t.Helper()
	raw, err := json.Marshal(gf)
	require.NoError(t, err)
	return string(raw)
}

// codex P3 R16-1: two SPELLINGS of one physical destination in one chain
// must restore as a single ordered group — never split into two groups
// unwound in map order.
func TestRevert_SpellingSplitChain_GroupsCanonically(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	require.NoError(t, fs.MkdirAll("/dst/SPL", config.DirPerm))
	destV1 := "/dst/SPL/Poster.jpg" // op.spelling #1
	destV2 := "/dst/SPL/poster.jpg" // op spelling #2 — same file on win/mac-cs
	require.NoError(t, afero.WriteFile(fs, destV1, []byte("final"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, destV1+".dlbak.b1", []byte("original"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, destV2+".dlbak.b2", []byte("mid"), config.FilePerm))

	f := &p3Fixture{fs: fs, repo: repo}
	op, _ := f.addAppliedOp(t, "job-1", "SPL-001", false, "unused")

	raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: destV1, Backup: destV1 + ".dlbak.b1", DestSeq: 1},
		{Destination: destV2, Backup: destV2 + ".dlbak.b2", DestSeq: 2},
	}})
	require.NoError(t, err)
	op.GeneratedFiles = string(raw)
	require.NoError(t, repo.Update(ctx, op))

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded, "split-spelling chain unwinds as ONE destination")
	require.Equal(t, "original", string(mustRead2(t, fs, destV1)),
		"oldest bytes win — regardless of group order (seq1 last)")
	exists, _ := afero.Exists(fs, destV1+".dlbak.b1")
	require.False(t, exists)
	exists, _ = afero.Exists(fs, destV2+".dlbak.b2")
	require.False(t, exists)

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

// codex P3 R18-1: a blocker that consumes its journal but SKIPs (anchor
// missing) is still progress — consumption lets deeper chains unwind instead
// of stalling after one pass.
func TestRevert_ChainUnwindsThroughSkippedBlockers(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/dst/CHN3/poster.jpg"
	require.NoError(t, fs.MkdirAll("/dst/CHN3", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("C-final"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.1", []byte("original"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.2", []byte("post-A"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.3", []byte("post-B"), config.FilePerm))

	mk := func(movieID string, seq int64) *models.BatchFileOperation {
		newPath := "/dst/" + movieID + "/" + movieID + ".mkv"
		require.NoError(t, fs.MkdirAll("/dst/"+movieID, config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, newPath, []byte("video"), config.FilePerm))
		raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
			{Destination: dest, Backup: dest + fmt.Sprintf(".dlbak.%d", seq), DestSeq: seq},
		}})
		require.NoError(t, err)
		op := &models.BatchFileOperation{
			BatchJobID: "job-1", MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv", NewPath: newPath,
			OperationType: models.OperationTypeMove, GeneratedFiles: string(raw),
			RevertStatus: models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(ctx, op))
		return op
	}
	ops := []*models.BatchFileOperation{mk("D3-AAA", 1), mk("D3-BBB", 2), mk("D3-CCC", 3)}
	// All three anchors deleted out-of-band → every op SKIPs at the row
	// level, but the journal chain must still unwind fully.
	for _, op := range ops {
		require.NoError(t, fs.Remove(op.NewPath))
	}

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 3, res.Skipped, "every op anchors-missing once its journal unwound")
	require.Equal(t, 0, res.Failed)
	require.Equal(t, "original", string(mustRead2(t, fs, dest)),
		"the whole chain rewound to the oldest bytes through skipped blockers")
	for _, op := range ops {
		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
		require.NoError(t, err)
		require.Empty(t, gf.Replacements)
	}
}

// Chain-check query failures surface as revert errors (never an open door).
func TestCheckDestBlocking_ScanError(t *testing.T) {
	repo := &errScanRepo2{p3OpRepo: newP3OpRepo(), err: errors.New("scan wedged")}
	r := NewReverter(afero.NewMemMapFs(), repo)
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "SCN-001", OriginalPath: "/src/s.mkv",
		OperationType: models.OperationTypeUpdate,
		RevertStatus:  models.RevertStatusApplied,
	}
	err := r.checkDestBlocking(context.Background(), op, "/dst/s/poster.jpg", 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "destination journal")
}

type errScanRepo2 struct {
	*p3OpRepo
	err error
}

func (m *errScanRepo2) FindOperationsByDestination(context.Context, string) ([]models.BatchFileOperation, error) {
	return nil, m.err
}

// consumeReplacementEntry legs: unreadable row, malformed ledger body.
func TestConsumeReplacementEntry_ErrorLegs(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	op, _ := (&p3Fixture{fs: fs, repo: repo}).addAppliedOp(t, "job-1", "CER-001", false, "x", p3Replacement{seq: 1, backupBytes: "y"})

	r := NewReverter(fs, repo)
	entry := models.ReplacementEntry{Destination: "/dst/CER-001/poster.jpg", Backup: "b", DestSeq: 1}

	// unreadable row
	goneRepo := &rowGoneRepo2{p3OpRepo: repo, goneID: op.ID}
	r2 := NewReverter(fs, goneRepo)
	require.Error(t, r2.consumeReplacementEntry(ctx, op, entry), "unreadable row surfaces")

	// malformed ledger body on the live row.
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	row.GeneratedFiles = `{"replacements":broken`
	require.NoError(t, repo.Update(ctx, row))
	require.Error(t, r.consumeReplacementEntry(ctx, op, entry), "malformed ledger surfaces")
}

type rowGoneRepo2 struct {
	*p3OpRepo
	goneID uint
}

func (m *rowGoneRepo2) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	if id == m.goneID {
		return nil, errors.New("office vacated")
	}
	return m.p3OpRepo.FindByID(ctx, id)
}

// checkDestBlocking tolerance legs: reverted rows skip, unparseable rows skip.
func TestCheckDestBlocking_ToleranceLegs(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/dst/TOL/poster.jpg"
	op := &models.BatchFileOperation{BatchJobID: "job-1", MovieID: "TOL-001", OperationType: models.OperationTypeUpdate, RevertStatus: models.RevertStatusApplied}
	require.NoError(t, repo.Create(ctx, op))

	rev := &models.BatchFileOperation{BatchJobID: "job-1", MovieID: "TOL-002", OperationType: models.OperationTypeUpdate, RevertStatus: models.RevertStatusReverted,
		GeneratedFiles: `{"replacements":[{"destination":"/dst/TOL/poster.jpg","backup":"b","dest_seq":9}]}`}
	require.NoError(t, repo.Create(ctx, rev))
	mal := &models.BatchFileOperation{BatchJobID: "job-1", MovieID: "TOL-003", OperationType: models.OperationTypeUpdate, RevertStatus: models.RevertStatusApplied,
		GeneratedFiles: `{"replacements":nope`}
	require.NoError(t, repo.Create(ctx, mal))

	r := NewReverter(fs, repo)
	require.NoError(t, r.checkDestBlocking(ctx, op, dest, 1),
		"reverted+malformed rows never block")
}
