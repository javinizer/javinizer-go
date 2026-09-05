package workflow

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// partialPublishOwnerFs replays the PR #241 P1 owner ambiguity on the shared
// virtual filesystem: the primed owner's same-volume rename answers EXDEV
// (forcing the authorized move onto its cross-device leg — stage, bound
// publish, source cleanup), then the first removal of a non-empty object
// fails once — the post-publish source-cleanup wedge. The destination by
// then carries the OWNER's published bytes; both objects stay byte-intact.
type partialPublishOwnerFs struct {
	afero.Fs
	src, dst     string
	removeFailed atomic.Bool
}

func (p *partialPublishOwnerFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(p.src) && filepath.Clean(newname) == filepath.Clean(p.dst) {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
	}
	return p.Fs.Rename(oldname, newname)
}

func (p *partialPublishOwnerFs) Remove(name string) error {
	if info, err := p.Fs.Stat(name); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		if p.removeFailed.CompareAndSwap(false, true) {
			return &os.PathError{Op: "remove", Path: name, Err: syscall.EPERM}
		}
	}
	return p.Fs.Remove(name)
}

// TestRevertLog_PartialPublishOwner_RevertDragsNoForeignBytes is the PR #241
// P1 revert-level regression (codex: the partial-published owner's journal
// must be authored so the revert path never drags another claimant's bytes):
// a primed force-mode owner cross-device-PUBLISHES the shared destination
// but its source cleanup fails (ambiguous error, both files retained). The
// claim SETTLES, so the blocked waiter resolves to its authorized-skip
// verdict and publishes NOTHING. Then, through the REAL sqlite revert log
// and history reverter:
//
//  1. the waiter's journal carries no primary-move record (the existing #241
//     P1 duplicate-skip rule) — reverting it moves nothing;
//  2. the failed owner's row still names the SHARED destination, but that
//     destination byte-provably belongs to the owner, so its revert refuses
//     while the retained source copy occupies the original path (both kept,
//     per the #234 ambiguous-failure invariant) and, once the operator has
//     cleared the retained copy, restores the OWNER's OWN bytes — never a
//     promoted claimant's, because with the settled claim none was promoted.
func TestRevertLog_PartialPublishOwner_RevertDragsNoForeignBytes(t *testing.T) {
	_, repo, base, rl := newW241Harness(t)
	ctx := context.Background()

	require.NoError(t, base.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/in/B.mkv", []byte("b-bytes"), 0o644))
	poison := &partialPublishOwnerFs{
		Fs:  base,
		src: "/in/A.mkv",
		dst: filepath.FromSlash(w241Target),
	}
	org := organizer.NewOrganizer(poison, &organizer.Config{
		FolderFormat:  "shared",
		FileFormat:    "shared",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	tracker := organizer.NewDuplicateTracker(false)
	tracker.PrimeBatch([]organizer.DuplicatePriming{
		{SourcePath: "/in/A.mkv", TargetPath: w241Target, WillMove: true},
		{SourcePath: "/in/B.mkv", TargetPath: w241Target, WillMove: true},
	})
	ppCmd := func(movieID, src, name string) organizer.OrganizeCmd {
		return organizer.OrganizeCmd{
			Match:            models.FileMatchInfo{MovieID: movieID, Path: src, Name: name, Extension: ".mkv"},
			Movie:            &models.Movie{ID: movieID},
			DestDir:          "/dest",
			MoveFiles:        true,
			ForceUpdate:      true,
			DuplicateTracker: tracker,
		}
	}

	// The owner's cross-device move publishes the destination, then wedges on
	// the verified source cleanup — the typed partial-publish ambiguity.
	resA, errA := org.Organize(ctx, ppCmd("ABC-100", "/in/A.mkv", "A.mkv"))
	require.Error(t, errA)
	require.True(t, poison.removeFailed.Load(), "the wedge really fired at the post-publish source removal")
	require.NotNil(t, resA)
	require.Equal(t, w241Target, filepath.ToSlash(resA.NewPath))
	require.False(t, resA.Moved)

	// The waiter observes the SETTLED claim and keeps its authorized-skip
	// verdict — the pre-fix release would have promoted it onto the published
	// destination, overwriting the owner's bytes.
	resB, errB := org.Organize(ctx, ppCmd("ABC-200", "/in/B.mkv", "B.mkv"))
	require.NoError(t, errB)
	require.True(t, resB.DuplicateSkipped)
	require.False(t, resB.Moved)
	require.Len(t, resB.Warnings, 1)

	opA := w241Begin(t, rl, "ABC-100", "/in/A.mkv")
	opB := w241Begin(t, rl, "ABC-200", "/in/B.mkv")
	require.NoError(t, rl.CompleteFailed(ctx, opA, &ApplyResult{Movie: &models.Movie{ID: "ABC-100"}, OrganizeResult: resA}))
	require.NoError(t, rl.Complete(ctx, opB, &ApplyResult{Movie: &models.Movie{ID: "ABC-200"}, OrganizeResult: resB}))

	// Authored consistently: the failed owner's row still names the shared
	// destination it published (revertable), the skipped waiter's row names
	// no primary move at all.
	rowA := w241Row(t, repo, opA)
	require.Equal(t, w241Target, filepath.ToSlash(rowA.NewPath))
	require.Equal(t, models.RevertStatusFailed, rowA.RevertStatus)
	rowB := w241Row(t, repo, opB)
	require.Empty(t, rowB.NewPath, "the authorized-skip waiter journals no primary-move record")
	require.Equal(t, models.RevertStatusNoOp, rowB.RevertStatus,
		"codex P2 (PR #241 F2): the skip finalizes completed-noop, never reverting against an empty anchor")

	reverter := history.NewReverter(poison, repo)

	// 1) Reverting the WAITER: its completed-noop row (codex P2, PR #241 F2)
	// never enters revert selection — nothing moves anywhere, no outcome, and
	// no anchor "" probe.
	rb, err := reverter.RevertScrape(ctx, "job-w241", "ABC-200")
	require.NoError(t, err)
	assert.Empty(t, rb.Outcomes, "the waiter's revert must never treat the shared destination as movable")
	assert.Zero(t, rb.Total)
	targetBytes, err := afero.ReadFile(base, filepath.FromSlash(w241Target))
	require.NoError(t, err)
	assert.Equal(t, []byte("a-bytes"), targetBytes, "the shared destination still holds the OWNER's published bytes")
	assertFileContent(t, base, "/in/B.mkv", []byte("b-bytes"))

	// 2) Reverting the failed OWNER while its retained source copy still
	// occupies the original path: the move-back refuses the occupied original
	// path — BOTH copies stay byte-intact (the #234 ambiguous-failure
	// invariant), no foreign-byte drag is possible.
	rb, err = reverter.RevertScrape(ctx, "job-w241", "ABC-100")
	require.NoError(t, err)
	require.Len(t, rb.Outcomes, 1)
	assert.NotEqual(t, models.RevertOutcomeReverted, rb.Outcomes[0].Outcome,
		"the revert refuses while the retained source copy occupies the original path")
	targetBytes, err = afero.ReadFile(base, filepath.FromSlash(w241Target))
	require.NoError(t, err)
	assert.Equal(t, []byte("a-bytes"), targetBytes)
	assertFileContent(t, base, "/in/A.mkv", []byte("a-bytes"))
	assertFileContent(t, base, "/in/B.mkv", []byte("b-bytes"))

	// 3) After the operator clears the retained duplicate source copy, the
	// failed owner's revert restores the OWNER'S OWN published bytes onto its
	// own source path — the exact outcome a clean move's revert produces.
	require.NoError(t, base.Remove("/in/A.mkv"))
	rb, err = reverter.RevertScrape(ctx, "job-w241", "ABC-100")
	require.NoError(t, err)
	require.Len(t, rb.Outcomes, 1)
	require.Equal(t, models.RevertOutcomeReverted, rb.Outcomes[0].Outcome)
	assertFileContent(t, base, "/in/A.mkv", []byte("a-bytes"), "only the owner's own bytes return to its own source path")
	exists, err := afero.Exists(base, filepath.FromSlash(w241Target))
	require.NoError(t, err)
	assert.False(t, exists, "the shared destination is vacated by the owner's own revert")
	assertFileContent(t, base, "/in/B.mkv", []byte("b-bytes"), "no one's bytes ever landed on the waiter's source path")
}

func assertFileContent(t *testing.T, fs afero.Fs, path string, want []byte, msgAndArgs ...any) {
	t.Helper()
	got, err := afero.ReadFile(fs, filepath.FromSlash(path))
	require.NoError(t, err)
	assert.Equal(t, want, got, msgAndArgs...)
}
