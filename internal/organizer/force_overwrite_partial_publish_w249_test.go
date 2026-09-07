package organizer

// PR #249 codex P2 follow-up (F1) — the partial-publish audit crumb: a
// force-overwrite move driven onto its cross-device leg (injected EXDEV)
// whose bound staged publish LANDED on an occupied destination but whose
// source cleanup then refused (the typed fsutil.ErrPublishCompleted
// ambiguity) must NOT drop the displaced-occupancy evidence with the error.
// The pre-fix chain returned replaced=false pre-error from fsutil's fallback
// and every move lane bailed before warning — exactly the replacement the
// audit exists to disclose vanished from every console/result warning
// consumer. These pins wedge the source cleanup of each MoveFileFsDestReplaced
// lane (organize move, in-place non-rename move, in-place-norenamefolder
// rename) and assert the RESULT shape:
//
//   - the FAILED result carries the crumb in Warnings (the console,
//     eventlog, and worker history surfaces all read that slice);
//   - the error unwraps to fsutil.ErrPublishCompleted (the publish-completed
//     typing rides up so Apply journals the ambiguity as an event
//     nonetheless — the w241 revert-log lineage keeps that failed row
//     revertable, pointing at the published destination);
//   - Moved stays false and the journal-shaping markers stay untouched, so
//     the destination is NEVER double-journaled as if a clean move/replace
//     happened next to the partial-publish row;
//   - both objects stay byte-intact on disk (#224 keep-both).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// partialPublishWedgeFs is the organizer-side F1 armature, bound by name
// (unlike the content-sniffing w241 wedges): the same-device rename of the
// exact (src, dst) pair answers EXDEV — forcing the authorized move onto its
// cross-device leg (stage, bound publish, source cleanup) — and exactly the
// post-publish source removal refuses. The staged publish's own rename
// (staged → dst) delegates normally, as does every unrelated Remove.
type partialPublishWedgeFs struct {
	afero.Fs
	src, dst     string
	removeFailed bool
}

func (w *partialPublishWedgeFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(w.src) && filepath.Clean(newname) == filepath.Clean(w.dst) {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
	}
	return w.Fs.Rename(oldname, newname)
}

func (w *partialPublishWedgeFs) Remove(name string) error {
	if filepath.Clean(name) == filepath.Clean(w.src) && !w.removeFailed {
		w.removeFailed = true
		return &os.PathError{Op: "remove", Path: name, Err: syscall.EPERM}
	}
	return w.Fs.Remove(name)
}

func TestForceOverwriteAudit_PartialPublishCrumb_OrganizeMoveLane(t *testing.T) {
	const (
		src = "/in/A.mkv"
		dst = "/dest/ABC-123/ABC-123.mkv"
	)

	t.Run("occupied destination: FAILED result carries the crumb and the publish-completed typing", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, src, []byte("winner-bytes"), 0o644))
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
		wedge := &partialPublishWedgeFs{Fs: base, src: src, dst: dst}
		org := NewOrganizer(wedge, &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeOrganize,
		}, nil, nil)

		result, err := org.Organize(context.Background(), forceAuditCmd(src, true, true))
		require.Error(t, err)
		assert.True(t, errors.Is(err, fsutil.ErrPublishCompleted),
			"the publish-completed typing propagates — Apply journals this as an event nonetheless")
		require.True(t, wedge.removeFailed, "the wedge really fired at the post-publish source removal")
		require.NotNil(t, result)
		assert.False(t, result.Moved,
			"the source is retained — this is NOT a clean move and must never journal as one")
		assert.False(t, result.DuplicateSkipped)
		assert.False(t, result.PrePublication,
			"a publish-completed failure is NOT the pre-publication no-op class — the event keeps its destination")
		assert.Equal(t, filepath.ToSlash(dst), filepath.ToSlash(result.NewPath),
			"the failed event still names the published destination (single partial-publish journal row)")
		require.Len(t, result.Warnings, 1,
			"the displaced resident bytes keep their audit crumb on the FAILED result — the pre-fix drop")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))

		content, rerr := afero.ReadFile(base, dst)
		require.NoError(t, rerr)
		assert.Equal(t, []byte("winner-bytes"), content,
			"the publish really displaced the resident bytes the crumb discloses")
		retained, serr := afero.ReadFile(base, src)
		require.NoError(t, serr, "the source is preserved byte-intact (#224 keep-both)")
		assert.Equal(t, []byte("winner-bytes"), retained)
	})

	t.Run("vacant destination: same publish-completed failure, no crumb", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, src, []byte("winner-bytes"), 0o644))
		wedge := &partialPublishWedgeFs{Fs: base, src: src, dst: dst}
		org := NewOrganizer(wedge, &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeOrganize,
		}, nil, nil)

		result, err := org.Organize(context.Background(), forceAuditCmd(src, true, true))
		require.Error(t, err)
		assert.True(t, errors.Is(err, fsutil.ErrPublishCompleted))
		require.NotNil(t, result)
		assert.False(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"nothing was displaced — the crumb never fires on unconfirmed evidence, failure or not")
		content, rerr := afero.ReadFile(base, dst)
		require.NoError(t, rerr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})
}

func TestForceOverwriteAudit_PartialPublishCrumb_InPlaceLanes(t *testing.T) {
	t.Run("in-place non-rename move lane keeps the crumb on the partial publish", func(t *testing.T) {
		base := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat: "<ID>", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlace,
		}
		const (
			src = "/pool/mixed/ABC-100 ownA.mkv"
			dst = "/pool/mixed/ABC-100 vidfile.mkv"
		)
		require.NoError(t, base.MkdirAll("/pool/mixed", 0o755))
		require.NoError(t, afero.WriteFile(base, src, []byte("a-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/pool/mixed/DEF-999 other.mkv", []byte("other-tenant"), 0o644))
		require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
		wedge := &partialPublishWedgeFs{Fs: base, src: src, dst: dst}
		org := NewOrganizer(wedge, cfg, nil, ipfMatcher(t))

		result, err := org.Organize(context.Background(), OrganizeCmd{
			Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: src, Name: "ABC-100 ownA.mkv", Extension: ".mkv"},
			Movie:       &models.Movie{ID: "ABC-100"},
			DestDir:     "/dest",
			MoveFiles:   true,
			ForceUpdate: true,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, fsutil.ErrPublishCompleted),
			"the wrapped lane error keeps the publish-completed typing unwrap-reachable")
		require.NotNil(t, result)
		assert.False(t, result.Moved)
		require.Len(t, result.Warnings, 1,
			"the in-place non-rename move lane carries the partial-publish crumb too")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
		content, rerr := afero.ReadFile(base, dst)
		require.NoError(t, rerr)
		assert.Equal(t, []byte("a-bytes"), content, "the resident bytes were really displaced")
		retained, serr := afero.ReadFile(base, src)
		require.NoError(t, serr)
		assert.Equal(t, []byte("a-bytes"), retained, "the source is retained byte-intact")
	})

	t.Run("in-place-norenamefolder lane keeps the crumb on the partial publish", func(t *testing.T) {
		base := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat: "<ID>", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		}
		const (
			src = "/pool/ABC-100 ownA.mkv"
			dst = "/pool/ABC-100 vidfile.mkv"
		)
		require.NoError(t, base.MkdirAll("/pool", 0o755))
		require.NoError(t, afero.WriteFile(base, src, []byte("a-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
		wedge := &partialPublishWedgeFs{Fs: base, src: src, dst: dst}
		org := NewOrganizer(wedge, cfg, nil, nil)

		result, err := org.Organize(context.Background(), OrganizeCmd{
			Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: src, Name: "ABC-100 ownA.mkv", Extension: ".mkv"},
			Movie:       &models.Movie{ID: "ABC-100"},
			DestDir:     "/dest",
			MoveFiles:   true,
			ForceUpdate: true,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, fsutil.ErrPublishCompleted))
		require.NotNil(t, result)
		assert.False(t, result.Moved)
		require.Len(t, result.Warnings, 1,
			"the norenamefolder lane carries the partial-publish crumb too")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
		content, rerr := afero.ReadFile(base, dst)
		require.NoError(t, rerr)
		assert.Equal(t, []byte("a-bytes"), content)
		retained, serr := afero.ReadFile(base, src)
		require.NoError(t, serr)
		assert.Equal(t, []byte("a-bytes"), retained)
	})
}
