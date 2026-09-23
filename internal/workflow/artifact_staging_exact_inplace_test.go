package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestPR260ExactSuccessfulClaimSettlement(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-settle")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "exact-settle"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	tracker := organizer.NewDuplicateTracker(false)
	winner := filepath.Join(root, "incoming", "winner.mp4")
	target := filepath.Join(dest, "winner.mp4")
	_, duplicate := tracker.ObserveClaim(context.Background(), winner, target, true)
	require.False(t, duplicate)
	stage.original.Organize.DuplicateTracker = tracker
	stage.duplicatePlan = &organizer.OrganizePlan{SourcePath: winner, TargetPath: target}
	stage.finishDuplicateClaim(nil)
	require.Nil(t, stage.duplicatePlan)
	_, duplicate = tracker.ObserveClaim(context.Background(), source, target, true)
	require.True(t, duplicate, "successful claim remains settled")
	stage.cleanup()
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
	pr260AssertNoFinals(t, base, dest)
}
func TestPR260ExactInPlacePlannedPublication(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "exact-inplace", "")
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-inplace")
	isolatedDir := filepath.Join(root, "only-video")
	require.NoError(t, base.MkdirAll(isolatedDir, 0o755))
	standalone := filepath.Join(isolatedDir, movie.ID+".mp4")
	require.NoError(t, afero.WriteFile(base, standalone, []byte("isolated media"), 0o644))
	match.Path = standalone
	match.Name = filepath.Base(standalone)
	match.MovieID = movie.ID
	orch := pr260RealApply(base, &movie, organizer.MediaFormatConfig{}, nil, false)
	m, matchErr := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, matchErr)
	orch.organizer = organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "renamed-folder", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), m)
	cmd := pr260FencedCommand(&movie, match, isolatedDir, pr260FencedCounter(t, db), operationmode.OperationModeInPlace, false, true, organizer.LinkModeNone, false, false)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, stage.inPlace)
	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource, InPlaceRenamed: true}}
	err = stage.publish(context.Background(), orch, state, nil)
	require.NoError(t, err)
	require.NotNil(t, state.organizeResult)
	require.NotEqual(t, isolatedDir, state.organizeResult.FolderPath)
	existsOld, oldErr := afero.Exists(base, isolatedDir)
	require.NoError(t, oldErr)
	require.False(t, existsOld, "only empty original folder removed")
	require.NotEmpty(t, state.organizeResult.NewPath)
	exists, e := afero.Exists(base, state.organizeResult.NewPath)
	require.NoError(t, e)
	require.True(t, exists)
	pr260AssertRetained(t, base, source, sub, part, other)
	stage.cleanup()
	pr260AssertStageGone(t, base, root)
}
