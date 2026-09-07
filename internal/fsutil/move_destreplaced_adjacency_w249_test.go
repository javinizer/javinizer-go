package fsutil

// Probe-adjacency ceiling pins (PR #249 codex P2 — the rename-atomicity
// adjudication, F2): the same-device DestReplaced legs probe destination
// occupancy ONE syscall ahead of the publish rename, and no construction the
// afero Fs abstraction can express closes that window — POSIX rename(2) is
// atomically replace-SILENT, Linux renameat2(RENAME_EXCHANGE) / Darwin
// renamex_np(RENAME_SWAP) (the only kernel-exact displacement detectors)
// are non-portable islands unreachable through Fs.Rename, and the
// hardlink-snapshot dance keeps the same two-syscall adjacency with strictly
// more side effects (see the bound comment on MoveFileFsDestReplaced). These
// pins drive a foreign mutation INTO the exact probe → rename window — the
// wrapped probe answers with the filesystem's REAL state, then the
// plant/vacate lands before the rename runs — and pin the adjudicated
// residual RESULT shape in both directions:
//
//   - the publish always completes and the destination always carries OUR
//     bytes: rename(2) atomicity is never in question, so an ERROR is not
//     defensible (erroring a genuinely completed publish would misclassify a
//     landed destination as a failure upstream);
//   - what the window costs is crumb ACCURACY only — an occupant planted
//     inside it is displaced WITHOUT a crumb (the returned DestReplaced
//     answer is probe-instant truth), an occupant vacated inside it crumbs
//     with nothing displaced. The crumb is publish-adjacent best-effort
//     evidence by construction; these pins keep that contract honest.
//
// Dest-identity after the publish stays kernel-guaranteed on these legs (the
// rename moves the source dentry itself — dst names exactly what src named
// at the rename instant), so no post-publish reverify exists here; the
// staged legs' explicit post-publish reverify (PublishStagedBound) exists
// precisely because THEIR publish re-resolves a substitutable name.

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// probeWindowFs wedges a foreign mutation between a DestReplaced verb's dst
// occupancy probe and its publish rename: LstatIfPossible(dst) answers from
// the filesystem's real state and, one "scheduler tick" before the rename,
// the wedge mutates the destination. Deterministic and goroutine-free — the
// verbs probe dst exactly once per publish attempt.
type probeWindowFs struct {
	afero.Fs
	dst     string
	once    sync.Once
	fired   bool
	wedgeFn func(fs afero.Fs)
}

func (w *probeWindowFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	info, did, err := w.Fs.(afero.Lstater).LstatIfPossible(name)
	if filepath.Clean(name) == filepath.Clean(w.dst) {
		w.once.Do(func() {
			w.fired = true
			// The probe holds its truthful answer; the foreign mutation lands
			// strictly before this call's caller issues the rename — inside
			// the adjudicated probe → publish window.
			w.wedgeFn(w.Fs)
		})
	}
	return info, did, err
}

// adversarialRenameWedgeFs models the same window for the CROSS-DEVICE leg:
// the union-merged occupancy probe runs inside the publish closure one
// syscall ahead of ReplaceFile's rename of the staged name, so wedging the
// FIRST staged-name → dst rename plants between that probe and the
// publication. Same residual class as the same-device legs.
type adversarialRenameWedgeFs struct {
	afero.Fs
	src, dst string
	exdev    bool
	once     sync.Once
	fired    bool
	wedgeFn  func(fs afero.Fs)
}

func (w *adversarialRenameWedgeFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(w.src) && filepath.Clean(newname) == filepath.Clean(w.dst) {
		// The same-device pair answers EXDEV once, forcing the cross-device leg.
		if !w.exdev {
			w.exdev = true
			return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
		}
	}
	if filepath.Clean(newname) == filepath.Clean(w.dst) {
		// The staged publish rename: the closure's occupancy probe already
		// answered (one Lstat earlier) — the foreign plant lands here.
		w.once.Do(func() {
			w.fired = true
			w.wedgeFn(w.Fs)
		})
	}
	return w.Fs.Rename(oldname, newname)
}

func TestDestReplaced_ProbeAdjacencyWindow(t *testing.T) {
	t.Run("move leg: occupant planted after the probe is displaced with no crumb", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/in/src.txt", []byte("winner-bytes"), 0o644))
		fs := &probeWindowFs{Fs: base, dst: "/out/dst.txt", wedgeFn: func(under afero.Fs) {
			require.NoError(t, under.MkdirAll("/out", 0o755))
			require.NoError(t, afero.WriteFile(under, "/out/dst.txt", []byte("planted-foreign-bytes"), 0o644))
		}}

		replaced, err := MoveFileFsDestReplaced(fs, "/in/src.txt", "/out/dst.txt")
		require.NoError(t, err, "the publish genuinely completed — no error is defensible for a landed rename")
		require.True(t, fs.fired, "the plant landed inside the probe → rename window")
		assert.False(t, replaced,
			"the probe answered BEFORE the plant — the adjudicated residual: displacement without a crumb")
		content, rerr := afero.ReadFile(base, "/out/dst.txt")
		require.NoError(t, rerr)
		assert.Equal(t, []byte("winner-bytes"), content,
			"the plant's foreign bytes were really displaced by the rename — the audit gap is documented, not hidden")
		srcGone, serr := afero.Exists(base, "/in/src.txt")
		require.NoError(t, serr)
		assert.False(t, srcGone, "the move consumed its source")
	})

	t.Run("move leg: occupant vacated after the probe crumbs with nothing displaced", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/in/src.txt", []byte("winner-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/out/dst.txt", []byte("resident-bytes"), 0o644))
		fs := &probeWindowFs{Fs: base, dst: "/out/dst.txt", wedgeFn: func(under afero.Fs) {
			require.NoError(t, under.Remove("/out/dst.txt"))
		}}

		replaced, err := MoveFileFsDestReplaced(fs, "/in/src.txt", "/out/dst.txt")
		require.NoError(t, err)
		require.True(t, fs.fired)
		assert.True(t, replaced,
			"the probe answered BEFORE the vacate — the opposite adjudicated residual: a crumb with nothing displaced")
		content, rerr := afero.ReadFile(base, "/out/dst.txt")
		require.NoError(t, rerr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("rename verb: same window, same residual shapes", func(t *testing.T) {
		t.Run("plant", func(t *testing.T) {
			base := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(base, "/src.txt", []byte("winner-bytes"), 0o644))
			fs := &probeWindowFs{Fs: base, dst: "/dst.txt", wedgeFn: func(under afero.Fs) {
				require.NoError(t, afero.WriteFile(under, "/dst.txt", []byte("planted-foreign-bytes"), 0o644))
			}}

			replaced, err := RenameDestReplaced(fs, "/src.txt", "/dst.txt")
			require.NoError(t, err)
			require.True(t, fs.fired)
			assert.False(t, replaced, "displacement inside the probe → rename window carries no crumb")
			content, rerr := afero.ReadFile(base, "/dst.txt")
			require.NoError(t, rerr)
			assert.Equal(t, []byte("winner-bytes"), content)
		})

		t.Run("vacate", func(t *testing.T) {
			base := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(base, "/src.txt", []byte("winner-bytes"), 0o644))
			require.NoError(t, afero.WriteFile(base, "/dst.txt", []byte("resident-bytes"), 0o644))
			fs := &probeWindowFs{Fs: base, dst: "/dst.txt", wedgeFn: func(under afero.Fs) {
				require.NoError(t, under.Remove("/dst.txt"))
			}}

			replaced, err := RenameDestReplaced(fs, "/src.txt", "/dst.txt")
			require.NoError(t, err)
			require.True(t, fs.fired)
			assert.True(t, replaced, "a vacate inside the window keeps the probe's crumb — nothing was displaced")
			content, rerr := afero.ReadFile(base, "/dst.txt")
			require.NoError(t, rerr)
			assert.Equal(t, []byte("winner-bytes"), content)
		})
	})

	t.Run("cross-device leg: plant between the closure probe and the publish lands silently too", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/in/src.txt", []byte("winner-bytes"), 0o644))
		fs := &adversarialRenameWedgeFs{Fs: base, src: "/in/src.txt", dst: "/out/dst.txt", wedgeFn: func(under afero.Fs) {
			require.NoError(t, afero.WriteFile(under, "/out/dst.txt", []byte("planted-foreign-bytes"), 0o644))
		}}

		replaced, err := MoveFileFsDestReplaced(fs, "/in/src.txt", "/out/dst.txt")
		require.NoError(t, err)
		require.True(t, fs.exdev, "the move really took the cross-device leg")
		require.True(t, fs.fired, "the plant landed between the publish closure's probe and its rename")
		assert.False(t, replaced,
			"the staged publish's bound probe shares the same adjacency ceiling — displacement in its window carries no crumb")
		content, rerr := afero.ReadFile(base, "/out/dst.txt")
		require.NoError(t, rerr)
		assert.Equal(t, []byte("winner-bytes"), content,
			"the publish displaced the plant and landed our bytes — kernel atomicity intact, crumb silent")
		srcGone, serr := afero.Exists(base, "/in/src.txt")
		require.NoError(t, serr)
		assert.False(t, srcGone, "a completed cross-device move consumes its source")
	})
}
