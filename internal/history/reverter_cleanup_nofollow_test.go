package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// cleanupLinkModeInfo mirrors the workflow package's symlinkModeInfo
// (artifact_staging_source_identity_test.go): MemMapFs has no symlink model,
// so platform-independent link-occupant legs model the directory entry's mode
// directly — the shape a no-follow lookup of a symlink reports.
type cleanupLinkModeInfo struct{ os.FileInfo }

func (cleanupLinkModeInfo) Mode() os.FileMode { return os.ModeSymlink | 0o777 }

// cleanupLinkEntryLstatFs reports linkPath as a symlink directory entry
// through the no-follow lookup while the wrapped memfs keeps its own state
// for that path: seed a real file there to model a link whose TARGET carries
// those bytes (a following Stat/Open still resolves them), or leave it absent
// to model a dangling link (a following Stat answers ENOENT).
type cleanupLinkEntryLstatFs struct {
	afero.Fs
	linkPath string
	info     os.FileInfo
}

func (f *cleanupLinkEntryLstatFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Clean(name) == filepath.Clean(f.linkPath) {
		return cleanupLinkModeInfo{f.info}, true, nil
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// codex P2 (PRRT_kwDORn9KaM6m5HFu): the move-back source-vacancy probe is
// no-FOLLOW — a dangling symlink at the original path is an occupied
// directory entry even though Stat answers ENOENT through it, and the POSIX
// rename back would otherwise REPLACE that foreign link. The matrix pins
// every occupant class: absent renames back; file, directory, and symlink
// occupants all suppress; the error leg is covered by the round-24 family
// ("unprovable source state suppresses the rename and stays the pin").
func TestCleanupGeneratedFilesFS_MoveBackSourceOccupants(t *testing.T) {
	const (
		src = "/src-w161e/W161E-001-cd2.mp4"
		dst = "/dst-w161e/lib/W161E-001-cd2.mp4"
	)
	newOp := func(pinned bool) *models.BatchFileOperation {
		gf := models.GeneratedFilesJSON{MoveBack: []models.FileMove{{OriginalPath: src, NewPath: dst}}}
		if pinned {
			gf.PlannedDeletes = []models.DeleteEntry{{Path: dst, SHA256: sha256HexOf([]byte("part two"))}}
		}
		return &models.BatchFileOperation{OperationType: models.OperationTypeMove, GeneratedFiles: models.MarshalLedgerJSON(gf)}
	}
	seedTarget := func(t *testing.T, fs afero.Fs) {
		require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o777))
		require.NoError(t, afero.WriteFile(fs, dst, []byte("part two"), 0o666))
	}

	t.Run("absent source renames the target back", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seedTarget(t, fs)
		require.NoError(t, fs.MkdirAll("/src-w161e", 0o777))
		cleanupGeneratedFilesFS(fs, newOp(false), "/dst-w161e")
		got, err := afero.ReadFile(fs, src)
		require.NoError(t, err)
		assert.Equal(t, "part two", string(got))
		if _, err := fs.Stat(dst); !os.IsNotExist(err) {
			t.Fatalf("the consumed move vacated the destination: %v", err)
		}
	})

	t.Run("regular file occupant suppresses the rename", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seedTarget(t, fs)
		require.NoError(t, fs.MkdirAll("/src-w161e", 0o777))
		require.NoError(t, afero.WriteFile(fs, src, []byte("user re-edit"), 0o666))
		cleanupGeneratedFilesFS(fs, newOp(false), "/dst-w161e")
		got, err := afero.ReadFile(fs, src)
		require.NoError(t, err)
		assert.Equal(t, "user re-edit", string(got), "the occupant is never renamed over")
		got, err = afero.ReadFile(fs, dst)
		require.NoError(t, err)
		assert.Equal(t, "part two", string(got), "the unpinned target is retained")
	})

	t.Run("directory occupant suppresses the rename", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seedTarget(t, fs)
		require.NoError(t, fs.MkdirAll(src, 0o777))
		cleanupGeneratedFilesFS(fs, newOp(false), "/dst-w161e")
		info, err := fs.Stat(src)
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "a directory at the source is never renamed over")
		got, err := afero.ReadFile(fs, dst)
		require.NoError(t, err)
		assert.Equal(t, "part two", string(got))
	})

	t.Run("dangling symlink occupant suppresses the rename", func(t *testing.T) {
		base := afero.NewMemMapFs()
		seedTarget(t, base)
		targetInfo, err := base.Stat(dst)
		require.NoError(t, err)
		fs := &cleanupLinkEntryLstatFs{Fs: base, linkPath: src, info: targetInfo}
		cleanupGeneratedFilesFS(fs, newOp(false), "/dst-w161e")
		if _, err := base.Stat(src); !os.IsNotExist(err) {
			t.Fatalf("the rename never materialized anything over the foreign link: %v", err)
		}
		link, lerr := lstatRestoreSource(fs, src)
		require.NoError(t, lerr)
		assert.NotZero(t, link.Mode()&os.ModeSymlink, "the foreign symlink entry survives byte-for-byte")
		got, err := afero.ReadFile(base, dst)
		require.NoError(t, err)
		assert.Equal(t, "part two", string(got), "suppressed: the unpinned target is retained")
	})

	t.Run("dangling symlink occupant with a surviving pin retains the pinned copy too", func(t *testing.T) {
		base := afero.NewMemMapFs()
		seedTarget(t, base)
		targetInfo, err := base.Stat(dst)
		require.NoError(t, err)
		fs := &cleanupLinkEntryLstatFs{Fs: base, linkPath: src, info: targetInfo}
		cleanupGeneratedFilesFS(fs, newOp(true), "/dst-w161e")
		got, err := afero.ReadFile(base, dst)
		require.NoError(t, err, "neither the rename nor the pin may act on an armed-and-suppressed target")
		assert.Equal(t, "part two", string(got))
		if _, err := base.Stat(src); !os.IsNotExist(err) {
			t.Fatalf("nothing materialized at the linked source path: %v", err)
		}
		link, lerr := lstatRestoreSource(fs, src)
		require.NoError(t, lerr)
		assert.NotZero(t, link.Mode()&os.ModeSymlink, "the foreign symlink entry survives byte-for-byte")
	})
}

// codex P2 (PRRT_kwDORn9KaM6m5HF7): a planned delete fires only on the pinned
// REGULAR file — the hash pin certifies the published bytes at a regular-file
// destination, never a successor symlink whose target happens to carry the
// same bytes (hashing through the link would bless it and Remove would unlink
// an unrelated directory entry), a directory, or an unprovable entry.
func TestCleanupGeneratedFilesFS_PlannedDeleteEntryKinds(t *testing.T) {
	const dst = "/dst-w161f/lib/W161F-001-tray.jpg"
	newOp := func(pin string) *models.BatchFileOperation {
		return &models.BatchFileOperation{
			OperationType:  models.OperationTypeMove,
			GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PlannedDeletes: []models.DeleteEntry{{Path: dst, SHA256: pin}}}),
		}
	}
	seed := func(t *testing.T, fs afero.Fs, bytes []byte) {
		require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o777))
		require.NoError(t, afero.WriteFile(fs, dst, bytes, 0o666))
	}

	t.Run("regular file with the pinned bytes is removed", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seed(t, fs, []byte("published"))
		cleanupGeneratedFilesFS(fs, newOp(sha256HexOf([]byte("published"))), "/dst-w161f")
		if _, err := fs.Stat(dst); !os.IsNotExist(err) {
			t.Fatalf("the pinned regular file fired: %v", err)
		}
	})

	t.Run("regular file with different bytes is retained", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seed(t, fs, []byte("rebuilt"))
		cleanupGeneratedFilesFS(fs, newOp(sha256HexOf([]byte("published"))), "/dst-w161f")
		got, err := afero.ReadFile(fs, dst)
		require.NoError(t, err)
		assert.Equal(t, "rebuilt", string(got))
	})

	t.Run("absent destination is consumed without error", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o777))
		cleanupGeneratedFilesFS(fs, newOp(sha256HexOf([]byte("published"))), "/dst-w161f")
		if _, err := fs.Stat(dst); !os.IsNotExist(err) {
			t.Fatalf("the absent destination stayed absent: %v", err)
		}
	})

	t.Run("directory occupant is retained", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(dst, 0o777))
		cleanupGeneratedFilesFS(fs, newOp(sha256HexOf([]byte("published"))), "/dst-w161f")
		info, err := fs.Stat(dst)
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "a directory entry is never hash-and-removed")
	})

	t.Run("symlink occupant with pinned target bytes is retained", func(t *testing.T) {
		base := afero.NewMemMapFs()
		seed(t, base, []byte("published"))
		targetInfo, err := base.Stat(dst)
		require.NoError(t, err)
		fs := &cleanupLinkEntryLstatFs{Fs: base, linkPath: dst, info: targetInfo}
		cleanupGeneratedFilesFS(fs, newOp(sha256HexOf([]byte("published"))), "/dst-w161f")
		link, lerr := lstatRestoreSource(fs, dst)
		require.NoError(t, lerr)
		assert.NotZero(t, link.Mode()&os.ModeSymlink,
			"the foreign link is never unlinked — a following open would have matched the pin and removed it")
		got, err := afero.ReadFile(base, dst)
		require.NoError(t, err)
		assert.Equal(t, "published", string(got))
	})

	t.Run("unprovable destination is retained fail-closed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		seed(t, base, []byte("published"))
		fs := &statDenyFS{Fs: base, path: dst, err: errors.New("probe denied")}
		cleanupGeneratedFilesFS(fs, newOp(sha256HexOf([]byte("published"))), "/dst-w161f")
		got, err := afero.ReadFile(base, dst)
		require.NoError(t, err)
		assert.Equal(t, "published", string(got), "an unprovable destination is never deleted, pin or not")
	})
}

// Real-filesystem proof of both no-follow legs (the memfs legs above model
// link entries through the Lstater wrapper): a REAL dangling symlink at the
// move-back source suppresses the rename-back and survives untouched, the
// armed pinned target retained alongside it (codex P1,
// PRRT_kwDORn9KaM6m5kmF), and a REAL symlink at an un-armed pinned
// planned-delete destination whose target carries the pinned bytes is never
// hashed through or unlinked.
func TestRevertCleanupNoFollowRealSymlinks(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst", "lib", "W161G-001")
	require.NoError(t, os.MkdirAll(srcDir, 0o777))
	require.NoError(t, os.MkdirAll(dstDir, 0o777))
	videoSource := filepath.Join(srcDir, "W161G-001.mkv")
	videoTarget := filepath.Join(dstDir, "W161G-001.mkv")
	siblingSource := filepath.Join(srcDir, "W161G-001-cd2.mp4")
	siblingTarget := filepath.Join(dstDir, "W161G-001-cd2.mp4")
	require.NoError(t, os.WriteFile(videoTarget, []byte("video"), 0o666))
	require.NoError(t, os.WriteFile(siblingTarget, []byte("part two"), 0o666))
	danglingTarget := filepath.Join(srcDir, "gone.mp4")
	if err := os.Symlink(danglingTarget, siblingSource); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	stray := filepath.Join(dstDir, "W161G-001-stray.nfo")
	strayVictim := filepath.Join(root, "stray-victim.nfo")
	require.NoError(t, os.WriteFile(strayVictim, []byte("stray"), 0o666))
	if err := os.Symlink(strayVictim, stray); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	op := &models.BatchFileOperation{
		BatchJobID:    "job-w161g-nofollow",
		MovieID:       "W161G-001",
		OriginalPath:  videoSource,
		NewPath:       videoTarget,
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			PlannedDeletes: []models.DeleteEntry{
				{Path: siblingTarget, SHA256: sha256HexOf([]byte("part two"))},
				{Path: stray, SHA256: sha256HexOf([]byte("stray"))},
			},
			MoveBack: []models.FileMove{{OriginalPath: siblingSource, NewPath: siblingTarget}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w161g-nofollow")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	linkDest, err := os.Readlink(siblingSource)
	require.NoError(t, err)
	assert.Equal(t, danglingTarget, linkDest, "the foreign dangling symlink survives byte-for-byte — the rename back never ran")
	siblingBytes, siblingErr := os.ReadFile(siblingTarget)
	require.NoError(t, siblingErr, "the armed pinned target is retained — neither renamed over the link nor deleted by its pin")
	assert.Equal(t, "part two", string(siblingBytes))

	linkDest, err = os.Readlink(stray)
	require.NoError(t, err)
	assert.Equal(t, strayVictim, linkDest, "the hash pin never unlinked the foreign symlink")
	got, err := os.ReadFile(strayVictim)
	require.NoError(t, err)
	assert.Equal(t, "stray", string(got), "the link target's pinned bytes are intact")

	got, err = os.ReadFile(videoSource)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got), "the ordinary column-driven primary still reverted")
}
