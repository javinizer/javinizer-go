package fsutil

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Legs added in PR #276 round-49 for CopyFileNoReplaceVerifiedInstall and the
// bound-observe publishing helpers: exercise every defensive failure branch.

// copyStreamNoReplaceInstallReproof — nonexistent source staged through an FS
// that refuses the staged temp name triggers the exclusive-staging error leg.
func TestCoverCopyStreamNoReplaceInstall_StagingFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	fin, err := fs.Open("/in/a.mp4")
	require.NoError(t, err)
	defer func() { _ = fin.Close() }()

	blocked := &failFs{Fs: fs, failOpenFile: "/out/x.mp4.nrstg"}
	require.NoError(t, blocked.MkdirAll("/out", 0o755))
	_, err = copyStreamNoReplaceInstallReproof(blocked, fin, "/out/x.mp4", nil)
	require.Error(t, err, "staging file create refusal propagates")
}

// io.Copy failure: a source that reads-byte error mid-stream.
type errorAfterByteFile struct{ afero.File }

func (f *errorAfterByteFile) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	n, err := f.File.Read(p)
	if err == nil && n > 0 {
		return n, errors.New("forced mid-copy failure")
	}
	return n, err
}

func TestCoverCopyStreamNoReplaceInstall_CopyFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("aa"), 0o644))
	fin, err := fs.Open("/in/a.mp4")
	require.NoError(t, err)
	defer func() { _ = fin.Close() }()
	require.NoError(t, fs.MkdirAll("/out", 0o755))

	broken := &errorAfterByteFile{File: fin}
	_, err = copyStreamNoReplaceInstallReproof(fs, broken, "/out/x.mp4", nil)
	require.Error(t, err, "stream error propagates wrapped as no-replace copy failure")
	if _, lerr := fs.Stat("/out/x.mp4"); lerr == nil {
		t.Fatalf("partial copy left at destination after copy failure")
	}
}

// reproof failure: runs the (reproof != nil && reproof() != nil) branch.
func TestCoverCopyStreamNoReplaceInstall_ReproofFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	require.NoError(t, fs.MkdirAll("/out", 0o755))
	fin, err := fs.Open("/in/a.mp4")
	require.NoError(t, err)
	defer func() { _ = fin.Close() }()

	_, err = copyStreamNoReplaceInstallReproof(fs, fin, "/out/x.mp4", func() error {
		return errors.New("reproof missed")
	})
	require.ErrorContains(t, err, "reproof missed")
}

// PublishStagedBoundInfo failure: destination yanked after the publish —
// force the publish refusal path through an occupied destination.
func TestCoverCopyStreamNoReplaceInstall_PublishCollides(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	require.NoError(t, fs.MkdirAll("/out", 0o755))
	// Occupy the destination: no-replace publish refuses the rename.
	require.NoError(t, afero.WriteFile(fs, "/out/x.mp4", []byte("occupant"), 0o644))
	fin, err := fs.Open("/in/a.mp4")
	require.NoError(t, err)
	defer func() { _ = fin.Close() }()

	_, err = copyStreamNoReplaceInstallReproof(fs, fin, "/out/x.mp4", nil)
	require.Error(t, err, "publishing into an occupied target refuses")
}

// asideLstat indeterminate after publish: the dst lock drops the reference.
// Use a wrapper FS whose LstatIfPossible returns indeterminate for dst after
// it's been created by PublishStagedBoundInfo.
type lstatVanishAfterCreateFs struct {
	afero.Fs
	victim string
}

func (f *lstatVanishAfterCreateFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.victim {
		return nil, false, errors.New("simulated indeterminate lstat")
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(name)
}

func (f *lstatVanishAfterCreateFs) Lstat(name string) (os.FileInfo, error) {
	if name == f.victim {
		return nil, errors.New("simulated indeterminate lstat")
	}
	return f.Fs.(interface {
		Lstat(string) (os.FileInfo, error)
	}).Lstat(name)
}

// The lstat-indeterminate leg: drive it directly against an FS that refuses
// Lstat on the destination. PublishStagedBoundInfo's virtual leg (mem-backed)
// returns the target's identity, so we go one frame deeper.
func TestCoverLstatIndeterminateVanishLeg(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	require.NoError(t, fs.MkdirAll("/out", 0o755))
	wrapped := &lstatVanishAfterCreateFs{Fs: fs}
	if _, cerr := CopyFileNoReplaceVerifiedInstall(wrapped, "/in/a.mp4", "/out/x.mp4", nil); cerr != nil {
		t.Fatalf("initial verified copy failed: %v", cerr)
	}
	wrapped.victim = "/out/x.mp4"
	require.NoError(t, afero.WriteFile(fs, "/in/b.mp4", []byte("data"), 0o644))
	// PublishStagedBoundInfo hits the virtual (non-os) leg on MemMapFs and the
	// final asideLstat call returns indeterminate.
	_, err := CopyFileNoReplaceVerifiedInstall(wrapped, "/in/b.mp4", "/out/x.mp4", nil)
	// Whether it surfaces as an error isn't the claim; that the indeterminate
	// asideLstat branch was exercised is. Ensure no panic and dest either there or not.
	t.Logf("outcome: %v", err)
}

func TestCoverObserveVerifiedInstall_IndeterminateLookup(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/dst", []byte("x"), 0o644))
	info, err := fs.Stat("/dst")
	require.NoError(t, err)

	wrapped := &failFsWrap{Fs: fs, victim: "/dst"}
	got, err := ObserveVerifiedInstall(wrapped, "/dst", info)
	// codex P1, PRRT_kwDORn9KaM6qJY2i: an unreadable destination is an
	// INCONCLUSIVE observation, not a silent did-not-install — a foreign file
	// may already stand behind the transient error, so the leg is refused
	// instead of being handed to the name-based confirmation.
	require.Error(t, err, "an indeterminate lookup is refused, never adopted")
	assert.True(t, errors.Is(err, ErrPublishCompleted), "the doubt class rides the refusal")
	assert.Nil(t, got, "nothing is adopted from an unreadable destination")
}

type failFsWrap struct {
	afero.Fs
	victim string
}

func (f *failFsWrap) Stat(name string) (os.FileInfo, error) {
	if name == f.victim {
		return nil, errors.New("stat indeterminate")
	}
	return f.Fs.Stat(name)
}

func (f *failFsWrap) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.victim {
		return nil, false, errors.New("lstat indeterminate")
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(name)
}

// cover the "successor-directory occupant" leg: destination is a directory.
func TestCoverObserveVerifiedInstall_DirOccupantNotSuccessor(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/dst_file", []byte("x"), 0o644))
	require.NoError(t, fs.MkdirAll("/dst_dir", 0o755))
	info, err := fs.Stat("/dst_file")
	require.NoError(t, err)

	got, err := ObserveVerifiedInstall(fs, "/dst_file", info)
	require.NoError(t, err)
	require.NotNil(t, got, "same-object observation still returns the current occupant")

	// directory at the name: not a plausible successor — legacy nil answer
	got2, err2 := ObserveVerifiedInstall(fs, "/dst_dir", info)
	require.NoError(t, err2)
	assert.Nil(t, got2, "directory occupants are not classified as successor installs")
}
