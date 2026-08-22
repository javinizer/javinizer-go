package history

// POSTER-WRITE-HARDENING codex local review round 4 (PR#215 findings F1–F4):
//
//   - F1 (sweeper, replacement_sweep_p3.go): the restore-undo unlinks
//     (undoRestore after a failed journal read/marker persistence, and the
//     consumption-failure + re-arm-succeeded leg) ran by PATHNAME after the
//     wave-31/wave-32 destination re-gates passed; a foreign swap inside the
//     gate→undo window turned the undo into deleting a foreign file. Both
//     unlinks are now identity-bound to the published restore object
//     (restoredDestStillOurs re-derivation + SameFile): divergence retains
//     the destination AND the backup with the journal entry left live.
//   - F2 (reverter, reverter_replacements_p3.go): the cleanup-failure +
//     marker-persistence-failure compensation trusted destMissingBeforeRestore
//     (the PRE-COPY Lstat) and deleted whatever dest then named. The undo is
//     now bound to the identity THIS pass published (restoredID): a pending
//     leg published nothing (never unlinks), an indeterminate verdict fails
//     closed, and only a destination still naming the published object is
//     removed (the established R9-2 undo).
//   - F3 (publish cleanup): an fsutil.ErrPublishCompleted-carrying publish
//     error (wave-33's ErrPublishNoReplaceStagedUnverified, joined into the
//     completed class) left the staged name in place DELIBERATELY — it may
//     address a foreign object. The three staged-cleanup arms (restore
//     publish, re-arm publish, downloader rollback — the latter in its own
//     package test) no longer remove the staged name for that class.
//   - F4 (pre-sweep scope): the direct RevertBatch/RevertScrape pre-sweep
//     collected only journaled destinations; a process dying between the
//     destination move-aside and RecordReplacement leaves no journaled entry,
//     so the stranded backup under a Begin-persisted root was never healed.
//     opSweepRoots unions gf.Roots with the replacement destination dirs and
//     the pre-sweep scans them (SweepDirs) after the unchanged destination
//     sweep.
//
// Seam discipline: the publish→undo and quarantine→undo instants are
// unreachable for a filesystem double on the real OsFs (the wave-30 identity
// gate requires the native descriptor), so the verdict instants ride the
// scripted restoredDestStillOurs seam exactly like waves 31/32 — while the
// FOREIGN destination object is physically planted at the precise fs/repo
// hook instant, so a pathname-bound undo would demonstrably have deleted it.

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w34FailJournalTxRepo fails the chosen 1-based UpdateJournalInTx call and,
// at that same instant, plants FOREIGN bytes at the given destination (the
// finding F1/F2 publish→undo and quarantine→undo window swap). The physical
// plant + the scripted identity verdict prove the identity-bound undo refuses
// where the pre-wave-34 pathname unlink would have destroyed foreign bytes.
type w34FailJournalTxRepo struct {
	*p3OpRepo
	calls   int
	failAt  int
	err     error
	fs      afero.Fs
	dest    string
	foreign []byte
}

func (m *w34FailJournalTxRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	m.calls++
	if m.calls == m.failAt {
		if m.fs != nil && m.dest != "" {
			if err := afero.WriteFile(m.fs, m.dest, m.foreign, 0o600); err != nil {
				return err
			}
		}
		return m.err
	}
	return m.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

// w34QuarantineUnlinkWedge fails the quarantine-name unlink with a plain
// error and plants FOREIGN bytes at dest at that instant — the finding
// F1-markErr/F2 swap in the quarantine→undo window.
type w34QuarantineUnlinkWedge struct {
	afero.Fs
	backup  string
	dest    string
	foreign []byte
	err     error
}

func (f *w34QuarantineUnlinkWedge) Remove(name string) error {
	norm := strings.ReplaceAll(name, "\\", "/")
	if norm == f.backup || strings.HasPrefix(norm, f.backup+backupQuarantineSuffix) {
		if norm != f.backup {
			// Wave-42: the conditional handoff also unlinks its 0-byte
			// take-aside placeholder under the same suffix — a warn-only
			// release the wedge must never hit. Only the NON-EMPTY
			// quarantined verified object is the wedged instant.
			if info, serr := f.Fs.Stat(name); serr != nil || info.Size() == 0 {
				return f.Fs.Remove(name)
			}
		}
		if f.dest != "" {
			_ = afero.WriteFile(f.Fs, f.dest, f.foreign, 0o600)
		}
		return f.err
	}
	return f.Fs.Remove(name)
}

// w34QuarantineMoveWedge fails the quarantining RENAME (backup → its ".dlq."
// sibling) and plants FOREIGN bytes at dest at that instant — the pending-leg
// variant of the window (finding F2): the move never happened, so the journaled
// backup name keeps its own bytes untouched.
type w34QuarantineMoveWedge struct {
	afero.Fs
	backup  string
	dest    string
	foreign []byte
	err     error
}

func (f *w34QuarantineMoveWedge) Rename(oldname, newname string) error {
	norm := strings.ReplaceAll(newname, "\\", "/")
	if strings.HasPrefix(norm, f.backup+backupQuarantineSuffix) {
		if f.dest != "" {
			_ = afero.WriteFile(f.Fs, f.dest, f.foreign, 0o600)
		}
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

// w34StagedNames lists the staged-restore/re-arm leftovers in dir (the
// transient suffix grammars, never the .dlbak ownership markers).
func w34StagedNames(t *testing.T, fs afero.Fs, dir string) []string {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.Contains(n, ".rstr.") || strings.Contains(n, rearmStagingSuffix+".") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// F1, undoRestore leg (journal read transaction failed inside the armed
// sweep): the destination swapped foreign at the failing-transaction instant;
// the scripted verdict answers the wave-31 publish re-gate true, then the
// undo re-gate false. The undo must leave the foreign object byte-intact, the
// backup in place, and the journal entry live.
func TestSweepW34_UndoRestoreRefusedAfterForeignDestSwap(t *testing.T) {
	base := afero.NewMemMapFs()
	ctx := context.Background()
	dir := "/out/W34-UNDO"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "old", time.Hour)
	baseRepo := newP3OpRepo()
	op := journalRow(t, baseRepo, "job-w34", "W34-UNDO", dest, backup, 1, models.RevertStatusApplied)

	readErr := errors.New("w34 journal read wedged")
	repo := &w34FailJournalTxRepo{p3OpRepo: baseRepo, failAt: 1, err: readErr, fs: base, dest: dest, foreign: []byte("w34 foreign swap-victim")}
	w32ScriptRestoredDestSeam(t, true, false)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	healed, err := NewReplacementSweeper(base, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "the refused undo heals nothing")

	require.Equal(t, "w34 foreign swap-victim", string(mustRead2(t, base, dest)),
		"the identity-bound undo NEVER deletes the foreign occupant (pre-wave-34 the pathname unlink removed it)")
	require.Equal(t, "old", string(mustRead2(t, base, backup)), "the backup stays for the retry")
	row, rerr := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, rerr)
	require.Len(t, w25JournalEntries(t, baseRepo, op.ID), 1, "the journal entry stays live")
	require.NotEqual(t, models.RevertStatusReverted, row.RevertStatus)

	out := logs.String()
	require.Contains(t, out, "restore undo REFUSED", "the refusal names itself")
	require.Contains(t, out, "journal entry left live")
}

// F1, undoRestore leg (marker persistence failed after a failed backup
// removal): the foreign swap lands at the wedged quarantine-unlink instant;
// the scripted verdict walks wave-31 → post-quarantine re-gate → undo re-gate
// as true, true, false. Destination AND backup retained, entry armed.
func TestSweepW34_MarkerFailureUndoRefusedAfterDestDivergence(t *testing.T) {
	base := afero.NewMemMapFs()
	ctx := context.Background()
	dir := "/out/W34-MARK"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "old", time.Hour)
	baseRepo := newP3OpRepo()
	op := journalRow(t, baseRepo, "job-w34", "W34-MARK", dest, backup, 1, models.RevertStatusApplied)

	markErr := errors.New("w34 marker persist wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: markErr}}
	removeErr := errors.New("w34 quarantine unlink wedged")
	fs := &w34QuarantineUnlinkWedge{Fs: base, backup: backup, dest: dest, foreign: []byte("w34 dest plant"), err: removeErr}
	w32ScriptRestoredDestSeam(t, true, true, false)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)

	require.Equal(t, "w34 dest plant", string(mustRead2(t, base, dest)),
		"the diverged destination is retained — the undo never unlinks a foreign object")
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the wedged quarantine unlink compensated the verified object back onto the journaled name")
	entries := w25JournalEntries(t, baseRepo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "the failed marker persist leaves the entry armed")

	out := logs.String()
	require.Contains(t, out, "failed to retain cleanup marker", "the persistence failure still surfaces")
	require.Contains(t, out, "restore undo REFUSED")
}

// F1, consumption-failure + re-arm-succeeded leg (the line-1105 unlink): the
// scripted verdict walks true, true, false. Because the re-arm succeeded, the
// journal entry stays armed AND the re-armed backup keeps the old bytes —
// while the diverged destination is retained instead of unlinked.
func TestSweepW34_ConsumptionFailureUndoRefusedAfterDestDivergence(t *testing.T) {
	base := afero.NewMemMapFs()
	ctx := context.Background()
	dir := "/out/W34-CONSUME"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "old", time.Hour)
	baseRepo := newP3OpRepo()
	op := journalRow(t, baseRepo, "job-w34", "W34-CONSUME", dest, backup, 1, models.RevertStatusApplied)

	consumeErr := errors.New("w34 consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: consumeErr}}
	w32ScriptRestoredDestSeam(t, true, true, false)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	healed, err := NewReplacementSweeper(base, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)

	require.Equal(t, "old", string(mustRead2(t, base, dest)),
		"the restored destination is retained — the pathname unlink never ran against a diverged identity")
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the SUCCEEDED re-arm already re-established the backup before the refused undo")
	entries := w25JournalEntries(t, baseRepo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "no marker: the entry stays armed for the retry")

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, "restore undo REFUSED")
	require.Contains(t, out, "re-armed backup retained")
}

// F2, armed reverter leg: cleanup (wedged quarantine unlink) + marker
// persistence both fail; the foreign plant lands at the wedged-unlink
// instant; the scripted verdict walks true, true, false. The compensation
// must NOT unlink the foreign destination — the pre-F2 pathname undo trusted
// only the pre-copy Lstat and deleted it.
func TestRestoreReplacementJournalW34_UndoRetainsForeignDestAfterSwap(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup := newW27ArmedReplacement(t, base, "W34-RV-SWAP")
	removeErr := errors.New("w34 backup remove wedged")
	fs := &w34QuarantineUnlinkWedge{Fs: base, backup: backup, dest: dest, foreign: []byte("w34 foreign dest"), err: removeErr}
	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)
	markerErr := errors.New("w34 marker update transient")
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: markerErr}
	w32ScriptRestoredDestSeam(t, true, true, false)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	restored, err := NewReverter(fs, failingRepo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, removeErr)
	require.True(t, restored[dest])
	require.Equal(t, "w34 foreign dest", string(mustRead2(t, base, dest)),
		"the foreign occupant survives — the undo is bound to the published identity, not the pre-copy Lstat")
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the verified object is back at the journaled name for the retry")

	row, ferr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, ferr)
	gf, gerr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, gerr)
	require.Len(t, gf.Replacements, 1, "the entry stays armed")
	require.False(t, gf.Replacements[0].RestorePending)

	out := logs.String()
	require.Contains(t, out, "cleanup marker persistence failed")
	require.Contains(t, out, "no longer names the published restore object")
	require.NotContains(t, out, "restore undone, will retry", "no undo ran against the foreign occupant")
}

// F2 control: the same leg with the destination STILL naming the published
// object (scripted true verdicts) runs the established R9-2 undo — the
// pre-crash absent state is reproduced.
func TestRestoreReplacementJournalW34_UndoRunsWhenDestStillOurs(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup := newW27ArmedReplacement(t, base, "W34-RV-UNDO")
	removeErr := errors.New("w34 backup remove wedged")
	fs := &w34QuarantineUnlinkWedge{Fs: base, backup: backup, err: removeErr}
	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: errors.New("w34 marker update transient")}
	w32ScriptRestoredDestSeam(t, true, true, true)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	_, err := NewReverter(fs, failingRepo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, removeErr)
	exists, eerr := afero.Exists(base, dest)
	require.NoError(t, eerr)
	require.False(t, exists, "an identity-matched undo reproduces the proven-absent pre-restore state")
	require.Equal(t, "old", string(mustRead2(t, base, backup)), "the backup survives for the retry")
	require.Contains(t, logs.String(), "restore undone, will retry")
}

// F2, pending-clean leg: this pass published NOTHING (the durable marker
// certified the destination bytes), so an occupant at a destination the
// pre-copy Lstat proved missing is necessarily foreign. The pre-F2
// compensation unlinked it anyway; the wave-34 pending case retains it
// without even consulting the publish identity (there is none this pass).
func TestRestoreReplacementJournalW34_PendingLegNeverDeletesForeignOccupant(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W34-RV-PEND/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W34-RV-PEND", 0o755))
	writeSweepFile(t, base, backup, "certified-old", time.Hour)
	repo := newP3OpRepo()
	op := &models.BatchFileOperation{
		BatchJobID: "job-w34", MovieID: "W34-RV-PEND", OriginalPath: "/src/w34-rv-pend.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination: dest, Backup: backup, DestSeq: 1,
				Installed: true, RestorePending: true,
			}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))

	moveErr := errors.New("w34 quarantine move wedged")
	fs := &w34QuarantineMoveWedge{Fs: base, backup: backup, dest: dest, foreign: []byte("w34 foreign create"), err: moveErr}
	markerErr := errors.New("w34 marker update transient")
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: markerErr}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	_, err := NewReverter(fs, failingRepo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, moveErr)
	require.Equal(t, "w34 foreign create", string(mustRead2(t, base, dest)),
		"the foreign create survives — a pending leg never published this pass, so nothing licenses an unlink")
	require.Equal(t, "certified-old", string(mustRead2(t, base, backup)),
		"the wedged quarantine move never relocated the backup")
	row, ferr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, ferr)
	require.True(t, journalEntryRestorePending(row, sweepSlash(backup)),
		"the durable pending entry is unchanged (the marker persistence failed)")

	out := logs.String()
	require.Contains(t, out, "this pending leg published nothing")
	require.NotContains(t, out, "restore undone, will retry")
}

// F3, restore publish cleanup: the typed staged-cleanup refusal (wave-33's
// ErrPublishNoReplaceStagedUnverified, joined with ErrPublishCompleted) means
// the destination provably carries our bytes while fsutil DELIBERATELY left
// the staged name — possibly foreign. The arm must keep it; the error
// surface stays the "swap staged restore" wrap with both sentinels reachable.
func TestCopyRestoreBytesPublishW34_PublishCompletedKeepsStagedName(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w34-p3", 0o755))
	backup := "/w34-p3/poster.jpg.dlbak." + p3HexA
	dest := "/w34-p3/poster.jpg"
	require.NoError(t, afero.WriteFile(base, backup, []byte("restore bytes"), 0o644))

	unverified := errors.New("w34 staged reverify mismatch")
	pubErr := error(fsutil.ErrPublishNoReplaceStagedUnverified)
	pubErr = errors.Join(pubErr, unverified, fsutil.ErrPublishCompleted)
	stub := func(afero.Fs, string, string) error { return pubErr }

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	_, err := copyRestoreBytesPublish(base, backup, dest, stub, true, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "swap staged restore")
	require.ErrorIs(t, err, fsutil.ErrPublishCompleted)
	require.ErrorIs(t, err, fsutil.ErrPublishNoReplaceStagedUnverified)

	staged := w34StagedNames(t, base, "/w34-p3")
	require.Len(t, staged, 1, "the possibly-foreign staged name is LEFT byte-intact — pre-F3 the arm unlinked it")
	require.Equal(t, "restore bytes", string(mustRead2(t, base, "/w34-p3/"+staged[0])))
	require.Equal(t, "restore bytes", string(mustRead2(t, base, backup)), "the restore source is untouched")
	require.Contains(t, logs.String(), "left in place")
}

// F3 control: publish failures that prove NOTHING was installed keep dropping
// their own staged copy (the pre-wave-34 cleanup, unchanged).
func TestCopyRestoreBytesPublishW34_NonCompletedFailuresDropStagedCopy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pubErr error
	}{
		{"collision refusal", fsutil.ErrPublishCollision},
		{"unsupported refusal", fsutil.ErrPublishNoReplaceUnsupported},
		{"plain publish failure", errors.New("w34 publish wedged")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			require.NoError(t, base.MkdirAll("/w34-p3c", 0o755))
			backup := "/w34-p3c/poster.jpg.dlbak." + p3HexA
			dest := "/w34-p3c/poster.jpg"
			require.NoError(t, afero.WriteFile(base, backup, []byte("restore bytes"), 0o644))
			stub := func(afero.Fs, string, string) error { return tc.pubErr }

			_, err := copyRestoreBytesPublish(base, backup, dest, stub, true, nil)
			require.ErrorContains(t, err, "swap staged restore")
			require.Empty(t, w34StagedNames(t, base, "/w34-p3c"),
				"an unpublished staged copy is still dropped — only the completed class retains")
			require.Equal(t, "restore bytes", string(mustRead2(t, base, backup)))
		})
	}
}

// F3, re-arm publish cleanup: identical discipline through the rearmPublishFn
// seam — the completed class keeps the staged name, the error keeps the
// "re-arm install backup" wrap, and the caller's pending-kind routing still
// classifies the name as OWNED (clean).
func TestCopyRearmSourceBytesW34_PublishCompletedKeepsStagedName(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w34-p3r", 0o755))
	dest := "/w34-p3r/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, afero.WriteFile(base, dest, []byte("rearm bytes"), 0o644))
	src, err := openRearmSource(base, dest)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()
	info, err := src.Stat()
	require.NoError(t, err)

	pubErr := errors.Join(fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)
	prev := rearmPublishFn
	rearmPublishFn = func(afero.Fs, string, string) error { return pubErr }
	t.Cleanup(func() { rearmPublishFn = prev })

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	err = copyRearmSourceBytes(base, src, backup, info)
	require.ErrorContains(t, err, "re-arm install backup")
	require.ErrorIs(t, err, fsutil.ErrPublishCompleted)
	require.ErrorIs(t, err, fsutil.ErrPublishNoReplaceStagedUnverified)
	staged := w34StagedNames(t, base, "/w34-p3r")
	require.Len(t, staged, 1, "the possibly-foreign staged name is retained")
	require.Equal(t, "rearm bytes", string(mustRead2(t, base, "/w34-p3r/"+staged[0])))
	require.Contains(t, logs.String(), "left in place")
	require.Equal(t, models.RestorePendingKindClean, rearmPendingKind(err),
		"the caller's routing still reads the completed class as an OWNED name (the pending retry reaps it)")
}

// F3 re-arm control: a plain pre-publish-completed failure still drops the
// staged copy.
func TestCopyRearmSourceBytesW34_NonCompletedFailureDropsStagedCopy(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w34-p3rc", 0o755))
	dest := "/w34-p3rc/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, afero.WriteFile(base, dest, []byte("rearm bytes"), 0o644))
	src, err := openRearmSource(base, dest)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	plain := errors.New("w34 publish wedged")
	prev := rearmPublishFn
	rearmPublishFn = func(afero.Fs, string, string) error { return plain }
	t.Cleanup(func() { rearmPublishFn = prev })

	err = copyRearmSourceBytes(base, src, backup, nil)
	require.ErrorContains(t, err, "re-arm install backup")
	require.NotErrorIs(t, err, fsutil.ErrPublishCompleted)
	require.Empty(t, w34StagedNames(t, base, "/w34-p3rc"))
}

// F4 unit legs: the per-operation sweep roots union the Begin-persisted roots
// with the replacement destination directories, dedupe the cleaned forms, and
// contribute nothing for missing/unparseable ledgers.
func TestOpSweepRootsW34(t *testing.T) {
	op := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Roots: []string{"/media/a", "", "/media/a/"},
		Replacements: []models.ReplacementEntry{
			{Destination: "/media/b/poster.jpg", Backup: "/media/b/poster.jpg.dlbak." + p3HexA},
			{Destination: "/media/a/fanart.jpg", Backup: "/media/a/fanart.jpg.dlbak." + p3HexB},
		},
	})}
	require.Equal(t, []string{"/media/a", "/media/b"}, opSweepRoots(op),
		"roots ∪ replacement destination directories, cleaned and deduped")

	require.Nil(t, opSweepRoots(&models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`}),
		"an unparseable ledger contributes nothing (the destination collection skips it too)")
	require.Nil(t, opSweepRoots(&models.BatchFileOperation{}), "no ledger, no roots")
}

// F4 integration: an operation with Begin-persisted roots but NO replacement
// entries (the downloader died between the move-aside and RecordReplacement)
// has its roots scanned by the direct-revert pre-sweep; the stranded,
// journaled-nowhere backup heals its missing destination through the orphan
// leg while the marker itself stays for inspection. An operation carrying no
// roots at all changes nothing (the pre-sweep scope was destinations-only
// before wave-34 and stays that way).
func TestSweepJournaledDestinationsW34_RootsScopeHealsStrandedBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W34-SWP"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "stranded-original", time.Hour)

	op := models.BatchFileOperation{
		BatchJobID: "job-w34-swp", MovieID: "W34-SWP", OriginalPath: "/src/w34-swp.mkv",
		OperationType:  models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{dir}}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, &op))

	NewReverter(base, repo).sweepJournaledDestinations(ctx, []models.BatchFileOperation{op})
	require.Equal(t, "stranded-original", string(mustRead2(t, base, dest)),
		"the roots sweep sees and heals the stranded backup the destination-only set could not name")
	exists, err := afero.Exists(base, backup)
	require.NoError(t, err)
	require.True(t, exists, "orphan posture retains the unjournaled marker for manual inspection")

	// Control: same stranded layout but the operation carries NO roots — the
	// pre-sweep scope is unchanged for it and heals nothing.
	dest2 := "/out/W34-SWP2/poster.jpg"
	backup2 := dest2 + ".dlbak." + p3HexB
	require.NoError(t, base.MkdirAll("/out/W34-SWP2", 0o755))
	writeSweepFile(t, base, backup2, "stranded-2", time.Hour)
	op2 := models.BatchFileOperation{
		BatchJobID: "job-w34-swp2", MovieID: "W34-SWP2", OriginalPath: "/src/w34-swp2.mkv",
		OperationType:  models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{}),
		RevertStatus:   models.RevertStatusApplied,
	}
	NewReverter(base, repo).sweepJournaledDestinations(ctx, []models.BatchFileOperation{op2})
	exists, err = afero.Exists(base, dest2)
	require.NoError(t, err)
	require.False(t, exists, "a roots-less, replacement-less op sweeps nothing (pre-wave-34 scope unchanged)")
}

// F4 seam legs: the roots invocation receives exactly the unioned directory
// scope (deduped across ops), a roots-leg failure surfaces best-effort, and a
// destinations-leg failure keeps its first-error priority for the caller's
// established log line.
func TestSweepJournaledDestinationsW34_SeamLegs(t *testing.T) {
	ctx := context.Background()

	t.Run("roots receive the unioned dirs and their failure surfaces", func(t *testing.T) {
		dest := "/out/W34-SEAM/poster.jpg"
		backup := dest + ".dlbak." + p3HexA
		ops := []models.BatchFileOperation{
			{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
				Roots:        []string{"/out/W34-ROOTA"},
				Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, DestSeq: 1}},
			})},
			{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
				Roots: []string{"/out/W34-ROOTA", "/out/W34-ROOTB"},
			})},
		}
		var gotDests, gotRoots []string
		rootsErr := errors.New("w34 roots wedged")
		prevD, prevR := reverterSweepDestinations, reverterSweepRoots
		reverterSweepDestinations = func(context.Context, *ReplacementSweeper, []string) (int, error) {
			gotDests = []string{dest}
			return 0, nil
		}
		reverterSweepRoots = func(_ context.Context, _ *ReplacementSweeper, dirs []string) (int, error) {
			gotRoots = append([]string(nil), dirs...)
			return 0, rootsErr
		}
		t.Cleanup(func() { reverterSweepDestinations, reverterSweepRoots = prevD, prevR })

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		defer restoreLog()

		r := &Reverter{sweeper: &ReplacementSweeper{}}
		r.sweepJournaledDestinations(ctx, ops)
		require.Equal(t, []string{dest}, gotDests, "the destination set is exactly the journaled one (unchanged)")
		require.ElementsMatch(t, []string{"/out/W34-ROOTA", "/out/W34-ROOTB", "/out/W34-SEAM"}, gotRoots,
			"the roots seam receives roots ∪ replacement dirs, deduped across ops")
		require.Contains(t, logs.String(), "pre-revert replacement sweep failed: "+rootsErr.Error()+" (continuing with revert)")
	})

	t.Run("the destinations failure keeps first-error priority", func(t *testing.T) {
		dest := "/out/W34-SEAM2/poster.jpg"
		ops := []models.BatchFileOperation{{GeneratedFiles: covW2DJournal(t, dest, dest+".dlbak."+p3HexA, 1)}}
		destsErr := errors.New("w34 dests wedged")
		rootsErr := errors.New("w34 roots wedged")
		prevD, prevR := reverterSweepDestinations, reverterSweepRoots
		reverterSweepDestinations = func(context.Context, *ReplacementSweeper, []string) (int, error) { return 0, destsErr }
		reverterSweepRoots = func(context.Context, *ReplacementSweeper, []string) (int, error) { return 0, rootsErr }
		t.Cleanup(func() { reverterSweepDestinations, reverterSweepRoots = prevD, prevR })

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		defer restoreLog()

		r := &Reverter{sweeper: &ReplacementSweeper{}}
		r.sweepJournaledDestinations(ctx, ops)
		require.Contains(t, logs.String(), "pre-revert replacement sweep failed: "+destsErr.Error()+" (continuing with revert)")
		require.NotContains(t, logs.String(), rootsErr.Error(), "the second leg's error never displaces the first")
	})
}
