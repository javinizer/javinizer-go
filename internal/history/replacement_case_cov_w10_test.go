package history

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func w10SetCaseProbe(t *testing.T, sensitive bool) {
	t.Helper()
	previous := fsutil.CaseSensitiveProbe
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return sensitive, nil }
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = previous
		fsutil.ResetCaseSensitivityCache()
	})
}

func w10CaseJournal(t *testing.T, fs afero.Fs, repo *p3OpRepo) (*models.BatchFileOperation, string, string) {
	t.Helper()
	dir := "/out/W10-CASE"
	upper := dir + "/Poster.jpg"
	lower := dir + "/poster.jpg"
	upperBackup := upper + ".dlbak.1111111111111111"
	lowerBackup := lower + ".dlbak.2222222222222222"
	require.NoError(t, fs.MkdirAll(dir, config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, upper, []byte("new-upper"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, lower, []byte("new-lower"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, upperBackup, []byte("old-upper"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, lowerBackup, []byte("old-lower"), config.FilePerm))
	op := &models.BatchFileOperation{
		BatchJobID:    "job-w10",
		MovieID:       "W10-CASE",
		OriginalPath:  "/src/W10-CASE.mkv",
		NewPath:       dir + "/W10-CASE.mkv",
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{
				{Destination: upper, Backup: upperBackup, DestSeq: 1},
				{Destination: lower, Backup: lowerBackup, DestSeq: 1},
			},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op, upper, lower
}

func TestRestoreReplacementJournal_CaseSensitiveKeepsCrossCaseChainsSeparate(t *testing.T) {
	w10SetCaseProbe(t, true)
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, upper, lower := w10CaseJournal(t, fs, repo)

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.Len(t, restored, 2, "case-sensitive destination spellings must form separate groups")
	require.Equal(t, "old-upper", p3ReadFile(t, fs, upper))
	require.Equal(t, "old-lower", p3ReadFile(t, fs, lower))
	require.NotEqual(t, sweepSlash(upper), sweepSlash(lower))

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "each cross-case entry must be consumed independently")
	_, err = fs.Stat(upper + ".dlbak.1111111111111111")
	require.ErrorIs(t, err, afero.ErrFileNotFound)
	_, err = fs.Stat(lower + ".dlbak.2222222222222222")
	require.ErrorIs(t, err, afero.ErrFileNotFound)
}

func TestRestoreReplacementJournal_CaseInsensitivePreservesFoldedLegacyGrouping(t *testing.T) {
	w10SetCaseProbe(t, false)
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, upper, lower := w10CaseJournal(t, fs, repo)

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.Len(t, restored, 1, "insensitive roots retain the legacy folded bucket")
	require.Equal(t, "old-lower", p3ReadFile(t, fs, upper), "legacy first-spelling target receives the last replay")
	require.Equal(t, "new-lower", p3ReadFile(t, fs, lower), "legacy folded grouping does not create a second target")
	require.Equal(t, sweepSlash(upper), sweepSlash(lower))
}

func TestReplacementSweep_CaseSensitiveKeepsCrossCaseBackupsDistinct(t *testing.T) {
	w10SetCaseProbe(t, true)
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, upper, lower := w10CaseJournal(t, fs, repo)
	require.NoError(t, fs.Remove(upper))
	require.NoError(t, fs.Remove(lower))
	backdate(t, fs, upper+".dlbak.1111111111111111")
	backdate(t, fs, lower+".dlbak.2222222222222222")

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, healed, "case-sensitive sweep keys must arbitrate both backups")
	require.Equal(t, "old-upper", p3ReadFile(t, fs, upper))
	require.Equal(t, "old-lower", p3ReadFile(t, fs, lower))
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

// Codex P2 (keyed-lock probe posture): a probe ERROR leaves the case decision
// undecidable. Folding on that guess could alias byte-distinct files on a
// case-sensitive volume, so the conservative fallback PRESERVES case
// distinctions; only a positive insensitivity determination unlocks folding.
func TestProbeFailurePreservesCaseKeyedMatching(t *testing.T) {
	previous := fsutil.CaseSensitiveProbe
	probeErr := errors.New("probe unavailable")
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return true, probeErr }
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = previous
		fsutil.ResetCaseSensitivityCache()
	})

	root := t.TempDir()
	require.True(t, fsutil.IsCaseSensitiveRoot(root), "an undecidable probe must not fold case")
	require.NotEqual(t,
		fsutil.DestKeyForRoot(root, root+"/Poster.jpg"),
		fsutil.DestKeyForRoot(root, root+"/poster.jpg"),
		"probe failure keeps distinct keys; folded matching needs a positive determination")
}
