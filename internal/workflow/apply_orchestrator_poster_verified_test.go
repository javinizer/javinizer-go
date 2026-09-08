package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestStepDownload_PosterVerifiedBranches(t *testing.T) {
	tests := []struct {
		name    string
		results []downloader.DownloadResult
		want    bool
	}{
		{
			name: "installed poster",
			results: []downloader.DownloadResult{{
				Type: downloader.MediaTypePoster, Downloaded: true,
			}},
			want: true,
		},
		{
			name: "non-poster media",
			results: []downloader.DownloadResult{
				{Type: downloader.MediaTypeCover, Downloaded: true},
				{Type: downloader.MediaTypeTrailer, Downloaded: true},
			},
			want: false,
		},
		{
			name: "skipped poster",
			results: []downloader.DownloadResult{{
				Type: downloader.MediaTypePoster, Downloaded: true, Skipped: true,
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impl := &applyOrchImpl{downloader: &stubDownloader{
				outcome: &downloader.DownloadOutcome{
					Results:      tt.results,
					CreatedPaths: []string{"/dest/media"},
				},
			}}
			state := &applyPipelineState{movie: &models.Movie{ID: "TEST-001"}, finalDir: "/dest"}
			steps := stepCompletion{}
			err := impl.stepDownload(context.Background(), ApplyCmd{Download: true}, "", state, &steps)
			require.NoError(t, err)
			require.True(t, steps.Downloaded)
			require.Equal(t, tt.want, steps.PosterVerified)
		})
	}
}
