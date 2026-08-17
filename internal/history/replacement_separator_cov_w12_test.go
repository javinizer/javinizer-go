package history

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestRestoreReplacementJournal_W12POSIXKeepsBackslashDestinationDistinct(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX literal-backslash semantics require a host where filepath treats backslashes as name characters")
	}
	previousPathPolicy := fsutil.PathBackslashesAreSeparators
	previousCaseProbe := fsutil.CaseSensitiveProbe
	fsutil.PathBackslashesAreSeparators = false
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return true, nil }
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() {
		fsutil.PathBackslashesAreSeparators = previousPathPolicy
		fsutil.CaseSensitiveProbe = previousCaseProbe
		fsutil.ResetCaseSensitivityCache()
	})

	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dir := "/media"
	literal := dir + `/poster\old.jpg`
	separator := dir + "/poster/old.jpg"
	literalBackup := literal + ".dlbak.1111111111111111"
	separatorBackup := separator + ".dlbak.2222222222222222"
	require.NoError(t, fs.MkdirAll(dir, config.DirPerm))
	require.NoError(t, fs.MkdirAll(filepath.Dir(separator), config.DirPerm))
	for _, path := range []string{literal, separator} {
		require.NoError(t, afero.WriteFile(fs, path, []byte("new"), config.FilePerm))
	}
	require.NoError(t, afero.WriteFile(fs, literalBackup, []byte("old-literal"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, separatorBackup, []byte("old-separator"), config.FilePerm))

	op := &models.BatchFileOperation{
		ID: 1201,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{
				{Destination: literal, Backup: literalBackup, DestSeq: 1},
				{Destination: separator, Backup: separatorBackup, DestSeq: 2},
			},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.Len(t, restored, 2, "literal and separator spellings are independent POSIX destinations")
	require.True(t, restored[literal])
	require.True(t, restored[separator])
	require.Equal(t, "old-literal", p3ReadFile(t, fs, literal))
	require.Equal(t, "old-separator", p3ReadFile(t, fs, separator))

	for _, backup := range []string{literalBackup, separatorBackup} {
		_, statErr := fs.Stat(backup)
		require.ErrorIs(t, statErr, afero.ErrFileNotFound)
	}
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "both independent journal entries must be consumed")
}
