package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type callbackPreviewStrategy struct {
	plan       func(models.FileMatchInfo, *models.Movie, string, bool) (*organizer.OrganizePlan, error)
	executions int
}

func (s *callbackPreviewStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, destination string, force bool) (*organizer.OrganizePlan, error) {
	return s.plan(match, movie, destination, force)
}

func (s *callbackPreviewStrategy) Execute(*organizer.OrganizePlan) (*organizer.OrganizeResult, error) {
	s.executions++
	return nil, errors.New("preview must not execute an operation")
}

type countingPreviewNFO struct {
	mockNFOFieldMergerForPreview
	filenameCalls int
}

func (n *countingPreviewNFO) ResolveNFOFilename(movie *models.Movie, cfg nfo.NFONameConfig) string {
	n.filenameCalls++
	return n.mockNFOFieldMergerForPreview.ResolveNFOFilename(movie, cfg)
}

func TestPreviewCancellationStopsPlanningAndMediaPaths(t *testing.T) {
	for _, tc := range []struct {
		name              string
		cancelInResolver  bool
		files             int
		wantPlanningCalls int
	}{
		{name: "before planning", cancelInResolver: true, files: 1},
		{name: "between files", files: 2, wantPlanningCalls: 1},
		{name: "after final plan", files: 1, wantPlanningCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			strategy := &callbackPreviewStrategy{plan: func(match models.FileMatchInfo, _ *models.Movie, _ string, _ bool) (*organizer.OrganizePlan, error) {
				calls++
				cancel() // Synchronous boundary: no sleeps or scheduling assumptions.
				return &organizer.OrganizePlan{SourcePath: match.Path, TargetPath: "/output/TEST-001.mp4", TargetDir: "/output", TargetFile: "TEST-001.mp4"}, nil
			}}
			merger := &countingPreviewNFO{mockNFOFieldMergerForPreview: mockNFOFieldMergerForPreview{nfoFilename: "movie.nfo"}}
			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/synthetic/first.mp4", []byte("synthetic fixture"), 0o600))
			orch := newPreviewOrchestrator(fs, nil, PreviewConfig{
				NFOEnabled: true,
				ResolveStrategy: func(operationmode.OperationMode) organizer.OperationStrategy {
					if tc.cancelInResolver {
						cancel()
					}
					return strategy
				},
			}, nfo.NFONameConfig{}, nil, merger, nil)
			files := []models.FileMatchInfo{{Path: "/synthetic/first.mp4"}, {Path: "/synthetic/second.mp4"}}
			got, err := orch.Execute(ctx, PreviewCmd{Movie: &models.Movie{ID: "TEST-001"}, FileResults: files[:tc.files], OperationMode: operationmode.OperationModeMetadataArtwork})
			require.NoError(t, err)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, tc.wantPlanningCalls, calls)
			require.Equal(t, &PreviewResult{OperationMode: operationmode.OperationModeMetadataArtwork}, got, "cancelled plans must not leak partial video or artwork targets")
			require.Zero(t, merger.filenameCalls, "cancellation blocks downstream NFO planning")
			require.Zero(t, strategy.executions, "preview never performs filesystem operations")
			contents, readErr := afero.ReadFile(fs, "/synthetic/first.mp4")
			require.NoError(t, readErr)
			require.Equal(t, "synthetic fixture", string(contents))
		})
	}
}

func TestPreviewTransientPlanningFailureRetriesExactlyOneEncodedTarget(t *testing.T) {
	const source = `\\source-server\share\original.mp4`
	const destination = `\\target-server\library`
	const target = `\\target-server\library\FALLBACK-001\FALLBACK-001.mp4`
	calls := 0
	strategy := &callbackPreviewStrategy{plan: func(match models.FileMatchInfo, movie *models.Movie, dest string, force bool) (*organizer.OrganizePlan, error) {
		calls++
		require.Equal(t, "//source-server/share/original.mp4", match.Path, "fallback must use the same prepared source as the first attempt")
		require.Equal(t, "//target-server/library", dest)
		require.Equal(t, "FALLBACK-001", movie.ID)
		require.False(t, force)
		if calls == 1 {
			return nil, errors.New("synthetic transient planning failure")
		}
		return &organizer.OrganizePlan{SourcePath: match.Path, TargetDir: dest + "/FALLBACK-001", TargetPath: dest + "/FALLBACK-001/FALLBACK-001.mp4", TargetFile: "FALLBACK-001.mp4", FolderName: "FALLBACK-001", BaseFileName: "FALLBACK-001"}, nil
	}}
	merger := &countingPreviewNFO{mockNFOFieldMergerForPreview: mockNFOFieldMergerForPreview{nfoFilename: "movie.nfo"}}
	orch := newPreviewOrchestrator(afero.NewMemMapFs(), nil, PreviewConfig{NFOEnabled: true, ResolveStrategy: func(operationmode.OperationMode) organizer.OperationStrategy { return strategy }}, nfo.NFONameConfig{}, nil, merger, nil)
	got, err := orch.Execute(t.Context(), PreviewCmd{Movie: &models.Movie{ID: "FALLBACK-001"}, FileResults: []models.FileMatchInfo{{Path: source, Name: "original.mp4", Extension: ".mp4"}}, Destination: destination, OperationMode: operationmode.OperationModeOrganize, SkipDownload: true})
	require.NoError(t, err)
	require.Equal(t, 2, calls, "one initial attempt and one fallback; no extra plan or synthetic target")
	require.Equal(t, []string{target}, got.VideoFiles)
	require.Equal(t, target, got.FullPath)
	require.Equal(t, `\\target-server\library\FALLBACK-001\movie.nfo`, got.NFOPath)
	require.Equal(t, 1, merger.filenameCalls)
	require.Zero(t, strategy.executions)
}
