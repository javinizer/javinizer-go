package batch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
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
