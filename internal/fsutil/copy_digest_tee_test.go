package fsutil

import (
	"bytes"
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

// midStreamMutator replays the codex P2 hazard (PRRT_kwDORn9KaM6nkVjY): a
// concurrent writer rewrites the admitted object IN PLACE — same file object,
// new bytes — once the stream has passed quota bytes. The composite's pinned
// descriptor keeps reading the LIVE object, so without the post-stream
// re-proof the staged copy would publish (and the digest lane would
// digest-certify) content the admission proof never saw.
type midStreamMutator struct {
	fs      afero.Fs
	body    []byte
	quota   int
	mutated bool
}

func (m *midStreamMutator) open(name string) (afero.File, error) {
	fh, err := m.fs.Open(name)
	if err != nil {
		return nil, err
	}
	return &midStreamMutatingFile{File: fh, mut: m, name: name}, nil
}

type midStreamMutatingFile struct {
	afero.File
	mut  *midStreamMutator
	name string
	seen int
}

func (f *midStreamMutatingFile) Read(p []byte) (int, error) {
	if !f.mut.mutated && f.seen >= f.mut.quota {
		f.mut.mutated = true
		if err := afero.WriteFile(f.mut.fs, f.name, f.mut.body, 0o644); err != nil {
			return 0, err
		}
	}
	n, err := f.File.Read(p)
	f.seen += n
	return n, err
}

// The P2 replay on both verified copy lanes: the admitted object is
// rewritten in place while its bytes stream into staging. The publication
// must refuse typed (the stale staged copy discarded), stamp no digest, and
// leave the destination unwritten — on a virtual and a real filesystem.
func TestCopyFileNoReplaceVerified_MidStreamMutationRefusesPublication(t *testing.T) {
	admitted := []byte("admitted pre-copy source state — the NFO and metadata describe THESE bytes")
	mutant := bytes.Repeat([]byte("MID-STREAM IN-PLACE REWRITE "), 256)
	lanes := []struct {
		name string
		run  func(fs afero.Fs, src, dst string, proof VerifiedSourceProof) (string, error)
	}{
		{"digest lane", func(fs afero.Fs, src, dst string, proof VerifiedSourceProof) (string, error) {
			return CopyFileNoReplaceVerifiedDigest(fs, src, dst, proof)
		}},
		{"copy lane", func(fs afero.Fs, src, dst string, proof VerifiedSourceProof) (string, error) {
			return "", CopyFileNoReplaceVerified(fs, src, dst, proof)
		}},
	}
	for _, lane := range lanes {
		for _, fsKind := range []string{"memfs", "osfs"} {
			t.Run(lane.name+"/"+fsKind, func(t *testing.T) {
				var base afero.Fs
				var src, dst, root string
				if fsKind == "osfs" {
					base = afero.NewOsFs()
					root = t.TempDir()
					src = filepath.Join(root, "src.mkv")
					dst = filepath.Join(root, "out", "dst.mkv")
					require.NoError(t, os.WriteFile(src, admitted, 0o644))
				} else {
					base = afero.NewMemMapFs()
					src = filepath.FromSlash("/in/src.mkv")
					dst = filepath.FromSlash("/out/dst.mkv")
					require.NoError(t, base.MkdirAll(filepath.Dir(src), 0o755))
					require.NoError(t, afero.WriteFile(base, src, admitted, 0o644))
					root = "/"
				}
				proof, _ := verifyProofOf(t, base, src)
				mut := &midStreamMutator{fs: base, body: mutant, quota: 4}

				digest, err := lane.run(&verifyHookFS{Fs: base, open: mut.open}, src, dst, proof)
				require.ErrorIs(t, err, ErrTakeAsideForeign, "the post-stream re-proof refuses the mid-stream-mutated object")
				assert.Empty(t, digest, "no digest may certify the post-admission bytes")
				assert.True(t, mut.mutated, "the replay really landed mid-stream")
				_, statErr := base.Stat(dst)
				assert.True(t, os.IsNotExist(statErr), "the refused publication writes no destination")
				got, rerr := afero.ReadFile(base, src)
				require.NoError(t, rerr)
				assert.Equal(t, mutant, got, "the source keeps its mid-stream bytes untouched by the refusal")
				assertNoBoundResidue(t, base, root)
			})
		}
	}
}

// reproofStatFailFile answers the admission handle Stat cleanly, then wedges
// the RE-INSPECTION the post-stream re-proof runs (the closure built by
// reproofStreamedSource) — an NFS/SMB-style handle invalidated mid-stream —
// so the lane's re-inspect guard fires rather than the drift classifier.
type reproofStatFailFile struct {
	afero.File
	calls int
	err   error
}

func (f *reproofStatFailFile) Stat() (os.FileInfo, error) {
	f.calls++
	if f.calls > 1 {
		return nil, f.err
	}
	return f.File.Stat()
}

// reproofStatFailFS hands each opened source one reproofStatFailFile and keeps
// it, so the test can pin the exact admission/re-inspection Stat call count.
type reproofStatFailFS struct {
	afero.Fs
	err  error
	last *reproofStatFailFile
}

func (f *reproofStatFailFS) Open(name string) (afero.File, error) {
	fh, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	f.last = &reproofStatFailFile{File: fh, err: f.err}
	return f.last, nil
}

// The re-proof's re-inspection fault on both verified copy lanes: the
// admission Stat passes, the staged stream lands every admitted byte, and only
// the post-stream handle re-inspection wedges. The refusal must carry the
// re-inspect class (never the ErrTakeAsideForeign drift verdict), discard the
// staged copy, and attribute no digest; the destination is never written.
func TestCopyFileNoReplaceVerified_ReproofStatFaultRefusesPublication(t *testing.T) {
	sentinel := errors.New("re-inspect wedged")
	lanes := []struct {
		name string
		run  func(fs afero.Fs, src, dst string, proof VerifiedSourceProof) (string, error)
	}{
		{"digest lane", func(fs afero.Fs, src, dst string, proof VerifiedSourceProof) (string, error) {
			return CopyFileNoReplaceVerifiedDigest(fs, src, dst, proof)
		}},
		{"copy lane", func(fs afero.Fs, src, dst string, proof VerifiedSourceProof) (string, error) {
			return "", CopyFileNoReplaceVerified(fs, src, dst, proof)
		}},
	}
	for _, lane := range lanes {
		t.Run(lane.name, func(t *testing.T) {
			base := afero.NewOsFs()
			root := t.TempDir()
			src := filepath.Join(root, "src.mkv")
			dst := filepath.Join(root, "out", "dst.mkv")
			require.NoError(t, os.WriteFile(src, []byte("payload for the re-inspect fault leg"), 0o644))
			admit := func(string, os.FileInfo) error { return nil }
			fault := &reproofStatFailFS{Fs: base, err: sentinel}

			digest, err := lane.run(fault, src, dst, admit)
			require.ErrorIs(t, err, sentinel)
			assert.ErrorContains(t, err, "re-inspect the streamed")
			assert.NotErrorIs(t, err, ErrTakeAsideForeign, "an indeterminate re-inspection is not a foreign-object drift verdict")
			assert.Empty(t, digest, "no digest may be attributed to bytes the re-proof never re-inspected")
			require.NotNil(t, fault.last)
			assert.Equal(t, 2, fault.last.calls, "exactly the admission Stat and the post-stream re-inspection touched the source handle")
			_, statErr := os.Stat(dst)
			assert.True(t, os.IsNotExist(statErr), "the refused publication writes no destination")
			assertNoBoundResidue(t, base, root)
		})
	}
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
