//go:build windows

package fsutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// The Windows close-order contract of the verified copy composite: every
// delete-shared source handle the leg pins — on the publish path AND the
// admission-refusal path — is closed before the composite returns, proven by
// an exclusive (share-none) reopen of the source entry, which a stale pin
// denies outright. Scanner/indexer transients are absorbed by the bounded
// retry; a leaked composite handle persists for the process lifetime and
// surfaces here deterministically.
func TestCopyFileNoReplaceVerifiedSourceHandleClosedOnReturn(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))

	proof, _ := verifyProofOf(t, fs, src)
	require.NoError(t, CopyFileNoReplaceVerified(fs, src, dst, proof))
	requireExclusiveReopen(t, src)

	dstRefused := filepath.Join(root, "lib2", "movie.mp4")
	refusing := VerifiedSourceProof(func(string, os.FileInfo) error {
		return errors.New("admission denied")
	})
	err := CopyFileNoReplaceVerified(fs, src, dstRefused, refusing)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	requireExclusiveReopen(t, src)
}

func TestOpenVerifiedSourcePinSharesDelete(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "movie.mp4")
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))

	pinned, err := openVerifiedSource(fs, src)
	require.NoError(t, err)
	aside := src + ".aside"
	require.NoError(t, fs.Rename(src, aside), "the verified-source pin must share delete so the name may move mid-open")
	got, err := io.ReadAll(pinned)
	require.NoError(t, err)
	require.Equal(t, "admitted video bytes", string(got), "the descriptor still addresses the admitted object after the name moved")
	require.NoError(t, pinned.Close())
	require.NoError(t, fs.Rename(aside, src))
}

func requireExclusiveReopen(t *testing.T, path string) {
	t.Helper()
	p, perr := windows.UTF16PtrFromString(path)
	require.NoError(t, perr)
	var err error
	deadline := time.Now().Add(10 * time.Second)
	for {
		var handle windows.Handle
		handle, err = windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			require.NoError(t, windows.CloseHandle(handle))
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	require.NoError(t, err, "exclusive reopen refused — a verified-source handle leaked past the composite return")
}
