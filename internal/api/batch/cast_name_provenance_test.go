package batch

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestGetBatchMovieSourcesPreservesCastNameInterpretation(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	raw := &models.ScraperResult{
		Source:             "javlibrary",
		Actresses:          []models.ActressInfo{{FirstName: "Yorimoto", LastName: "Shiori"}},
		CastNameProvenance: []models.CastNameProvenance{{Source: "javlibrary", SourceID: "aa6sc", DisplayName: "Yorimoto Shiori", State: models.CastNameUnknownOrder}},
	}
	job.ResultsWriter().SetProvenance("/path/to/IPX-535.mp4", &resultstore.ProvenanceData{ScraperResults: []*models.ScraperResult{raw}})
	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/batch/"+job.GetID()+"/results/"+resultID+"/sources", nil))
	require.Equal(t, 200, w.Code)
	var response contracts.SourceResultsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Results, 1)
	require.Equal(t, raw.Actresses, response.Results[0].Actresses)
	require.Equal(t, raw.CastNameProvenance, response.Results[0].CastNameProvenance)
	// The source response is metadata only: it does not resolve/verify an actor or
	// replace source components with a reformatted display name.
	require.Zero(t, response.Results[0].Actresses[0].DMMID)
	response.Results[0].CastNameProvenance[0].State = models.CastNameUserCorrected
	again := httptest.NewRecorder()
	router.ServeHTTP(again, httptest.NewRequest("GET", "/batch/"+job.GetID()+"/results/"+resultID+"/sources", nil))
	require.Equal(t, 200, again.Code)
	require.NoError(t, json.Unmarshal(again.Body.Bytes(), &response))
	require.Equal(t, models.CastNameUnknownOrder, response.Results[0].CastNameProvenance[0].State)
}
