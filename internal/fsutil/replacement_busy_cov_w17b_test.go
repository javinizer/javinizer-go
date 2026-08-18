package fsutil

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestAcquireReplacementBusyW17B_PreservesOldUnownedMarker(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w17b-user/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, fs.MkdirAll("/out/w17b-user", 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte("user-owned bytes"), 0o644))
	old := time.Now().Add(-replacementBusyStaleAge - time.Minute)
	require.NoError(t, fs.Chtimes(path, old, old))

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy)
	require.Contains(t, logs.String(), path, "the refusal warning must identify the preserved path")

	info, statErr := fs.Stat(path)
	require.NoError(t, statErr)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	content, readErr := afero.ReadFile(fs, path)
	require.NoError(t, readErr)
	require.Equal(t, "user-owned bytes", string(content))
}

func TestAcquireReplacementBusyW17B_ReclaimsWellFormedDeadMarker(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w17b-dead/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/w17b-dead", 0o755))
	path := ReplacementBusyPath(dest)
	deadPID := 999999999
	created := time.Now().Add(-time.Hour)
	token := fmt.Sprintf("pid=%d,time=%d", deadPID, created.UnixNano())
	require.NoError(t, afero.WriteFile(fs, path, []byte(token), 0o600))

	release, err := AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	release()
	_, err = fs.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestParseReplacementBusyTokenW17B_ValidTokenRemainsRecognized(t *testing.T) {
	pid, created, ok := parseReplacementBusyToken("pid=" + strconv.Itoa(os.Getpid()) + ",time=1")
	require.True(t, ok)
	require.Equal(t, os.Getpid(), pid)
	require.Equal(t, int64(1), created)
}
