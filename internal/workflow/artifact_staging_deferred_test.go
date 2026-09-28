package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The v1.6.x regression: organize mode copied every source video into a
// hidden .javinizer-apply-* sibling folder even for "move" operations, which
// on a network share doubled the transfer volume of large files. The video
// must now be deferred to the fenced publication (same-volume move = rename),
// while small sidecar siblings stay staged, and in-place modes keep their
// real staged copy for directory-rename rollback.
func TestPrepareArtifactOrganizeDefersVideoCopy(t *testing.T) {
	base, root, source, subtitle, multipart, _, match := pr260FencedFiles(t, "defer-copy")
	dest := filepath.Join(root, "library")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-copy"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
	}

	stage, staged, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	defer stage.cleanup()

	assert.True(t, stage.videoStagingDeferred(), "organize mode defers the video")
	exists, err := afero.Exists(base, stage.stagedSource)
	require.NoError(t, err)
	assert.False(t, exists, "no staged copy of the video payload")
	assert.Equal(t, stage.stagedSource, staged.Match.Path, "pipeline still addresses the virtual staged path")
	assert.Equal(t, source, stage.sourcePath)

	for _, sibling := range []string{subtitle, multipart} {
		exists, serr := afero.Exists(base, filepath.Join(stage.root, ".source", filepath.Base(sibling)))
		require.NoError(t, serr)
		assert.True(t, exists, "small sidecars stay staged: %s", sibling)
	}
	manifestExists, merr := afero.Exists(base, filepath.Join(stage.root, artifactStageManifestName))
	require.NoError(t, merr)
	assert.True(t, manifestExists, "ownership manifest stamped")
}

func TestPrepareArtifactInPlaceKeepsStagedVideoCopy(t *testing.T) {
	base, _, _, _, _, _, match := pr260FencedFiles(t, "defer-inplace")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-inplace"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
		OperationMode:    operationmode.OperationModeInPlace,
	}

	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	defer stage.cleanup()

	assert.False(t, stage.videoStagingDeferred(), "in-place keeps the staged copy")
	exists, err := afero.Exists(base, stage.stagedSource)
	require.NoError(t, err)
	assert.True(t, exists, "in-place stages a real video copy")
}

type noPlanOrganizer struct{}

func (noPlanOrganizer) Organize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	return nil, errors.New("unused")
}

// The deferred organize step requires the planner seam: an organizer without
// PlanOrganize must fail the organize step instead of silently doing nothing.
func TestDeferredOrganizeRequiresPlanningSeam(t *testing.T) {
	base, root, _, _, _, _, match := pr260FencedFiles(t, "defer-noseam")
	dest := filepath.Join(root, "library")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-noseam"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
	}
	_, err := (&applyOrchImpl{fs: base, organizer: noPlanOrganizer{}}).Execute(context.Background(), cmd)
	require.ErrorContains(t, err, "planning seam")
}

// A planning failure in the deferred organize step aborts the pipeline.
func TestDeferredOrganizePlanFailureAborts(t *testing.T) {
	base, root, _, _, _, _, match := pr260FencedFiles(t, "defer-planfault")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, failPlanAt: 1}
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-planfault"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
	}
	_, err := (&applyOrchImpl{fs: base, organizer: fault}).Execute(context.Background(), cmd)
	require.ErrorContains(t, err, "publication plan denied")
}

// In-place still stages a real video copy: a denied staged create must clean
// up the staging root and retain every input.
func TestPrepareArtifactInPlaceStagedCopyFailureRetainsInputs(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "inplace-copy-fault")
	fs := &pr260FiniteCopyFS{Fs: base, op: "staged create", source: source}
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "inplace-copy-fault"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
		OperationMode:    operationmode.OperationModeInPlace,
	}
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.ErrorContains(t, err, "create staged source")
	require.Nil(t, stage)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertStageGone(t, base, root)
}

// The ownership manifest sits inside the staging root: both artifact walks
// must exclude it from publication destinations and installs.
func TestStagingWalksSkipOwnershipManifest(t *testing.T) {
	base := afero.NewMemMapFs()
	root := filepath.FromSlash("/stage")
	final := filepath.FromSlash("/final")
	artifact := filepath.Join(root, "child", "a.txt")
	require.NoError(t, base.MkdirAll(filepath.Dir(artifact), 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(root, artifactStageManifestName), []byte("{}"), 0o600))
	require.NoError(t, afero.WriteFile(base, artifact, []byte("x"), 0o644))
	stage := &artifactStage{fs: base, root: root, finalRoot: final}

	dests, err := stage.treeDestinations("", "", "", "")
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(final, "child", "a.txt")}, dests)

	_, err = stage.installTree("", "", nil, "", "")
	require.NoError(t, err)
	exists, _ := afero.Exists(base, filepath.Join(final, "child", "a.txt"))
	require.True(t, exists)
	manifestLeaked, _ := afero.Exists(base, filepath.Join(final, artifactStageManifestName))
	require.False(t, manifestLeaked, "manifest is never installed into the library")
}
