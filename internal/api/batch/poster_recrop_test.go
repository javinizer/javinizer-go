package batch

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestPosterRecropResultConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	result := &resultstore.MovieResult{ErrorCode: downloader.PosterRecropRequiredCode, Error: "poster requires a fresh crop", Status: models.JobStatusFailed, FileMatchInfo: models.FileMatchInfo{Path: path}}
	dbJob, err := jobpersist.Encode(jobpersist.Snapshot{Results: map[string]*resultstore.MovieResult{path: result}})
	require.NoError(t, err)
	snapshot, errs := jobpersist.Decode(dbJob)
	require.Empty(t, errs)
	for _, response := range []any{movieResultToResponse(snapshot.Results[path], nil), movieResultToSlimResponse(snapshot.Results[path], nil)} {
		data, err := json.Marshal(response)
		require.NoError(t, err)
		var wire map[string]any
		require.NoError(t, json.Unmarshal(data, &wire))
		require.Equal(t, downloader.PosterRecropRequiredCode, wire["error_code"])
		require.Equal(t, result.Error, wire["error"])
	}
}
