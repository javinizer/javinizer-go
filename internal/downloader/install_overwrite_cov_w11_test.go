package downloader

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCopyBackupToDest_PreservesBackupModTimeW11(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W11-DOWNLOADER/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/W11-DOWNLOADER", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("restored"), 0o600))

	expected := time.Unix(946684800, 0)
	require.NoError(t, fs.Chtimes(backup, expected, expected))
	require.NoError(t, copyBackupToDest(fs, backup, dest))

	info, err := fs.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, expected.Unix(), info.ModTime().Unix())
}

func TestCopyBackupToDest_ChtimesFailureW11(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W11-DOWNLOADER-FAIL/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, base.MkdirAll("/out/W11-DOWNLOADER-FAIL", 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("restored"), 0o600))

	failure := errors.New("downloader chtimes wedged")
	fs := covW11DownloaderChtimesFailFs{Fs: base, err: failure}
	err := copyBackupToDest(fs, backup, dest)
	require.ErrorIs(t, err, failure)
	require.Contains(t, err.Error(), "stage rollback times")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)))

	entries, readErr := afero.ReadDir(base, "/out/W11-DOWNLOADER-FAIL")
	require.NoError(t, readErr)
	// Wave-26 (codex P2, PR#215): the staged name stays because with the
	// handle closed we can no longer prove it's ours; the artifact's residue
	// is inert (ordinal-salted) and a later cleanup never unlink-chases a
	// possibly-foreign occupant.
	residue := 0
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".dlrstr.") {
			residue++
		}
	}
	require.Equal(t, 1, residue, "the unproven staged name is retained (never a pathname Remove of an occupant that may now be foreign)")
}

type covW11DownloaderChtimesFailFs struct {
	afero.Fs
	err error
}

func (f covW11DownloaderChtimesFailFs) Chtimes(name string, atime, mtime time.Time) error {
	if strings.Contains(name, ".dlrstr.") {
		return f.err
	}
	return f.Fs.Chtimes(name, atime, mtime)
}
