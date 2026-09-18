package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pr260OpenFailureFs struct {
	afero.Fs
	path string
}

func (f *pr260OpenFailureFs) Open(name string) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.path) {
		return nil, errors.New("pr260: source open denied")
	}
	return f.Fs.Open(name)
}

func (f *pr260OpenFailureFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.path) {
		return nil, errors.New("pr260: source open denied")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type pr260FailureArtifactFencer struct{}

func (pr260FailureArtifactFencer) WithApplyPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(&models.Movie{})
}

func (pr260FailureArtifactFencer) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(&models.Movie{})
}

func TestPR260ArtifactStagingSourceOpenFailureRetainsInputs(t *testing.T) {
	baseFS, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "source-open-failure")
	fs := &pr260OpenFailureFs{Fs: baseFS, path: source}
	dest := filepath.Join(root, "published")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "pr260-source-open-failure"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{MoveFiles: true},
	}

	orch := &applyOrchImpl{fs: fs}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.ErrorContains(t, err, "open artifact source")
	assert.Nil(t, stage)
	pr260AssertNoFinals(t, baseFS, dest)
	pr260AssertRetained(t, baseFS, source, subtitle, multipart, unrelated)
	pr260AssertStageGone(t, baseFS, root)
}

type pr260AllocationFailureFs struct {
	afero.Fs
	parent    string
	failMkdir bool
	failTemp  bool
}

func (f *pr260AllocationFailureFs) MkdirAll(name string, perm os.FileMode) error {
	if f.failMkdir && filepath.Clean(name) == filepath.Clean(f.parent) {
		return errors.New("pr260: staging parent denied")
	}
	return f.Fs.MkdirAll(name, perm)
}

func (f *pr260AllocationFailureFs) Mkdir(name string, perm os.FileMode) error {
	if f.failTemp && strings.HasPrefix(filepath.Base(name), ".javinizer-apply-") {
		return errors.New("pr260: staging directory denied")
	}
	return f.Fs.Mkdir(name, perm)
}

func pr260ArtifactFailureCommand(movie *models.Movie, match models.FileMatchInfo, dest string) ApplyCmd {
	return ApplyCmd{
		Movie:            movie,
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{Skip: true},
		Download:         true,
	}
}

func TestPR260ArtifactStagingAllocationFailuresPreserveInputs(t *testing.T) {
	tests := []struct {
		name      string
		failMkdir bool
		failTemp  bool
		want      string
	}{
		{name: "parent mkdir", failMkdir: true, want: "create artifact staging parent"},
		{name: "temporary directory mkdir", failTemp: true, want: "create artifact staging area"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseFS, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, tt.name)
			fs := &pr260AllocationFailureFs{Fs: baseFS, parent: root, failMkdir: tt.failMkdir, failTemp: tt.failTemp}
			dest := filepath.Join(root, "published")
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-" + tt.name}, match, dest)

			stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, stage)
			pr260AssertNoFinals(t, baseFS, dest)
			pr260AssertRetained(t, baseFS, source, subtitle, multipart, unrelated)
			pr260AssertStageGone(t, baseFS, root)
		})
	}
}

func TestPR260ArtifactStagingRenameFailureRetainsSourceAfterPartialPublish(t *testing.T) {
	baseFS, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "rename-failure")
	dest := filepath.Join(root, "published")
	fs := &pr260RenameFailureFs{Fs: baseFS}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-rename-failure"}, match, dest)
	orch := &applyOrchImpl{fs: fs}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	staged := filepath.Join(stage.root, "generated.nfo")
	require.NoError(t, afero.WriteFile(fs, staged, []byte("generated"), 0o644))

	state := &applyPipelineState{}
	err = stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, err, "publish staged artifact")
	stage.cleanup()
	pr260AssertNoFinals(t, baseFS, dest)
	pr260AssertRetained(t, baseFS, source, subtitle, multipart, unrelated)
	pr260AssertStageGone(t, baseFS, root)
}

type pr260RenameFailureFs struct{ afero.Fs }

func (f *pr260RenameFailureFs) Rename(oldname, newname string) error {
	if strings.Contains(oldname, ".dlbusy") || strings.Contains(newname, ".dlbusy") {
		return f.Fs.Rename(oldname, newname)
	}
	return errors.New("pr260: artifact install rename denied")
}

func TestPR260ArtifactStagingCancelledPublishCleansOwnedStage(t *testing.T) {
	baseFS, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "cancelled-publish")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-cancelled-publish"}, match, dest)
	orch := &applyOrchImpl{fs: baseFS}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	require.NoError(t, afero.WriteFile(baseFS, filepath.Join(stage.root, "generated.nfo"), []byte("generated"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = stage.publish(ctx, orch, &applyPipelineState{}, nil)
	require.ErrorIs(t, err, context.Canceled)
	stage.cleanup()
	pr260AssertNoFinals(t, baseFS, dest)
	pr260AssertRetained(t, baseFS, source, subtitle, multipart, unrelated)
	pr260AssertStageGone(t, baseFS, root)
}

type pr260LegacyPublicationFencer struct{}

func (pr260LegacyPublicationFencer) WithApplyPublicationFence(_ context.Context, _ string, _ int64, _ func(*models.Movie) error) error {
	return nil
}

func TestPR260ArtifactStagingRejectsIncompatiblePublicationCapability(t *testing.T) {
	baseFS, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "legacy-fencer")
	dest := filepath.Join(root, "published")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "pr260-legacy-fencer"},
		PublicationFence: pr260LegacyPublicationFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{Skip: true},
		Download:         true,
	}

	stage, _, err := (&applyOrchImpl{fs: baseFS}).prepareArtifact(context.Background(), cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lacks artifact capability")
	assert.Nil(t, stage)
	pr260AssertNoFinals(t, baseFS, dest)
	pr260AssertRetained(t, baseFS, source, subtitle, multipart, unrelated)
	pr260AssertStageGone(t, baseFS, root)
}
