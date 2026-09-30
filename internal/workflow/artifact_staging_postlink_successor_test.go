package workflow

// codex P1 (PR #276, PRRT_kwDORn9KaM6nsX9a) — "do not adopt an unproven
// hard-link successor". The verified hard-link leg detects that another
// writer replaced the fresh install before the post-link proof and returns
// ErrPublishCompleted PLUS ErrPublishSuccessorUnproven with the successor
// deliberately RETAINED. The publish-completed observation must NOT adopt
// that explicitly-unproven occupant as this batch's installed output:
// ObservePublishResult would record the successor's own identity as the
// installed record, and the failed apply's batch rollback would then
// authenticate UnlinkVerified against it and delete the foreign successor
// before restoring any displaced backup. The batch must retain the successor
// byte-intact instead — the sealed retain contract (round 46/47 lineage:
// unproven outcomes retain, round-42's proven publish-completed
// classification still observes).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

// faultPostLinkSuccessorFS replants the link destination inside the link→lstat
// window: the first lstat the verified composite runs AFTER the hard link
// landed (the install exists at the destination name) evicts it and plants a
// foreign successor — a deterministic stand-in for another writer renaming the
// fresh install aside and replanting the name at exactly the post-link proof
// boundary. Pre-link lookups of the still-absent destination pass through
// untouched (classification, no-clobber classification, busy-marker arming).
type faultPostLinkSuccessorFS struct {
	afero.Fs
	target  string
	payload []byte
	armed   bool
	fired   bool
}

func (f *faultPostLinkSuccessorFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.armed && !f.fired && f.target != "" && filepath.Clean(name) == filepath.Clean(f.target) {
		if _, err := f.Fs.Stat(name); err == nil {
			f.fired = true
			if rmErr := f.Fs.Remove(name); rmErr == nil {
				_ = afero.WriteFile(f.Fs, name, f.payload, 0o644)
			}
		}
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// End-to-end: the batch's record-reflection must NOT treat the explicitly
// unproven successor as this batch's installed output. Before the fix the
// ObservePublishResult leg on the PublishCompleted branch recorded the
// successor's identity and the batch rollback's UnlinkVerified deleted the
// foreign bytes; after the fix the successor is retained byte-intact (the
// leg is never marked installed, so rollback's UnlinkVerified never runs
// against its identity) while the error keeps the publish-completed doubt
// class for the existing pending-kind routing.
func TestDeferredHardLinkPostLinkSuccessorRetainedNotAdopted(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "hardlink-postlink-successor")
	dest := filepath.Join(root, "library")
	faultFS := &faultPostLinkSuccessorFS{Fs: base, payload: []byte("foreign successor — another writer's bytes, retained intact")}
	real := organizer.NewOrganizer(faultFS, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, preExecute: func(plan *organizer.OrganizePlan) {
		if filepath.Clean(plan.SourcePath) == filepath.Clean(source) {
			faultFS.target = plan.TargetPath
			faultFS.armed = true
		}
	}}
	orch := &applyOrchImpl{fs: faultFS, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "hardlink-postlink-successor"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Organize.LinkMode = organizer.LinkModeHard
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, real, faultFS, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.Error(t, publishErr)
	require.ErrorIs(t, publishErr, fsutil.ErrPublishCompleted, "the doubt class stays — this operation's own bytes may still stand under another name")
	require.ErrorIs(t, publishErr, fsutil.ErrTakeAsideForeign, "the typed admission refusal rides the surface error")
	require.ErrorIs(t, publishErr, fsutil.ErrPublishSuccessorUnproven, "the explicitly-unproven successor class distinguishes this from a proven install")
	require.True(t, faultFS.fired, "the successor genuinely replanted the destination inside the link→lstat window")
	target := faultFS.target
	require.NotEmpty(t, target, "the deferred video plan carried a real target")
	got, readErr := afero.ReadFile(base, target)
	require.NoError(t, readErr, "rollback must NOT UnlinkVerified the successor: its identity was never recorded as this batch's installed output")
	assert.Equal(t, "foreign successor — another writer's bytes, retained intact", string(got), "the successor is retained byte-intact")
	assert.False(t, stage.sourceCleanupArmed, "the link lane never arms the rename inverse")
	got, readErr = afero.ReadFile(base, source)
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(got), "the admitted source is never consumed by a link install")
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, kept := range []string{subtitle, multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}
