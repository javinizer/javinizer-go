package history

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCopyRestoreBytes_PreservesBackupModTimeW11(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W11-HISTORY/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/W11-HISTORY", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("restored"), 0o600))

	expected := time.Unix(946684800, 0)
	require.NoError(t, fs.Chtimes(backup, expected, expected))
	require.NoError(t, copyRestoreBytes(fs, backup, dest))

	info, err := fs.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, expected.Unix(), info.ModTime().Unix())
}

func TestCopyRestoreBytes_ChtimesFailureW11(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W11-HISTORY-FAIL/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, base.MkdirAll("/out/W11-HISTORY-FAIL", 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("restored"), 0o600))

	failure := errors.New("history chtimes wedged")
	fs := covW11HistoryChtimesFailFs{Fs: base, err: failure}
	err := copyRestoreBytes(fs, backup, dest)
	require.ErrorIs(t, err, failure)
	require.Contains(t, err.Error(), "stage restore times")
	require.Equal(t, "current", string(mustRead2(t, base, dest)))

	entries, readErr := afero.ReadDir(base, "/out/W11-HISTORY-FAIL")
	require.NoError(t, readErr)
	for _, entry := range entries {
		require.False(t, strings.Contains(entry.Name(), ".rstr."), "staged artifact remains: %s", entry.Name())
	}
}

type covW11HistoryChtimesFailFs struct {
	afero.Fs
	err error
}

func (f covW11HistoryChtimesFailFs) Chtimes(name string, atime, mtime time.Time) error {
	if strings.Contains(name, ".rstr.") {
		return f.err
	}
	return f.Fs.Chtimes(name, atime, mtime)
}
