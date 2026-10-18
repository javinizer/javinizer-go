package workflow

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

// Round-50 coverage for internal/workflow/artifact_staging.go:855 — the
// deferred-copy observe is bound to the install's identity
// (Codex P1 ntCe6, PR #276). When the observe refuses with something OTHER
// than the typed successor-unproven class, the apply must propagate the
// error wrapped as "bind copy-installed sidecar" rather than swallowing it.

var errBoundObserveInjected = errors.New("injected observe failure")

func TestDeferredCopiedSidecarBoundObserveGenericErrorWraps(t *testing.T) {
	base, root, source, _, _, _, match := pr260FencedFiles(t, "bound-observe-wrap")
	dest := root + "/library"
	real := organizer.NewOrganizer(base, &organizer.Config{
		FolderFormat: "movie", FileFormat: "movie", RenameFile: true,
		OperationMode: operationmode.OperationModeOrganize,
		MoveSubtitles: true, SubtitleExtensions: []string{".srt"},
	}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: real}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "bound-observe-wrap"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	old := observeBoundSidecarPublish
	t.Cleanup(func() { observeBoundSidecarPublish = old })
	observeBoundSidecarPublish = func(b *downloader.ReplacementBatch, destination string, installed *fsutil.BoundInstallIdentity) error {
		return errBoundObserveInjected
	}

	stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.Error(t, publishErr)
	require.True(t, errors.Is(publishErr, errBoundObserveInjected), "the injected refusal unwraps through the wrap; got %v", publishErr)
	assert.Contains(t, publishErr.Error(), "bind copy-installed sidecar")
}
