package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPreviewMissingFilesystemReportsConfigurationError(t *testing.T) {
	// The zero-value orchestrator must reject planning rather than panic when
	// neither a filesystem nor a custom strategy resolver was configured.
	orch := &previewOrchImpl{}
	result, err := orch.Execute(t.Context(), PreviewCmd{Movie: &models.Movie{ID: "ABC-001"}})
	require.ErrorContains(t, err, "filesystem not configured")
	require.Nil(t, result)
}

func TestArtifactRehomeLeavesDeferredSiblingUntouched(t *testing.T) {
	fs := afero.NewMemMapFs()
	const source = "/source/movie-pt2.mp4"
	const staged = "/stage/movie-pt2.mp4"
	require.NoError(t, afero.WriteFile(fs, source, []byte("original multipart bytes"), 0o600))
	// Even if a foreign file occupies its staging name, a deferred sibling has
	// no staged payload to rename or publish through the sidecar lane.
	require.NoError(t, afero.WriteFile(fs, staged, []byte("foreign staged occupant"), 0o600))
	stage := &artifactStage{fs: fs, stagedSource: "/stage/movie.mp4", original: ApplyCmd{Organize: OrganizeOptions{LinkMode: organizer.LinkModeNone}}, siblings: []artifactSibling{{sourcePath: source, stagedPath: staged, deferred: true}}}
	require.NoError(t, stage.rehomeRemainingSiblings("/stage/renamed.mp4", nil))
	for path, want := range map[string]string{source: "original multipart bytes", staged: "foreign staged occupant"} {
		got, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		require.Equal(t, want, string(got))
	}
	exists, err := afero.Exists(fs, "/stage/renamed-pt2.mp4")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestArtifactInstallTreeExcludesExplicitVideoPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	const video = "/stage/movie.mp4"
	require.NoError(t, afero.WriteFile(fs, video, []byte("staged video"), 0o600))
	stage := &artifactStage{fs: fs, root: "/stage", finalRoot: "/published"}
	journalCalls := 0
	preserved, err := stage.installTree(video, "", nil, "", "", func([]models.DeleteEntry) error { journalCalls++; return nil })
	require.NoError(t, err)
	require.False(t, preserved)
	require.Zero(t, journalCalls, "excluded video must not acquire a delete intent")
	got, err := afero.ReadFile(fs, video)
	require.NoError(t, err)
	require.Equal(t, "staged video", string(got))
	exists, err := afero.Exists(fs, "/published/movie.mp4")
	require.NoError(t, err)
	require.False(t, exists)
}
