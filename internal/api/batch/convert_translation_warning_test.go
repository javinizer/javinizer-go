package batch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// TestMovieResultToResponse_TranslationWarning proves the full batch result
// carries both translation warning fields from the embedded OrchestrationState.
func TestMovieResultToResponse_TranslationWarning(t *testing.T) {
	warning := "Translation (Google Translate (free)): rate limited - retry later"
	mr := &resultstore.MovieResult{
		ResultID: "r-1",
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/videos/IPX-900.mp4",
			MovieID: "IPX-900",
		},
		Status: models.JobStatusCompleted,
		OrchestrationState: models.OrchestrationState{
			TranslationWarning:     &warning,
			TranslationWarningCode: "rate_limited",
		},
	}

	full := movieResultToResponse(mr, nil)
	require.NotNil(t, full)
	assert.Equal(t, warning, full.TranslationWarning)
	assert.Equal(t, "rate_limited", full.TranslationWarningCode)

	slim := movieResultToSlimResponse(mr, nil)
	require.NotNil(t, slim)
	assert.Equal(t, "rate_limited", slim.TranslationWarningCode,
		"slim payload carries the code for mid-run badges")
}

// TestMovieResultToResponse_TranslationWarningOmitted proves the no-warning
// path serializes without the new fields (omitempty), keeping responses
// unchanged when translation fully succeeded.
func TestMovieResultToResponse_TranslationWarningOmitted(t *testing.T) {
	mr := &resultstore.MovieResult{
		ResultID:           "r-2",
		FileMatchInfo:      models.FileMatchInfo{Path: "/videos/IPX-901.mp4", MovieID: "IPX-901"},
		Status:             models.JobStatusCompleted,
		OrchestrationState: models.OrchestrationState{},
	}

	full := movieResultToResponse(mr, nil)
	require.NotNil(t, full)
	rawFull, err := json.Marshal(full)
	require.NoError(t, err)
	assert.NotContains(t, string(rawFull), "translation_warning")

	slim := movieResultToSlimResponse(mr, nil)
	require.NotNil(t, slim)
	rawSlim, err := json.Marshal(slim)
	require.NoError(t, err)
	assert.NotContains(t, string(rawSlim), "translation_warning")
}

// TestProcessBulkRescrapeMovie_TranslationWarningCodePopulated proves the bulk
// per-result builder carries the worker rescrape outcome's warning code onto
// the API contract (populated case), serialized as translation_warning_code.
func TestProcessBulkRescrapeMovie_TranslationWarningCodePopulated(t *testing.T) {
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(&worker.RescrapeResult{
		Status:                 models.RescrapeStatusSuccess,
		Movie:                  &models.Movie{ID: "MV-W1"},
		TranslationWarningCode: "rate_limited",
	}, nil)

	out, rec := processBulkRescrapeMovie(context.Background(), "MV-W1", mockJob, &contracts.BatchRescrapeRequest{}, minimalFactory{})

	require.Equal(t, models.RescrapeStatusSuccess, out.Status)
	assert.Nil(t, rec)
	assert.Equal(t, "rate_limited", out.TranslationWarningCode)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"translation_warning_code":"rate_limited"`)
}

// TestBulkRescrapeMovieResult_TranslationWarningCodeOmitted pins the omitempty
// wire shape: a clean rescrape does not emit the field.
func TestBulkRescrapeMovieResult_TranslationWarningCodeOmitted(t *testing.T) {
	raw, err := json.Marshal(contracts.BulkRescrapeMovieResult{MovieID: "MV-W2", Status: models.RescrapeStatusSuccess})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "translation_warning_code")
}
