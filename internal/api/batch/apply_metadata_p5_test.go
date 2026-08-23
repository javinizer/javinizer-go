package batch

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/require"
)

// TestApply_MetadataEditDuringApply_StaysAccepted is the explicit D5
// regression: apply owns the write-back merge, so review metadata edits remain
// admitted while the apply phase marker is active.
func TestApply_MetadataEditDuringApply_StaysAccepted(t *testing.T) {
	_, job, router := newEditHardeningRouter(t, &config.Config{})
	job.Controller().SetJobStatus(models.JobStatusRunning)
	job.Lifecycle().SetCurrentPhase(string(worker.JobPhaseApply))

	w := doJSON(t, router, http.MethodPatch,
		fmt.Sprintf("/batch/%s/results/AAA-100", job.GetID()),
		contracts.UpdateMovieRequest{Movie: &contracts.MovieView{ID: "AAA-100", Title: "edited during apply"}})
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	result, err := job.Results().GetMovieResult("/path/to/AAA-100.mp4")
	require.NoError(t, err)
	require.Equal(t, "edited during apply", result.Movie.Title)
}
