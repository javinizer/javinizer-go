package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
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
