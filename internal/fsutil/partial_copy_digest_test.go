package fsutil

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantPartialDigest recomputes the interim pin payload independently of the
// production helper so the tests pin the framing contract, not just "the
// same code answered twice".
func wantPartialDigest(t *testing.T, content []byte) string {
	t.Helper()
	span := int(CopyPartialDigestSpan)
	head := content
	if len(head) > span {
		head = head[:span]
	}
	var tail []byte
	if len(content) > span {
		tail = content[len(content)-span:]
	}
	h := sha256.New()
	var framing [8]byte
	binary.BigEndian.PutUint64(framing[:], uint64(len(content)))
	_, _ = h.Write(framing[:])
	_, _ = h.Write(head)
	_, _ = h.Write(tail)
	return hex.EncodeToString(h.Sum(nil))
}

func TestPartialCopyDigest_ShapeAndBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
	}{
		{"empty", []byte{}},
		{"small below span", []byte("video")},
		{"exactly one span", make([]byte, CopyPartialDigestSpan)},
		{"one byte over span", append(make([]byte, CopyPartialDigestSpan), 0x42)},
		{"over two spans", func() []byte {
			b := make([]byte, 2*CopyPartialDigestSpan+17)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			path := "/lib/payload.bin"
			require.NoError(t, base.MkdirAll("/lib", 0o755))
			require.NoError(t, afero.WriteFile(base, path, tc.content, 0o644))

			info, digest, err := PartialCopyDigest(base, path)
			require.NoError(t, err)
			require.NotNil(t, info, "the handle-bound identity always returns with the digest")
			assert.Equal(t, int64(len(tc.content)), info.Size())
			assert.Equal(t, wantPartialDigest(t, tc.content), digest)
		})
	}
}

func TestPartialCopyDigest_TailSensitivity(t *testing.T) {
	span := int(CopyPartialDigestSpan)
	front := make([]byte, span+64)
	back := make([]byte, span+64)
	copy(front, []byte("SAME HEAD CONTENT PADDING"))
	copy(back, []byte("SAME HEAD CONTENT PADDING"))
	front[span+63] = 'A'
	back[span+63] = 'B'
	require.Equal(t, front[:span], back[:span], "constructed same head, diverging tail")
	front[span] = 'Z'
	back[span] = 'Z'

	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/a.bin", front, 0o644))
	require.NoError(t, afero.WriteFile(base, "/b.bin", back, 0o644))
	_, da, err := PartialCopyDigest(base, "/a.bin")
	require.NoError(t, err)
	_, db, err := PartialCopyDigest(base, "/b.bin")
	require.NoError(t, err)
	assert.NotEqual(t, da, db, "same size + same head must still diverge on the tail span")
}

// partialDigestFaultFile wedges the requested handle discipline legs.
type partialDigestFaultFile struct {
	afero.File
	statErr    error
	readAtErr  error
	readAtFrom int64
	closeErr   error
}

func (f *partialDigestFaultFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return f.File.Stat()
}

func (f *partialDigestFaultFile) ReadAt(p []byte, off int64) (int, error) {
	if f.readAtErr != nil && off >= f.readAtFrom {
		return 0, f.readAtErr
	}
	return f.File.ReadAt(p, off)
}

func (f *partialDigestFaultFile) Close() error {
	if f.closeErr != nil {
		return f.closeErr
	}
	return f.File.Close()
}

type partialDigestFaultFS struct {
	afero.Fs
	openErr error
	file    *partialDigestFaultFile
}

func (f *partialDigestFaultFS) Open(name string) (afero.File, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	fh, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	if f.file == nil {
		return fh, nil
	}
	f.file.File = fh
	return f.file, nil
}

func TestPartialCopyDigest_FaultLegs(t *testing.T) {
	errSentinel := errors.New("wedged")
	seed := func(t *testing.T) (afero.Fs, string) {
		base := afero.NewMemMapFs()
		path := "/lib/payload.bin"
		require.NoError(t, base.MkdirAll("/lib", 0o755))
		require.NoError(t, afero.WriteFile(base, path, make([]byte, 2*CopyPartialDigestSpan+3), 0o644))
		return base, path
	}

	t.Run("open refused", func(t *testing.T) {
		base, path := seed(t)
		_, _, err := PartialCopyDigest(&partialDigestFaultFS{Fs: base, openErr: errSentinel}, path)
		require.ErrorContains(t, err, "open artifact for partial digest")
		require.ErrorIs(t, err, errSentinel)
	})

	t.Run("open of a missing name", func(t *testing.T) {
		base, _ := seed(t)
		_, _, err := PartialCopyDigest(base, "/lib/absent.bin")
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist, "callers classify consumed pins on the not-exist class")
	})

	t.Run("handle stat refused", func(t *testing.T) {
		base, path := seed(t)
		_, _, err := PartialCopyDigest(&partialDigestFaultFS{Fs: base, file: &partialDigestFaultFile{statErr: errSentinel}}, path)
		require.ErrorContains(t, err, "inspect artifact for partial digest")
		require.ErrorIs(t, err, errSentinel)
	})

	t.Run("non-regular entry refused", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/lib/dir", 0o755))
		_, _, err := PartialCopyDigest(base, "/lib/dir")
		require.ErrorContains(t, err, "not a regular file")
	})

	t.Run("head read refused", func(t *testing.T) {
		base, path := seed(t)
		_, _, err := PartialCopyDigest(&partialDigestFaultFS{Fs: base, file: &partialDigestFaultFile{readAtErr: errSentinel, readAtFrom: 0}}, path)
		require.ErrorContains(t, err, "head")
		require.ErrorIs(t, err, errSentinel)
	})

	t.Run("tail read refused", func(t *testing.T) {
		base, path := seed(t)
		_, _, err := PartialCopyDigest(&partialDigestFaultFS{Fs: base, file: &partialDigestFaultFile{readAtErr: errSentinel, readAtFrom: 1}}, path)
		require.ErrorContains(t, err, "tail")
		require.ErrorIs(t, err, errSentinel)
	})

	t.Run("close refused", func(t *testing.T) {
		base, path := seed(t)
		_, _, err := PartialCopyDigest(&partialDigestFaultFS{Fs: base, file: &partialDigestFaultFile{closeErr: errSentinel}}, path)
		require.ErrorContains(t, err, "close partial digest")
		require.ErrorIs(t, err, errSentinel)
	})
}

// The workflow pin and the history probe must read through the same OsFs
// route on a real volume (the deferred source and the reverted destination
// both live there).
func TestPartialCopyDigest_OsFs(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	path := filepath.Join(root, "payload.bin")
	content := make([]byte, CopyPartialDigestSpan+128)
	for i := range content {
		content[i] = byte(i * 7)
	}
	require.NoError(t, os.WriteFile(path, content, 0o644))

	info, digest, err := PartialCopyDigest(base, path)
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), info.Size())
	assert.Equal(t, wantPartialDigest(t, content), digest)
}
