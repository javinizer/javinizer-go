package organizer

// PR #249 codex P2 (F2) — the RESULT-level pin of the rename-adjacency
// adjudication: the move lane's publish-bound crumb probes destination
// occupancy ONE syscall ahead of the publish rename (fsutil's adjudicated
// accuracy ceiling — no portable kernel primitive reports displacement AT
// the rename instant; see the bound comment on fsutil.MoveFileFsDestReplaced).
// These pins land a foreign mutation INSIDE that exact window — the wrapped
// Rename runs the plant/vacate one "scheduler tick" before delegating, after
// the lane's probe already answered — and document the unexpected-but-
// adjudicated RESULT shapes:
//
//   - plant inside the window: the rename really displaces the foreign bytes
//     yet the result crumb is SILENT (probe-instant truth) — the window's
//     documented audit gap. The move still completed and the destination
//     carries our bytes, so NO error is defensible;
//   - vacate inside the window: the crumb FIRES although the rename
//     displaced nothing — probe-time evidence keys it.
//
// The classify → publish window (the finding this PR's publish-bound rework
// closed) is pinned separately in force_overwrite_publish_bound_test.go;
// this file pins the narrower, physically unclosable probe → rename window
// that rework deliberately leaves as the ceiling.

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// renameAdjacencyWedgeFs wedges the foreign mutation strictly between the
// move lane's publish-adjacent occupancy probe and the publish rename: the
// probe has already answered when this Rename runs, and the wedge mutates
// the destination before delegating — the narrowest window any same-device
// rename publish exposes.
type renameAdjacencyWedgeFs struct {
	afero.Fs
	src, dst string
	once     sync.Once
	fired    bool
	wedge    func(fs afero.Fs)
}

func (w *renameAdjacencyWedgeFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(w.src) && filepath.Clean(newname) == filepath.Clean(w.dst) {
		w.once.Do(func() {
			w.fired = true
			w.wedge(w.Fs)
		})
	}
	return w.Fs.Rename(oldname, newname)
}

func TestForceOverwriteAudit_RenameAdjacencyWindow_ResultShape(t *testing.T) {
	const (
		src = "/in/A.mkv"
		dst = "/dest/ABC-123/ABC-123.mkv"
	)

	organizerOver := func(base afero.Fs, wedge func(fs afero.Fs)) *Organizer {
		return NewOrganizer(&renameAdjacencyWedgeFs{Fs: base, src: src, dst: dst, wedge: wedge}, &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeOrganize,
		}, nil, nil)
	}

	t.Run("plant inside the probe-rename window: completed move, silent crumb", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, src, []byte("winner-bytes"), 0o644))
		org := organizerOver(base, func(fs afero.Fs) {
			require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0o755))
			require.NoError(t, afero.WriteFile(fs, dst, []byte("planted-foreign-bytes"), 0o644))
		})

		result, err := org.Organize(context.Background(), forceAuditCmd(src, true, true))
		require.NoError(t, err, "the publish genuinely completed — no error is defensible for a landed rename")
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"the probe answered BEFORE the plant — the adjudicated audit gap: displacement without a crumb")
		content, rerr := afero.ReadFile(base, dst)
		require.NoError(t, rerr)
		assert.Equal(t, []byte("winner-bytes"), content,
			"the plant's foreign bytes were really displaced — documented, not hidden")
	})

	t.Run("vacate inside the probe-rename window: crumb fires with nothing displaced", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, src, []byte("winner-bytes"), 0o644))
		require.NoError(t, base.MkdirAll("/dest/ABC-123", 0o755))
		require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
		org := organizerOver(base, func(fs afero.Fs) {
			require.NoError(t, fs.Remove(dst))
		})

		result, err := org.Organize(context.Background(), forceAuditCmd(src, true, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1,
			"the probe answered BEFORE the vacate — the crumb keys on probe-instant evidence, nothing was displaced")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
		content, rerr := afero.ReadFile(base, dst)
		require.NoError(t, rerr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})
}
