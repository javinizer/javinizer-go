package fsutil

// Publish-bound replacement signal (PR #249 codex P2) — the DestReplaced
// verbs report whether the publish itself displaced an occupied destination
// not aliasing the source's own object. These pins cover the signal shape
// (vacant → false, foreign occupant → true, same-inode alias → false even
// though the entry is consumed) and the cross-device fallback's propagation
// of the staged publish's bound signal. The organizer keys its
// force-overwrite audit crumb on these answers because classify-time
// occupancy can be raced stale across the staging stream.

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyFileFsDestReplaced(t *testing.T) {
	t.Run("vacant destination reports no replacement", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/in/src.txt", []byte("winner-bytes"), 0o644))

		replaced, err := CopyFileFsDestReplaced(fs, "/in/src.txt", "/out/dst.txt")
		require.NoError(t, err)
		assert.False(t, replaced, "the publish landed on a provably vacant name")
		content, err := afero.ReadFile(fs, "/out/dst.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("occupied destination reports the displacement", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/in/src.txt", []byte("winner-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(fs, "/out/dst.txt", []byte("resident-bytes"), 0o644))

		replaced, err := CopyFileFsDestReplaced(fs, "/in/src.txt", "/out/dst.txt")
		require.NoError(t, err)
		assert.True(t, replaced, "the publish displaced the resident occupant")
		content, err := afero.ReadFile(fs, "/out/dst.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), content, "resident bytes were replaced")
		srcContent, err := afero.ReadFile(fs, "/in/src.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), srcContent, "a copy retains its source")
	})

	t.Run("lexical self stays a no-op with no replacement", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte("self"), 0o644))

		replaced, err := CopyFileFsDestReplaced(fs, "/src.txt", "/./src.txt")
		require.NoError(t, err)
		assert.False(t, replaced)
	})

	t.Run("directory creation failures surface with no signal", func(t *testing.T) {
		roFs := afero.NewReadOnlyFs(afero.NewMemMapFs())
		replaced, err := CopyFileFsDestReplaced(roFs, "/src.txt", "/nested/dir/dst.txt")
		require.Error(t, err, "MkdirAll must fail on a readonly fs")
		assert.False(t, replaced)
	})

	t.Run("same-inode alias occupant is not a foreign replacement", func(t *testing.T) {
		if testing.Short() {
			t.Skip("os-level hardlink fixture")
		}
		dir := t.TempDir()
		fs := afero.NewOsFs()
		src := filepath.Join(dir, "src.txt")
		dst := filepath.Join(dir, "dst.txt")
		require.NoError(t, os.WriteFile(src, []byte("shared-bytes"), 0o644))
		require.NoError(t, os.Link(src, dst), "destination aliases the source inode")

		replaced, err := CopyFileFsDestReplaced(fs, src, dst)
		require.NoError(t, err)
		assert.False(t, replaced,
			"displacing an alias ENTRY destroys no foreign bytes — the inode lives on at the source")
		content, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, []byte("shared-bytes"), content)
		srcInfo, err := os.Stat(src)
		require.NoError(t, err)
		dstInfo, err := os.Stat(dst)
		require.NoError(t, err)
		assert.False(t, os.SameFile(srcInfo, dstInfo),
			"the publish installed the staged copy: dst no longer aliases the source inode")
	})
}

func TestMoveFileFsDestReplaced(t *testing.T) {
	t.Run("vacant destination reports no replacement", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte("winner-bytes"), 0o644))

		replaced, err := MoveFileFsDestReplaced(fs, "/src.txt", "/out/dst.txt")
		require.NoError(t, err)
		assert.False(t, replaced)
		content, err := afero.ReadFile(fs, "/out/dst.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("occupied destination reports the displacement", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte("winner-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(fs, "/out/dst.txt", []byte("resident-bytes"), 0o644))

		replaced, err := MoveFileFsDestReplaced(fs, "/src.txt", "/out/dst.txt")
		require.NoError(t, err)
		assert.True(t, replaced, "the rename displaced the resident occupant")
		content, err := afero.ReadFile(fs, "/out/dst.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), content)
		srcExists, err := afero.Exists(fs, "/src.txt")
		require.NoError(t, err)
		assert.False(t, srcExists, "the move consumed the source name")
	})

	t.Run("lexical self stays a no-op with no replacement", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte("self"), 0o644))

		replaced, err := MoveFileFsDestReplaced(fs, "/src.txt", "/./src.txt")
		require.NoError(t, err)
		assert.False(t, replaced)
	})

	t.Run("directory creation failures surface with no signal", func(t *testing.T) {
		roFs := afero.NewReadOnlyFs(afero.NewMemMapFs())
		replaced, err := MoveFileFsDestReplaced(roFs, "/src.txt", "/nested/dir/dst.txt")
		require.Error(t, err)
		assert.False(t, replaced)
	})

	t.Run("rename failures stay plain errors with no signal", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		replaced, err := MoveFileFsDestReplaced(fs, "/nonexistent.txt", "/dst.txt")
		require.Error(t, err, "a vanished source rename fails as before")
		assert.False(t, replaced)
		assert.Contains(t, err.Error(), "failed to move file")
	})

	t.Run("same-inode alias occupant is not a foreign replacement", func(t *testing.T) {
		if testing.Short() {
			t.Skip("os-level hardlink fixture")
		}
		dir := t.TempDir()
		fs := afero.NewOsFs()
		src := filepath.Join(dir, "src.txt")
		dst := filepath.Join(dir, "dst.txt")
		require.NoError(t, os.WriteFile(src, []byte("shared-bytes"), 0o644))
		require.NoError(t, os.Link(src, dst))

		replaced, err := MoveFileFsDestReplaced(fs, src, dst)
		require.NoError(t, err)
		assert.False(t, replaced,
			"dst aliased the same inode the rename published — no foreign bytes displaced")
		// POSIX specifies rename(2) between two hard links of ONE file as a
		// no-op: NOTHING was displaced, and both names keep the shared bytes —
		// exactly the silence the crumb answer asserts.
		content, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, []byte("shared-bytes"), content)
		srcContent, err := os.ReadFile(src)
		require.NoError(t, err)
		assert.Equal(t, []byte("shared-bytes"), srcContent)
	})
}

// exdevOnceRenameFs forces the FIRST rename of the named source to fail EXDEV
// so MoveFileFsDestReplaced takes the cross-device fallback; every other
// rename (the staged publish inside the fallback) delegates normally.
type exdevOnceRenameFs struct {
	afero.Fs
	injectSrc string
	injected  bool
}

func (e *exdevOnceRenameFs) Rename(src, dst string) error {
	if !e.injected && src == e.injectSrc {
		e.injected = true
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
	}
	return e.Fs.Rename(src, dst)
}

func TestMoveFileFsDestReplaced_CrossDeviceLeg(t *testing.T) {
	t.Run("cross-device onto occupied destination propagates the publish signal", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &exdevOnceRenameFs{Fs: base, injectSrc: "/src.txt"}
		require.NoError(t, afero.WriteFile(base, "/src.txt", []byte("winner-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/out/dst.txt", []byte("resident-bytes"), 0o644))

		replaced, err := MoveFileFsDestReplaced(fs, "/src.txt", "/out/dst.txt")
		require.NoError(t, err)
		assert.True(t, replaced,
			"the fallback's staged publish displaced the resident occupant — the bound signal must survive the EXDEV hop")
		content, err := afero.ReadFile(base, "/out/dst.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), content)
		srcExists, err := afero.Exists(base, "/src.txt")
		require.NoError(t, err)
		assert.False(t, srcExists, "the cross-device move removed the source")
	})

	t.Run("cross-device onto vacant destination stays silent", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &exdevOnceRenameFs{Fs: base, injectSrc: "/src.txt"}
		require.NoError(t, afero.WriteFile(base, "/src.txt", []byte("winner-bytes"), 0o644))

		replaced, err := MoveFileFsDestReplaced(fs, "/src.txt", "/out/dst.txt")
		require.NoError(t, err)
		assert.False(t, replaced)
		content, err := afero.ReadFile(base, "/out/dst.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), content)
	})
}

func TestRenameDestReplaced(t *testing.T) {
	t.Run("vacant destination reports no replacement", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte("winner-bytes"), 0o644))

		replaced, err := RenameDestReplaced(fs, "/src.txt", "/dst.txt")
		require.NoError(t, err)
		assert.False(t, replaced)
		content, err := afero.ReadFile(fs, "/dst.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("occupied destination reports the displacement with the same bare rename", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte("winner-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(fs, "/dst.txt", []byte("resident-bytes"), 0o644))

		replaced, err := RenameDestReplaced(fs, "/src.txt", "/dst.txt")
		require.NoError(t, err)
		assert.True(t, replaced)
		content, err := afero.ReadFile(fs, "/dst.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("rename error surfaces with the probe's answer irrelevant", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		replaced, err := RenameDestReplaced(fs, "/nonexistent.txt", "/dst.txt")
		require.Error(t, err)
		assert.False(t, replaced, "a vanished source probes nothing to exclude and displaces nothing")
	})
}

// statOnlyFs hides any Lstater capability of the wrapped filesystem so the
// publish probes take the link-following Stat leg (the no-Lstat wrapper shape
// the codebase's destMaybeLstat/asideLstat discipline exists for).
type statOnlyFs struct{ afero.Fs }

func TestPublishProbeIdentity_StatLegs(t *testing.T) {
	t.Run("non-Lstater wrapper probes through Stat", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := statOnlyFs{base}
		require.NoError(t, afero.WriteFile(base, "/src.txt", []byte("winner-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/dst.txt", []byte("resident-bytes"), 0o644))

		replaced, err := MoveFileFsDestReplaced(fs, "/src.txt", "/dst.txt")
		require.NoError(t, err)
		assert.True(t, replaced, "Stat-leg probes still see the resident occupant")
	})

	t.Run("indeterminate probes answer no displacement — confirm-or-silent", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/src.txt", []byte("winner-bytes"), 0o644))
		fs := probeDenyFs{Fs: statOnlyFs{base}, deny: map[string]bool{"/src.txt": true, "/dst.txt": true}}

		replaced, err := MoveFileFsDestReplaced(fs, "/src.txt", "/dst.txt")
		require.NoError(t, err)
		assert.False(t, replaced,
			"failed identity probes apply no alias exclusion and a failed occupancy probe claims nothing")
	})
}

// probeDenyFs fails Stat on the denied names only, making the publish probes
// indeterminate while every other operation (read/rename) delegates normally.
type probeDenyFs struct {
	afero.Fs
	deny map[string]bool
}

func (d probeDenyFs) Stat(name string) (os.FileInfo, error) {
	if d.deny[name] {
		return nil, errors.New("probe denied")
	}
	return d.Fs.Stat(name)
}
