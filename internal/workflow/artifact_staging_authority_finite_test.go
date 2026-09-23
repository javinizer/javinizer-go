package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type pr260FiniteAuthorityFence struct {
	pr260FailureArtifactFencer
	authoritative *models.Movie
	result        error
	callbacks     int
}

func (f *pr260FiniteAuthorityFence) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, cb func(*models.Movie) error) error {
	if f.result != nil {
		return f.result
	}
	f.callbacks++
	return cb(f.authoritative)
}
func TestPR260FiniteArtifactPublicationAuthorityAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authoritative *models.Movie
		fenceErr      error
		persisted     bool
		want          string
		publish       bool
		rejected      bool
		callbacks     int
	}{
		{name: "nil authority", want: "authoritative movie changed", callbacks: 1},
		{name: "generation changed", authoritative: &models.Movie{RenderGeneration: 9}, want: "authoritative movie changed", callbacks: 1},
		{name: "stale rejection", fenceErr: database.ErrApplyPublicationStale, want: database.ErrApplyPublicationStale.Error(), rejected: true},
		{name: "persisted missing", fenceErr: database.ErrNotFound, persisted: true, want: "record not found"},
		{name: "legacy missing", fenceErr: database.ErrNotFound, publish: true},
		{name: "matching authority", authoritative: &models.Movie{RenderGeneration: 3}, publish: true, callbacks: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "authority-"+tc.name)
			dest := filepath.Join(root, "published")
			fencer := &pr260FiniteAuthorityFence{authoritative: tc.authoritative, result: tc.fenceErr}
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-authority", RenderGeneration: 3}, match, dest)
			cmd.PublicationFence = fencer
			cmd.PersistedMovie = tc.persisted
			orch := &applyOrchImpl{fs: base}
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			staged := filepath.Join(stage.root, "generated.nfo")
			require.NoError(t, afero.WriteFile(base, staged, []byte("rendered"), 0o644))
			state := &applyPipelineState{nfoPath: staged, downloadPaths: []string{staged}}
			err = stage.publish(context.Background(), orch, state, nil)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.callbacks, fencer.callbacks)
			require.Equal(t, tc.rejected, stage.rejected)
			if tc.rejected {
				require.Empty(t, state.nfoPath)
				require.Empty(t, state.downloadPaths)
			}
			if tc.publish {
				b, e := afero.ReadFile(base, filepath.Join(dest, "generated.nfo"))
				require.NoError(t, e)
				require.Equal(t, "rendered", string(b))
				require.Equal(t, filepath.Join(dest, "generated.nfo"), state.nfoPath)
			} else {
				pr260AssertNoFinals(t, base, dest)
			}
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
		})
	}
}
func TestPR260FiniteArtifactFenceErrorKeepsOwnedStageUntilCleanup(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "authority-error")
	fencer := &pr260FiniteAuthorityFence{result: errors.New("fence transaction denied")}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-error"}, match, filepath.Join(root, "published"))
	cmd.PublicationFence = fencer
	orch := &applyOrchImpl{fs: base}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	staged := filepath.Join(stage.root, "generated.nfo")
	require.NoError(t, afero.WriteFile(base, staged, []byte("rendered"), 0o644))
	err = stage.publish(context.Background(), orch, &applyPipelineState{}, nil)
	require.ErrorContains(t, err, "fence transaction denied")
	b, e := afero.ReadFile(base, staged)
	require.NoError(t, e)
	require.Equal(t, "rendered", string(b))
	stage.cleanup()
	pr260AssertStageGone(t, base, root)
	pr260AssertNoFinals(t, base, filepath.Join(root, "published"))
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}
