package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sha256HexW(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// The tee certifies the destination's bytes off the copy's single read:
// digest == sha256(dst) == sha256(src), established without touching the
// payload a second time in production (the test re-reads only to assert).
func TestCopyFileNoReplaceDigest_TeeCertifiesDestination(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "out", "dst.mkv")
	content := make([]byte, 300000)
	for i := range content {
		content[i] = byte(i * 31)
	}
	require.NoError(t, os.WriteFile(src, content, 0o644))

	digest, err := CopyFileNoReplaceDigest(base, src, dst)
	require.NoError(t, err)
	assert.Equal(t, sha256HexW(content), digest)
	landed, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, sha256HexW(landed), digest, "the returned digest certifies exactly the published bytes")

	t.Run("occupied destination refuses with an empty digest", func(t *testing.T) {
		again, err := CopyFileNoReplaceDigest(base, src, dst)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrPublishCollision)
		assert.Empty(t, again)
	})

	t.Run("missing source carries no digest", func(t *testing.T) {
		again, err := CopyFileNoReplaceDigest(base, filepath.Join(root, "absent.mkv"), filepath.Join(root, "x"))
		require.Error(t, err)
		assert.Empty(t, again)
	})
}

func TestCopyFileNoReplaceVerifiedDigest_AdmittedBytesCertified(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "out", "dst.mkv")
	content := []byte("admitted video payload beyond any doubt")
	require.NoError(t, os.WriteFile(src, content, 0o644))
	admit := func(string, os.FileInfo) error { return nil }

	digest, err := CopyFileNoReplaceVerifiedDigest(base, src, dst, admit)
	require.NoError(t, err)
	assert.Equal(t, sha256HexW(content), digest)
	assert.Equal(t, content, func() []byte { b, _ := os.ReadFile(dst); return b }())

	t.Run("a refusing proof publishes nothing and no digest", func(t *testing.T) {
		sentinel := errors.New("not the admitted object")
		again, err := CopyFileNoReplaceVerifiedDigest(base, src, filepath.Join(root, "out2", "dst.mkv"), func(string, os.FileInfo) error { return sentinel })
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		assert.Empty(t, again)
		_, statErr := os.Stat(filepath.Join(root, "out2", "dst.mkv"))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("occupied destination refuses with an empty digest", func(t *testing.T) {
		again, err := CopyFileNoReplaceVerifiedDigest(base, src, dst, admit)
		require.ErrorIs(t, err, ErrPublishCollision)
		assert.Empty(t, again)
	})

	t.Run("nil proof degrades to the by-name digest twin", func(t *testing.T) {
		dst2 := filepath.Join(root, "out3", "dst.mkv")
		again, err := CopyFileNoReplaceVerifiedDigest(base, src, dst2, nil)
		require.NoError(t, err)
		assert.Equal(t, sha256HexW(content), again)
	})
}

// The digest-twins' classify shortcut: a self-copy classify must not stamp a
// digest for bytes that never installed.
func TestCopyFileNoReplaceDigest_ClassifyDoneCarriesNoDigest(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	require.NoError(t, os.WriteFile(src, []byte("self"), 0o644))
	digest, err := CopyFileNoReplaceDigest(base, src, src)
	require.NoError(t, err, "lexical self-copy classifies done without error")
	assert.Empty(t, digest, "no publish happened — no digest may be attributed")
	assert.Empty(t, func() string {
		d, _ := CopyFileNoReplaceVerifiedDigest(base, src, src, func(string, os.FileInfo) error { return nil })
		return d
	}())
}

// --- Fault legs of the digest composites: every failure carries NO digest,
// so a seal can never be attributed to bytes that did not land. ---

type mkdirAllFailFS struct {
	afero.Fs
	err error
}

func (f *mkdirAllFailFS) MkdirAll(string, os.FileMode) error { return f.err }

type openVerifiedStatFailFS struct {
	afero.Fs
	err error
}

func (f *openVerifiedStatFailFS) Open(name string) (afero.File, error) {
	fh, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	return &partialDigestFaultFile{File: fh, statErr: f.err}, nil
}

// midStreamFailFS wedges the source's Read after quota bytes, replaying an
// SMB drop mid-stream: the copy's staged-discard discipline must answer with
// no destination and no digest.
type midStreamFailFS struct {
	afero.Fs
	quota int
	err   error
}

type midStreamFailFile struct {
	afero.File
	seen  int
	quota int
	err   error
}

func (f *midStreamFailFile) Read(p []byte) (int, error) {
	if f.seen >= f.quota {
		return 0, f.err
	}
	n, err := f.File.Read(p)
	f.seen += n
	return n, err
}

func (f *midStreamFailFS) Open(name string) (afero.File, error) {
	fh, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	return &midStreamFailFile{File: fh, quota: f.quota, err: f.err}, nil
}

func TestCopyFileNoReplaceDigest_FaultLegs(t *testing.T) {
	sentinel := errors.New("wedged")
	seed := func(t *testing.T) (afero.Fs, string, string) {
		root := t.TempDir()
		fs := afero.NewOsFs()
		src := filepath.Join(root, "src.mkv")
		dst := filepath.Join(root, "out", "dst.mkv")
		require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
		require.NoError(t, os.WriteFile(src, []byte("payload for the copy fault legs — long enough to stream past any one buffer"), 0o644))
		return fs, src, dst
	}

	t.Run("destination directory failure carries no digest", func(t *testing.T) {
		fs, src, dst := seed(t)
		digest, err := CopyFileNoReplaceDigest(&mkdirAllFailFS{Fs: fs, err: sentinel}, src, dst)
		require.ErrorIs(t, err, sentinel)
		assert.Empty(t, digest)
		_, statErr := os.Stat(dst)
		assert.True(t, os.IsNotExist(statErr), "the refused install writes no destination")
		_, dirErr := os.Stat(filepath.Dir(dst))
		assert.True(t, os.IsNotExist(dirErr), "the wedged MkdirAll never created the destination directory")
	})

	t.Run("mid-stream source failure publishes nothing and no digest", func(t *testing.T) {
		fs, src, dst := seed(t)
		digest, err := CopyFileNoReplaceDigest(&midStreamFailFS{Fs: fs, quota: 8, err: sentinel}, src, dst)
		require.ErrorIs(t, err, sentinel)
		assert.Empty(t, digest)
		_, statErr := os.Stat(dst)
		assert.True(t, os.IsNotExist(statErr), "the staged discard discipline leaves no partial destination")
	})
}

func TestCopyFileNoReplaceVerifiedDigest_FaultLegs(t *testing.T) {
	sentinel := errors.New("wedged")
	seed := func(t *testing.T) (afero.Fs, string, string) {
		root := t.TempDir()
		fs := afero.NewOsFs()
		src := filepath.Join(root, "src.mkv")
		dst := filepath.Join(root, "out", "dst.mkv")
		require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
		require.NoError(t, os.WriteFile(src, []byte("payload for the verified fault legs"), 0o644))
		return fs, src, dst
	}
	admit := func(string, os.FileInfo) error { return nil }

	t.Run("destination directory failure carries no digest", func(t *testing.T) {
		fs, src, dst := seed(t)
		digest, err := CopyFileNoReplaceVerifiedDigest(&mkdirAllFailFS{Fs: fs, err: sentinel}, src, dst, admit)
		require.ErrorIs(t, err, sentinel)
		assert.Empty(t, digest)
	})

	t.Run("missing source opens no stream and no digest", func(t *testing.T) {
		fs, _, dst := seed(t)
		digest, err := CopyFileNoReplaceVerifiedDigest(fs, filepath.Join(filepath.Dir(dst), "absent.mkv"), dst, admit)
		require.Error(t, err)
		assert.Empty(t, digest)
	})

	t.Run("handle stat failure refuses before a byte flows", func(t *testing.T) {
		fs, src, dst := seed(t)
		digest, err := CopyFileNoReplaceVerifiedDigest(&openVerifiedStatFailFS{Fs: fs, err: sentinel}, src, dst, admit)
		require.ErrorIs(t, err, sentinel)
		assert.Empty(t, digest)
		_, statErr := os.Stat(dst)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("mid-stream failure inside the verified lane too", func(t *testing.T) {
		fs, src, dst := seed(t)
		digest, err := CopyFileNoReplaceVerifiedDigest(&midStreamFailFS{Fs: fs, quota: 8, err: sentinel}, src, dst, admit)
		require.ErrorIs(t, err, sentinel)
		assert.Empty(t, digest)
		_, statErr := os.Stat(dst)
		assert.True(t, os.IsNotExist(statErr), "no partial publish, no partial digest")
	})
}

type openFailForSrcFS struct {
	afero.Fs
	src string
	err error
}

func (f *openFailForSrcFS) Open(name string) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.src) {
		return nil, f.err
	}
	return f.Fs.Open(name)
}

func TestCopyFileNoReplaceVerifiedDigest_SourceOpenRefuseCarriesNoDigest(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "out", "dst.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o644))
	sentinel := errors.New("source open wedged")

	digest, err := CopyFileNoReplaceVerifiedDigest(&openFailForSrcFS{Fs: fs, src: src, err: sentinel}, src, dst, func(string, os.FileInfo) error { return nil })
	require.ErrorIs(t, err, sentinel)
	assert.Empty(t, digest)
	_, statErr := os.Stat(dst)
	assert.True(t, os.IsNotExist(statErr), "no open, no stream, no destination")
}

// The no-clobber preflight refuses the destination BEFORE a byte flows. The
// probe only runs against a real *afero.OsFs (wrapper filesystems bypass the
// capability check entirely), so the digest route's probe-refusal arm is
// wedged at the probe's own publish seam — the same discipline the probe's
// unix tests use (stubNoClobberProbe). A synthetic publish denial surfaces
// through ProbeNoClobberPublish, the copy returns no digest, and the
// destination directory is left exactly as the probe found it (the probe's
// synthetic pair is reaped by its own bound cleanup).
func TestCopyFileNoReplaceDigest_NoClobberProbeRefuseCarriesNoDigest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the no-clobber preflight is bypassed on windows")
	}
	sentinel := errors.New("synthetic no-clobber publish denial")
	root := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "out", "dst.mkv")
	require.NoError(t, os.WriteFile(src, []byte("payload denied by the no-clobber preflight"), 0o644))

	prev := publishNoClobberProbe
	publishNoClobberProbe = func(afero.Fs, string, string) error { return sentinel }
	t.Cleanup(func() { publishNoClobberProbe = prev })

	digest, err := CopyFileNoReplaceDigest(fs, src, dst)
	require.ErrorIs(t, err, sentinel, "the preflight verdict surfaces through the digest route")
	assert.ErrorContains(t, err, "no-clobber preflight")
	assert.Empty(t, digest)
	_, statErr := os.Stat(dst)
	assert.True(t, os.IsNotExist(statErr), "a refused preflight installs no destination")
	entries, readErr := os.ReadDir(filepath.Dir(dst))
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no staging or probe residue survives the refusal")
}

// The source-open arm needs the source to EXIST — classifyNoreplaceDestination
// rejects an absent source before any open — so the wrapper leaves the
// classified source on disk and refuses Open on exactly it: the copy aborts
// after the preflight with no staged stream, no digest, and no destination.
func TestCopyFileNoReplaceDigest_SourceOpenRefuseCarriesNoDigest(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "out", "dst.mkv")
	require.NoError(t, os.WriteFile(src, []byte("payload whose open is refused"), 0o644))
	sentinel := errors.New("source open wedged")

	digest, err := CopyFileNoReplaceDigest(&openFailForSrcFS{Fs: fs, src: src, err: sentinel}, src, dst)
	require.ErrorIs(t, err, sentinel)
	assert.ErrorContains(t, err, "no-replace copy: open source")
	assert.Empty(t, digest)
	_, statErr := os.Stat(dst)
	assert.True(t, os.IsNotExist(statErr), "no open, no stream, no destination")
}
