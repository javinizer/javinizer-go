package organizer

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

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// dupOwnerPublishPoisonFs replays the PR #241 P1 owner failures on a virtual
// filesystem, cross-platform: the owner's same-volume rename of the exact
// (src, dst) pair answers renameErr (EXDEV for the partial-publish wedge —
// forcing the move composite onto its cross-device leg — or a plain I/O
// error for a pre-publication failure). With failCleanup set, the FIRST
// removal of a non-empty object fails once — the post-publish source-cleanup
// wedge (the no-replace lane's bound terminal unlink, or the authorized
// lane's plain post-publish remove): the destination already carried the
// published bytes by then. Empty bookkeeping removals (take-aside
// placeholder release) pass through so the cleanup machinery reaches its
// real unlink step.
type dupOwnerPublishPoisonFs struct {
	afero.Fs
	src, dst     string
	renameErr    error
	failCleanup  bool
	removeFailed atomic.Bool
}

func (p *dupOwnerPublishPoisonFs) Rename(oldname, newname string) error {
	if p.renameErr != nil && filepath.Clean(oldname) == filepath.Clean(p.src) && filepath.Clean(newname) == filepath.Clean(p.dst) {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: p.renameErr}
	}
	return p.Fs.Rename(oldname, newname)
}

func (p *dupOwnerPublishPoisonFs) Remove(name string) error {
	if p.failCleanup {
		if info, err := p.Fs.Stat(name); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			if p.removeFailed.CompareAndSwap(false, true) {
				return &os.PathError{Op: "remove", Path: name, Err: syscall.EPERM}
			}
		}
	}
	return p.Fs.Remove(name)
}

func w241PPFixture(t *testing.T, poison *dupOwnerPublishPoisonFs) (*Organizer, afero.Fs) {
	t.Helper()
	base := afero.NewMemMapFs()
	poison.Fs = base
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(poison, cfg, nil, nil)
	require.NoError(t, base.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/in/B.mkv", []byte("b-bytes"), 0o644))
	return org, base
}

// TestOrganize_PartialPublishOwner_SettlesWaitersVerdicts is the PR #241 P1
// headline regression (codex: "keep claims when execution already published
// the destination"): the primed owner's cross-device move PUBLISHES the
// shared destination but its verified source cleanup fails — an ambiguous
// error with BOTH files byte-intact. The claim must SETTLE, never release:
// the sorted waiter that was blocked mid-observe resolves to its unchanged
// duplicate verdict (duplicate conflict without authorization / authorized
// skip with it), never to a promotion that would let its bytes overwrite the
// already-published owner bytes.
func TestOrganize_PartialPublishOwner_SettlesWaitersVerdicts(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"

	for _, force := range []bool{false, true} {
		mode := "normal mode: waiter duplicate-conflicts"
		if force {
			mode = "force mode: waiter keeps the authorized-skip verdict"
		}
		t.Run(mode, func(t *testing.T) {
			poison := &dupOwnerPublishPoisonFs{
				src:         "/in/A.mkv",
				dst:         filepath.FromSlash(target),
				renameErr:   syscall.EXDEV,
				failCleanup: true,
			}
			org, base := w241PPFixture(t, poison)
			tracker := NewDuplicateTracker(false)
			tracker.PrimeBatch([]DuplicatePriming{
				{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
				{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
			})

			cmdA := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, force, false)
			cmdB := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, force, false)

			type applyOutcome struct {
				res *OrganizeResult
				err error
			}
			bDone := make(chan applyOutcome, 1)
			go func() {
				res, err := org.Organize(context.Background(), cmdB)
				bDone <- applyOutcome{res, err}
			}()
			// The waiter is provably blocked behind the owned key before the
			// owner's ambiguous failure lands.
			waitForWaiter(t, tracker, target, "/in/B.mkv")

			resA, errA := org.Organize(context.Background(), cmdA)
			require.Error(t, errA, "the owner's cleanup wedge surfaces as an apply failure")
			require.True(t, fsutil.PublishCompleted(errA),
				"the failure is the typed partial-publish class — the destination carries the owner's bytes")
			require.NotNil(t, resA)
			assert.False(t, resA.Moved, "no clean move is recorded on the ambiguous leg")

			outB := <-bDone
			if force {
				require.NoError(t, outB.err, "the settled claim keeps the waiter's authorized-skip verdict")
				require.NotNil(t, outB.res)
				assert.True(t, outB.res.DuplicateSkipped)
				assert.False(t, outB.res.Moved, "the waiter must not re-publish over the owner's bytes")
				require.Len(t, outB.res.Warnings, 1)
				assert.Contains(t, outB.res.Warnings[0], "already claimed")
			} else {
				require.Error(t, outB.err, "the settled claim keeps the waiter's duplicate conflict")
				assert.Contains(t, filepath.ToSlash(outB.err.Error()), target)
			}

			content, err := afero.ReadFile(base, filepath.FromSlash(target))
			require.NoError(t, err, "the published destination stands")
			assert.Equal(t, []byte("a-bytes"), content, "target bytes remain the OWNER's — the waiter never overwrote them")
			ownerSrc, err := afero.ReadFile(base, "/in/A.mkv")
			require.NoError(t, err, "the cleanup refusal preserved the owner's source (both kept)")
			assert.Equal(t, []byte("a-bytes"), ownerSrc)
			waiterSrc, err := afero.ReadFile(base, "/in/B.mkv")
			require.NoError(t, err)
			assert.Equal(t, []byte("b-bytes"), waiterSrc, "the waiting claimant's source is untouched")
		})
	}
}

// TestOrganize_PrePublicationFailure_ReleasesWaiterPromotion pins the OTHER
// half of the classification (the pre-#241-P1 behavior that must NOT
// change): an owner whose execute fails BEFORE anything published (no typed
// publish-completed marker) still RELEASES its claim, so the sorted waiter
// promotes and lands its own bytes.
func TestOrganize_PrePublicationFailure_ReleasesWaiterPromotion(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"

	for _, force := range []bool{false, true} {
		mode := "normal mode"
		if force {
			mode = "force mode"
		}
		t.Run(mode, func(t *testing.T) {
			poison := &dupOwnerPublishPoisonFs{
				src:       "/in/A.mkv",
				dst:       filepath.FromSlash(target),
				renameErr: syscall.EIO, // not the cross-device class: nothing is ever published
			}
			org, base := w241PPFixture(t, poison)
			tracker := NewDuplicateTracker(false)
			tracker.PrimeBatch([]DuplicatePriming{
				{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
				{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
			})

			cmdA := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, force, false)
			cmdB := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, force, false)

			type applyOutcome struct {
				res *OrganizeResult
				err error
			}
			bDone := make(chan applyOutcome, 1)
			go func() {
				res, err := org.Organize(context.Background(), cmdB)
				bDone <- applyOutcome{res, err}
			}()
			waitForWaiter(t, tracker, target, "/in/B.mkv")

			_, errA := org.Organize(context.Background(), cmdA)
			require.Error(t, errA)
			require.False(t, fsutil.PublishCompleted(errA),
				"a rename refusal before any publication must NOT masquerade as a partial publish")

			outB := <-bDone
			require.NoError(t, outB.err, "the released claim promotes the waiter, whose organize proceeds")
			require.NotNil(t, outB.res)
			assert.True(t, outB.res.Moved)
			assert.False(t, outB.res.DuplicateSkipped)
			assert.Empty(t, outB.res.Warnings)

			content, err := afero.ReadFile(base, filepath.FromSlash(target))
			require.NoError(t, err)
			assert.Equal(t, []byte("b-bytes"), content, "the promoted claimant landed its bytes")
			ownerSrc, err := afero.ReadFile(base, "/in/A.mkv")
			require.NoError(t, err)
			assert.Equal(t, []byte("a-bytes"), ownerSrc, "the failed owner's source is untouched (nothing published, nothing consumed)")
			_, statErr := base.Stat("/in/B.mkv")
			assert.Error(t, statErr, "the promoted claimant really moved out of its source")
		})
	}
}
