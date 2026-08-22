package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w63ReadFailFile fails Read (models a read mid-stream I/O fault).
type w63ReadFailFile struct{ afero.File }

func (w63ReadFailFile) Read([]byte) (int, error) { return 0, errors.New("w63 read wedge") }

// w63ReadFailFs hands out a Read-failing descriptor for the victim path.
type w63ReadFailFs struct{ afero.Fs }

func (f w63ReadFailFs) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	base, err := f.Fs.OpenFile(name, flags, perm)
	if err != nil {
		return nil, err
	}
	return w63ReadFailFile{File: base}, nil
}

func w63ShaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Wave-63 (codex P2): capture stamps the sha256 of the set-aside bytes, and
// restores the pre-read mtime the downstream rollback/re-arm hands off.
func TestW63_CaptureReplacementBackupFacts_SHA256(t *testing.T) {
	fs := afero.NewMemMapFs()
	payload := []byte("the original poster bytes")
	require.NoError(t, afero.WriteFile(fs, "/w63/backup", payload, 0o644))

	facts, err := captureReplacementBackupFacts(fs, "/w63/backup")
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), facts.Size)
	require.Equal(t, w63ShaHex(payload), facts.SHA256, "the sha256 of the set-aside bytes is stamped")
}

// A no-follow open failure fails the capture closed — no forgeable entry.
func TestW63_CaptureReplacementBackupFacts_OpenFail(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/w63/backup", []byte("payload"), 0o644))
	fs := w48OpenFileFailFs{Fs: base, err: errors.New("w63 open wedge")}

	_, err := captureReplacementBackupFacts(fs, "/w63/backup")
	require.Error(t, err, "an open failure fails the capture closed")
}

// A read mid-stream failure fails the capture closed.
func TestW63_CaptureReplacementBackupFacts_ReadFail(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/w63/backup", []byte("payload"), 0o644))
	fs := w63ReadFailFs{Fs: base}

	_, err := captureReplacementBackupFacts(fs, "/w63/backup")
	require.Error(t, err, "a read failure fails the capture closed")
}
