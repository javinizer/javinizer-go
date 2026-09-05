package workflow

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// In-place duplicate-failure e2e rig for codex P1 (PR #241): a REAL sqlite
// revert log + apply orchestrators over ONE memfs, an in-place organizer
// (literal "shared"/"vidfile" formats make both primed claimants land on ONE
// canonical target directory + file regardless of movie ID), and both source
// trees seeded. Every video file name carries match.MovieID ("vidfile") so
// the source folders stay dedicated; the optional foreign plant pre-occupies
// the owner's inner target name inside its old directory.
const (
	w241IPOwnerSrc    = "/pool/oldA/vidfile ownA.mkv"
	w241IPStandbySrc  = "/pool/oldB/vidfile ownB.mkv"
	w241IPPlant       = "/pool/oldA/vidfile.mkv"
	w241IPTargetDir   = "/pool/shared"
	w241IPTarget      = "/pool/shared/vidfile.mkv"
	w241IPOwnerSurviv = "/pool/shared/vidfile ownA.mkv"
)

func newW241InPlaceRig(t *testing.T, withPlant bool) (repo *database.BatchFileOperationRepository, base afero.Fs, tracker *organizer.DuplicateTracker, orchFor func(orgFs afero.Fs) *applyOrchImpl, cmdFor func(movieID, src, name string, force bool) ApplyCmd) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo = database.NewBatchFileOperationRepository(db)
	base = afero.NewMemMapFs()
	rl := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "job-w241-ip", base, nil, nil, nil)

	require.NoError(t, base.MkdirAll(filepath.FromSlash("/pool/oldA"), 0o755))
	require.NoError(t, base.MkdirAll(filepath.FromSlash("/pool/oldB"), 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.FromSlash(w241IPOwnerSrc), []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.FromSlash(w241IPStandbySrc), []byte("b-bytes"), 0o644))
	if withPlant {
		require.NoError(t, afero.WriteFile(base, filepath.FromSlash(w241IPPlant), []byte("plant-bytes"), 0o644))
	}

	m, err := matcher.NewMatcher(&matcher.Config{RegexEnabled: false})
	require.NoError(t, err)
	tracker = primeW241InPlaceTracker(t)
	orchFor = func(orgFs afero.Fs) *applyOrchImpl {
		if orgFs == nil {
			orgFs = base
		}
		org := organizer.NewOrganizer(orgFs, &organizer.Config{
			FolderFormat:  "shared",
			FileFormat:    "vidfile",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeInPlace,
		}, nil, m)
		return &applyOrchImpl{fs: base, organizer: org, revertLog: rl, nfo: &applyStubNFO{}}
	}
	cmdFor = func(movieID, src, name string, force bool) ApplyCmd {
		return ApplyCmd{
			Movie:    &models.Movie{ID: movieID, Title: movieID + " Title"},
			Match:    models.FileMatchInfo{MovieID: "vidfile", Path: src, Name: name, Extension: ".mkv"},
			DestPath: "/dest",
			Organize: OrganizeOptions{MoveFiles: true, ForceUpdate: force, DuplicateTracker: tracker},
		}
	}
	return repo, base, tracker, orchFor, cmdFor
}

// primeW241InPlaceTracker primes the two moving claimants of the shared
// in-place target, owner first, with the case-sensitivity probe frozen so
// canonical keys derive identically on every platform.
func primeW241InPlaceTracker(t *testing.T) *organizer.DuplicateTracker {
	t.Helper()
	prev := fsutil.CaseSensitiveProbe
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return true, nil }
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = prev
		fsutil.ResetCaseSensitivityCache()
	})
	tracker := organizer.NewDuplicateTracker(false)
	tracker.PrimeBatch([]organizer.DuplicatePriming{
		{SourcePath: w241IPOwnerSrc, TargetPath: w241IPTarget, WillMove: true},
		{SourcePath: w241IPStandbySrc, TargetPath: w241IPTarget, WillMove: true},
	})
	return tracker
}

// vanishedOldDirFsW241 reports the owner's old directory as permanently
// absent — the post-priming vanish wedge: planning never stats OldDir
// (ReadDir lands first), execute's first act does.
type vanishedOldDirFsW241 struct {
	afero.Fs
	oldDir string
}

func (p *vanishedOldDirFsW241) Stat(name string) (os.FileInfo, error) {
	if filepath.Clean(name) == filepath.Clean(p.oldDir) {
		return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
	return p.Fs.Stat(name)
}

// w241IPRead reads path through the test's slash spelling, Windows-safe.
func w241IPRead(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, filepath.FromSlash(path))
	require.NoError(t, err)
	return data
}

func w241IPExists(t *testing.T, fs afero.Fs, path string) bool {
	t.Helper()
	exists, err := afero.Exists(fs, filepath.FromSlash(path))
	require.NoError(t, err)
	return exists
}

// w241IPWinnerUnwound asserts the promoted standby's real row unwound by
// exact inverse: its old tree restored with its file back at the original
// name, the shared directory gone.
func w241IPWinnerUnwound(t *testing.T, fs afero.Fs) {
	t.Helper()
	assert.Equal(t, []byte("b-bytes"), w241IPRead(t, fs, w241IPStandbySrc))
	assert.False(t, w241IPExists(t, fs, w241IPTargetDir), "the standby's renamed directory unwound")
}

// TestApply_InPlaceVanishedOldDir_RevertCannotDragStandbyDirectory is codex
// P1 (PR #241) finding leg (a) end-to-end: an in-place owner whose SOURCE
// DIRECTORY vanished between priming and execution fails before any rename —
// mutation-free — yet the blanket !plan.InPlace exemption used to keep its
// row journaled with the shared target, so once the released claim promoted
// the standby (whose in-place rename now OWNS /pool/shared), reverting the
// failed row would rename the standby's directory into the failed owner's
// original path. The failed row must now finalize completed-noop with NO
// target fields; its revert is a pure no-op and the standby's directory is
// untouched.
func TestApply_InPlaceVanishedOldDir_RevertCannotDragStandbyDirectory(t *testing.T) {
	ctx := context.Background()

	for _, force := range []bool{false, true} {
		mode := "normal mode"
		if force {
			mode = "force mode"
		}
		t.Run(mode, func(t *testing.T) {
			repo, base, _, orchFor, cmdFor := newW241InPlaceRig(t, false)
			poison := &vanishedOldDirFsW241{Fs: base, oldDir: "/pool/oldA"}

			failRes, failErr := orchFor(poison).Execute(ctx, cmdFor("ABC-100", w241IPOwnerSrc, "vidfile ownA.mkv", force))
			require.Error(t, failErr, "the vanished source dir fails the organize step")
			require.NotNil(t, failRes)
			assert.Equal(t, "organize", failRes.FailedStep)
			assert.True(t, failRes.PrePublication)
			require.NotNil(t, failRes.OrganizeResult)
			assert.True(t, failRes.OrganizeResult.PrePublication,
				"an in-place failure with NOTHING surviving marks pre-publication exactly like the non-in-place legs")
			assert.False(t, failRes.OrganizeResult.InPlaceRenamed)
			assert.False(t, fsutil.PublishCompleted(failErr),
				"nothing renamed — no partial-publish class")

			okRes, okErr := orchFor(nil).Execute(ctx, cmdFor("ABC-200", w241IPStandbySrc, "vidfile ownB.mkv", force))
			require.NoError(t, okErr, "the released claim promotes the standby onto the freed target")
			require.NotNil(t, okRes.OrganizeResult)
			assert.True(t, okRes.OrganizeResult.Moved)
			assert.True(t, okRes.OrganizeResult.InPlaceRenamed)
			assert.Equal(t, []byte("b-bytes"), w241IPRead(t, base, w241IPTarget))

			// Journal truth: the failed owner journaled NO target fields and
			// finalized completed-noop.
			rowF := w241Row(t, repo, failRes.OperationID)
			assert.Equal(t, models.RevertStatusNoOp, rowF.RevertStatus)
			assert.Empty(t, rowF.NewPath)
			assert.False(t, rowF.InPlaceRenamed)
			rowW := w241Row(t, repo, okRes.OperationID)
			assert.Equal(t, models.RevertStatusApplied, rowW.RevertStatus)
			assert.Equal(t, w241IPTarget, filepath.ToSlash(rowW.NewPath),
				"the promoted standby owns the shared target's revert record")
			assert.True(t, rowW.InPlaceRenamed)

			reverter := history.NewReverter(base, repo)

			// Reverting the failed OWNER row: pure no-op — the standby's
			// renamed directory is never its subject.
			rb, err := reverter.RevertScrape(ctx, "job-w241-ip", "ABC-100")
			require.NoError(t, err)
			assert.Zero(t, rb.Total)
			assert.Empty(t, rb.Outcomes)
			assert.Equal(t, []byte("b-bytes"), w241IPRead(t, base, w241IPTarget),
				"the standby's published bytes are untouched")
			assert.True(t, w241IPExists(t, base, w241IPTargetDir), "the standby's folder path stands")
			assert.True(t, w241IPExists(t, base, w241IPOwnerSrc),
				"nothing drags the standby's directory back onto the owner's old path")

			// The batch then fully reverts on the standby's real row alone.
			rb, err = reverter.RevertBatch(ctx, "job-w241-ip")
			require.NoError(t, err)
			require.Equal(t, 1, rb.Total)
			assert.Equal(t, 1, rb.Succeeded)
			assert.Zero(t, rb.Skipped)
			assert.Zero(t, rb.Failed)
			w241IPWinnerUnwound(t, base)
			_, err = reverter.RevertBatch(ctx, "job-w241-ip")
			require.ErrorIs(t, err, history.ErrBatchAlreadyReverted, "noop + reverted leaves nothing behind")
		})
	}
}

// TestApply_InPlaceInnerRefusalRollbackLanded_RevertNoOpEverywhere is codex
// P1 finding leg (b) end-to-end: the owner's directory rename LANDS, the
// inner file rename is refused by the foreign plant, and the rollback
// succeeds — nothing survived. The pre-fix journal kept InPlaceRenamed=true
// with the shared target even here (the result's marker predated rollback
// awareness), arming the failed row's revert against the standby's renamed
// directory. The rolled-back failure now finalizes completed-noop and its
// revert is a no-op everywhere, including the folder path.
func TestApply_InPlaceInnerRefusalRollbackLanded_RevertNoOpEverywhere(t *testing.T) {
	ctx := context.Background()
	repo, base, _, orchFor, cmdFor := newW241InPlaceRig(t, true)

	failRes, failErr := orchFor(nil).Execute(ctx, cmdFor("ABC-100", w241IPOwnerSrc, "vidfile ownA.mkv", false))
	require.Error(t, failErr)
	assert.Contains(t, filepath.ToSlash(failErr.Error()), "vidfile.mkv",
		"the foreign inner-target refusal surfaces")
	assert.False(t, fsutil.PublishCompleted(failErr))
	require.NotNil(t, failRes)
	assert.True(t, failRes.PrePublication)
	require.NotNil(t, failRes.OrganizeResult)
	assert.True(t, failRes.OrganizeResult.PrePublication)
	assert.False(t, failRes.OrganizeResult.InPlaceRenamed,
		"the landed rollback cleared the rename marker — nothing survived")

	okRes, okErr := orchFor(nil).Execute(ctx, cmdFor("ABC-200", w241IPStandbySrc, "vidfile ownB.mkv", false))
	require.NoError(t, okErr, "the released claim promotes the standby")
	assert.True(t, okRes.OrganizeResult.Moved)
	assert.Equal(t, []byte("b-bytes"), w241IPRead(t, base, w241IPTarget))

	rowF := w241Row(t, repo, failRes.OperationID)
	assert.Equal(t, models.RevertStatusNoOp, rowF.RevertStatus)
	assert.Empty(t, rowF.NewPath)
	assert.False(t, rowF.InPlaceRenamed)

	reverter := history.NewReverter(base, repo)
	rb, err := reverter.RevertScrape(ctx, "job-w241-ip", "ABC-100")
	require.NoError(t, err)
	assert.Zero(t, rb.Total)
	assert.Equal(t, []byte("a-bytes"), w241IPRead(t, base, w241IPOwnerSrc),
		"the rolled-back owner kept its bytes at its own path")
	assert.Equal(t, []byte("plant-bytes"), w241IPRead(t, base, w241IPPlant))
	assert.Equal(t, []byte("b-bytes"), w241IPRead(t, base, w241IPTarget),
		"the standby's directory is untouched by the failed row's revert")

	rb, err = reverter.RevertBatch(ctx, "job-w241-ip")
	require.NoError(t, err)
	require.Equal(t, 1, rb.Total)
	assert.Equal(t, 1, rb.Succeeded)
	w241IPWinnerUnwound(t, base)
	// The rolled-back owner's tree is byte-intact after the full batch unwind.
	assert.Equal(t, []byte("a-bytes"), w241IPRead(t, base, w241IPOwnerSrc))
	assert.Equal(t, []byte("plant-bytes"), w241IPRead(t, base, w241IPPlant))
	_, err = reverter.RevertBatch(ctx, "job-w241-ip")
	require.ErrorIs(t, err, history.ErrBatchAlreadyReverted)
}

// w241IPRollbackRefusedFs refuses exactly ONE rename of the rollback pair —
// the failed owner's directory rollback — deterministically and
// cross-platform.
type w241IPRollbackRefusedFs struct {
	afero.Fs
	old, new string
	fired    atomic.Bool
}

func (p *w241IPRollbackRefusedFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(p.old) && filepath.Clean(newname) == filepath.Clean(p.new) && p.fired.CompareAndSwap(false, true) {
		return &os.PathError{Op: "rename", Path: oldname, Err: syscall.EACCES}
	}
	return p.Fs.Rename(oldname, newname)
}

// TestApply_InPlaceRenameSurvived_SettlesAndJournalsActualBytes is codex P1
// finding leg (c) with the pinned decision: the inner rename is refused AND
// the directory rollback is refused, so the directory rename SURVIVES on
// disk. Surviving mutation is publication-equivalent — the destination NAME
// physically changed to the failed owner's target — so the claim SETTLES
// (never releases: no standby can promote and double-attach onto it), the
// row keeps journal semantics (revertable-failed, NOT completed-noop), and
// the journal names where the bytes actually went — the OLD file name inside
// the renamed directory — making the failed row's revert an exact-inverse
// unwind of precisely the surviving mutation.
func TestApply_InPlaceRenameSurvived_SettlesAndJournalsActualBytes(t *testing.T) {
	ctx := context.Background()
	repo, base, _, orchFor, cmdFor := newW241InPlaceRig(t, true)

	poison := &w241IPRollbackRefusedFs{Fs: base, old: "/pool/shared", new: "/pool/oldA"}
	failRes, failErr := orchFor(poison).Execute(ctx, cmdFor("ABC-100", w241IPOwnerSrc, "vidfile ownA.mkv", false))
	require.Error(t, failErr)
	assert.False(t, fsutil.PublishCompleted(failErr), "the inner publish never landed")
	assert.True(t, poison.fired.Load(), "the rollback rename was attempted and refused")
	require.NotNil(t, failRes)
	assert.False(t, failRes.PrePublication,
		"a surviving rename is NOT pre-publication — journal semantics stand")
	require.NotNil(t, failRes.OrganizeResult)
	assert.False(t, failRes.OrganizeResult.PrePublication)
	assert.True(t, failRes.OrganizeResult.InPlaceRenamed, "the directory rename survived")
	assert.Equal(t, filepath.FromSlash(w241IPOwnerSurviv), failRes.OrganizeResult.NewPath,
		"the result names where the bytes actually went")

	// On disk: the shared directory stands with the owner's file at its OLD
	// name beside the foreign plant; the owner's original path is gone.
	assert.True(t, w241IPExists(t, base, w241IPTargetDir))
	assert.Equal(t, []byte("a-bytes"), w241IPRead(t, base, w241IPOwnerSurviv))
	assert.Equal(t, []byte("plant-bytes"), w241IPRead(t, base, filepath.Join(w241IPTargetDir, filepath.Base(w241IPPlant))))
	assert.False(t, w241IPExists(t, base, "/pool/oldA"))

	// SETTLED claim: the authorized standby never promotes onto the surviving
	// directory — its own in-place plan refuses the occupied directory name
	// (in-place never swaps a foreign folder, #224); its bytes stay home and
	// its row finalizes completed-noop like every pre-publication terminal.
	standbyRes, standbyErr := orchFor(nil).Execute(ctx, cmdFor("ABC-200", w241IPStandbySrc, "vidfile ownB.mkv", true))
	require.Error(t, standbyErr)
	assert.Contains(t, filepath.ToSlash(standbyErr.Error()), w241IPTargetDir,
		"the surviving directory refuses the standby's rename")
	require.NotNil(t, standbyRes)
	assert.True(t, standbyRes.PrePublication,
		"the standby published nothing either")
	rowS := w241Row(t, repo, standbyRes.OperationID)
	assert.Equal(t, models.RevertStatusNoOp, rowS.RevertStatus)
	assert.Equal(t, []byte("b-bytes"), w241IPRead(t, base, w241IPStandbySrc),
		"the standby's bytes never left its source")

	// Journal truth: the failed owner's row kept journal semantics, named at
	// the bytes' actual location, revertable-failed.
	rowF := w241Row(t, repo, failRes.OperationID)
	assert.Equal(t, models.RevertStatusFailed, rowF.RevertStatus)
	assert.Equal(t, w241IPOwnerSurviv, filepath.ToSlash(rowF.NewPath),
		"the journal names where the bytes actually went")
	assert.True(t, rowF.InPlaceRenamed)
	assert.Equal(t, "/pool/oldA", filepath.ToSlash(rowF.OriginalDirPath))

	// Reverting the failed owner unwinds EXACTLY the surviving mutation: the
	// directory rename inverts, both files return byte-intact, and no
	// standby-owned name is ever touched.
	reverter := history.NewReverter(base, repo)
	rb, err := reverter.RevertScrape(ctx, "job-w241-ip", "ABC-100")
	require.NoError(t, err)
	require.Equal(t, 1, rb.Total)
	assert.Equal(t, 1, rb.Succeeded)
	assert.Equal(t, []byte("a-bytes"), w241IPRead(t, base, w241IPOwnerSrc),
		"the owner's file returned to its original path")
	assert.Equal(t, []byte("plant-bytes"), w241IPRead(t, base, w241IPPlant),
		"the foreign plant returned inside the restored directory")
	assert.False(t, w241IPExists(t, base, w241IPTargetDir),
		"the surviving rename unwound — nothing left at the shared name")
	assert.Equal(t, []byte("b-bytes"), w241IPRead(t, base, w241IPStandbySrc))

	// Nothing revertable remains: the standby's row was completed-noop, the
	// owner's row reverted.
	_, err = reverter.RevertBatch(ctx, "job-w241-ip")
	require.ErrorIs(t, err, history.ErrBatchAlreadyReverted)
}
