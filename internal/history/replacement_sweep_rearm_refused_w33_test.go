package history

// POSTER-WRITE-HARDENING wave-33 (codex local review round 3, PR#215 finding
// R1): the wave-29 ledger-only rearm-refused consumption used to remove the
// entry from the DB but leave its mapping in the in-memory idx.journaled
// built at sweep start. A marker-shaped file landed at a JUST-CONSUMED name
// before the directory scan then routed through the STALE owner copy into the
// pending-retry's consumed-entry removal leg — quarantined/deleted as if
// owned, where the conservative orphan rule retains + warns. idx.journaled is
// now the sweep-local MUTABLE view of live ledger state (consumption legs
// retract as they commit), and restoreAndConsume re-verifies the live ledger
// whenever a cleanup's only authorization is the index-time row copy.

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w33TwoRefusedRow seeds ONE applied operation row journaling TWO
// rearm-refused restore-pending replacement entries in the same directory;
// neither backup file exists (the refused re-arms left both names absent).
func w33TwoRefusedRow(t *testing.T, repo *p3OpRepo, movieID, dest1, backup1, dest2, backup2 string) *models.BatchFileOperation {
	t.Helper()
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
			{Destination: dest1, Backup: backup1, DestSeq: 1,
				RestorePending: true, RestorePendingKind: models.RestorePendingKindRearmRefused},
			{Destination: dest2, Backup: backup2, DestSeq: 2,
				RestorePending: true, RestorePendingKind: models.RestorePendingKindRearmRefused},
		}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op
}

// w33PlantOnScanFs plants a marker file into scanDir the first time the sweep
// OPENS that directory for enumeration — the deterministic replay of "a
// marker PLANTED in a journaled dir before the scan" but AFTER the ledger
// leg's absent-proof observed the name as missing.
type w33PlantOnScanFs struct {
	afero.Fs
	scanDir  string
	plant    string
	contents []byte
	done     bool
}

func (f *w33PlantOnScanFs) Open(name string) (afero.File, error) {
	// Compare through the sweep's own probe-aware destination key: the
	// sweeper may open the directory under a different separator spelling
	// than the fixture used (Windows journal spellings/deriving normalize
	// through sweepSlash), and a literal byte comparison silently never
	// fired the plant on the Windows runner (CI CI-4).
	if !f.done && sweepSlash(name) == sweepSlash(f.scanDir) {
		f.done = true
		if err := afero.WriteFile(f.Fs, f.plant, f.contents, 0o644); err != nil {
			return nil, err
		}
	}
	return f.Fs.Open(name)
}

// The core fix: the ledger leg consumes BOTH rearm-refused pendings
// journal-only (backup names absent at the absent-proof — the plant lands
// only when the directory scan opens the dir), and the planted marker at the
// just-consumed name is then ORPHAN-retained byte-intact — the stale idx
// owner routing (quarantine-delete) must be gone.
func TestSweepW33_LedgerConsumptionRetractsStaleIndexRouting(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W33-STALEROUTE"
	dest1 := dir + "/cover.jpg"
	dest2 := dir + "/poster.jpg"
	backup1 := dest1 + ".dlbak." + p3HexA
	backup2 := dest2 + ".dlbak." + p3HexB
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, dest1, "restored-cover", 0)
	writeSweepFile(t, base, dest2, "restored-poster", 0)
	op := w33TwoRefusedRow(t, repo, "W33-STALEROUTE", dest1, backup1, dest2, backup2)

	plantBytes := []byte("W33-PLANTED-FOREIGN-BYTES")
	fs := &w33PlantOnScanFs{Fs: base, scanDir: dir, plant: backup2, contents: plantBytes}

	var logs bytes.Buffer
	restore := logging.SetOutput(&logs)
	defer restore()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, healed, "both rearm-refused pendings consume journal-only; the planted marker heals nothing")
	require.True(t, fs.done, "the scan opened the directory (the plant landed before enumeration)")

	require.Equal(t, plantBytes, mustRead2(t, base, backup2),
		"the planted marker is ORPHAN-retained byte-intact — never quarantine-deleted through the stale owner")
	require.Equal(t, "restored-cover", string(mustRead2(t, base, dest1)))
	require.Equal(t, "restored-poster", string(mustRead2(t, base, dest2)))
	require.Empty(t, w29RowEntry(t, repo, op.ID), "both journal entries consumed")
	require.Contains(t, logs.String(), "no journal entry proves ownership",
		"the scan arbitrated the planted marker through the orphan retain+warn leg")
}

// The sweepOne belt: when the only cleanup authorization is the INDEX-TIME
// row copy, restoreAndConsume re-reads the live ledger — an entry consumed
// since the snapshot (a concurrent or earlier same-sweep consumption) flips
// the journaled routing to the ORPHAN posture: retain + warn, never remove.
func TestSweepW33_StalePendingOwnerConsumedEntryOrphanRetains(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W33-BELTCONSUMED"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	writeSweepFile(t, fs, backup, "planted-foreign", 0)

	// Journal the entry restore-pending, snapshot the row, then consume the
	// entry from the live ledger — the snapshot alone still authorizes.
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "W33-BELTCONSUMED", OriginalPath: "/src/w33-beltconsumed.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1, RestorePending: true,
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))
	require.NoError(t, repo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return consumeSweepJournalEntry(current, sweepSlash(backup))
	}))
	require.Empty(t, w29RowEntry(t, repo, op.ID), "the live ledger no longer journals the backup")

	var logs bytes.Buffer
	restore := logging.SetOutput(&logs)
	defer restore()

	got := NewReplacementSweeper(fs, repo).restoreAndConsume(ctx, op, backup, dest, sweepSlash(backup), nil)
	require.False(t, got, "stale-snapshot authorization alone never cleans up")
	require.Equal(t, "planted-foreign", string(mustRead2(t, fs, backup)), "the occupant survives untouched")
	require.Equal(t, "restored", string(mustRead2(t, fs, dest)))
	require.Contains(t, logs.String(), "no journal entry proves ownership")
}

// The belt's owner-row read failing keeps everything untouched and warns —
// an unreadable ownership answer is never license to remove.
func TestSweepW33_StalePendingOwnerRowUnreadableRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo() // empty: the snapshot's owner row 77 is unreadable
	ctx := context.Background()
	dir := "/out/W33-BELTGONE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	writeSweepFile(t, fs, backup, "planted-foreign", 0)
	stale := &models.BatchFileOperation{
		ID: 77, RevertStatus: models.RevertStatusApplied,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1, RestorePending: true,
		}}}),
	}

	var logs bytes.Buffer
	restore := logging.SetOutput(&logs)
	defer restore()

	got := NewReplacementSweeper(fs, repo).restoreAndConsume(ctx, stale, backup, dest, sweepSlash(backup), nil)
	require.False(t, got)
	require.Contains(t, logs.String(), "owner row unreadable before cleanup authorization")
	require.Equal(t, "planted-foreign", string(mustRead2(t, fs, backup)))
	require.Equal(t, "restored", string(mustRead2(t, fs, dest)))
}

// This process's own in-process pending memory IS live evidence (the removal
// failure happened here): the belt is skipped and the retry removes the owned
// backup and consumes the entry.
func TestSweepW33_InProcessMemorySkipsTheFreshReverify(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W33-BELTMEMORY"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	writeSweepFile(t, fs, backup, "owned-backup", 0)
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "W33-BELTMEMORY", OriginalPath: "/src/w33-beltmemory.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1, RestorePending: true,
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	sweeper := NewReplacementSweeper(fs, repo)
	backupKey := sweepSlash(backup)
	sweeper.rememberPendingRemoval(backupKey)
	require.True(t, sweeper.restoreAndConsume(ctx, op, backup, dest, backupKey, nil))

	_, serr := fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the owned backup is removed")
	require.Empty(t, w29RowEntry(t, repo, op.ID), "the entry is consumed")
	require.False(t, sweeper.hasPendingRemoval(backupKey), "the memory is forgotten")
}
