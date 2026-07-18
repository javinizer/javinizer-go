package batch

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// getExistingNFO godoc
// @Summary Get existing NFO comparison for a batch result
// @Description Lazy-load the existing NFO at the source file's directory, parse it, and return per-field differences against the scraped movie. Returns an empty response (no error) when no NFO is found or parsing fails.
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Success 200 {object} contracts.ExistingNFOResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/existing-nfo [get]
func getExistingNFO(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		result, _, found := lookupResultByResultID(job, resultID)
		if !found {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Result not found in job"})
			return
		}

		scrapedMovie := result.Movie
		if scrapedMovie == nil || scrapedMovie.ID == "" || result.FileMatchInfo.Path == "" {
			c.JSON(http.StatusOK, contracts.ExistingNFOResponse{})
			return
		}

		cfg := deps.CoreDeps.GetConfig()
		nameCfg := nfo.NFONameConfigFromAppConfig(cfg)
		nameCfg.IsMultiPart = result.FileMatchInfo.IsMultiPart
		nameCfg.PartSuffix = result.FileMatchInfo.PartSuffix

		parseResult, _, err := nfo.FindExistingNFO(
			deps.GetFs(),
			filepath.Dir(result.FileMatchInfo.Path),
			scrapedMovie,
			nameCfg,
			result.FileMatchInfo.Path,
			nil,
		)
		if err != nil {
			logging.Debugf("[ExistingNFO] failed to parse NFO for %s: %v", scrapedMovie.ID, err)
			c.JSON(http.StatusOK, contracts.ExistingNFOResponse{})
			return
		}
		if parseResult == nil || parseResult.Movie == nil {
			c.JSON(http.StatusOK, contracts.ExistingNFOResponse{})
			return
		}

		nfoMovie := parseResult.Movie
		diffs := workflow.IdentifyDifferences(nfoMovie, scrapedMovie, scrapedMovie)

		response := contracts.ExistingNFOResponse{
			ExistingNFO: contracts.MovieViewFromModel(nfoMovie),
		}
		if len(diffs) > 0 {
			response.NFODifferences = make([]contracts.FieldDifference, len(diffs))
			for i, d := range diffs {
				response.NFODifferences[i] = contracts.FieldDifference{
					Field:        d.Field,
					NFOValue:     d.NFOValue,
					ScrapedValue: d.ScrapedValue,
					MergedValue:  d.MergedValue,
				}
			}
		}

		c.JSON(http.StatusOK, response)
	}
}
