package batch

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// prepareBatchRequest is a shared helper that extracts the common preamble
// from batch API handlers: parse request body, resolve seam strings, fetch
// job from store, and status-check. Returns the resolved job, API config,
// resolved seam strings, and parsed body map. If any step fails, it writes
// an error response to the gin context and returns a non-nil error.
func prepareBatchRequest(deps *core.APIDeps, rt *core.APIRuntime, c *gin.Context, opts ...prepareOption) (worker.BatchJobInterface, error) {
	cfg := defaultPrepareConfig()
	for _, o := range opts {
		o(&cfg)
	}

	jobID := c.Param("id")

	// Parse request body as a generic map for seam string extraction.
	var body map[string]any
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			// Body might have already been consumed by a prior ShouldBindJSON call.
			// This is a best-effort parse for seam resolution fields.
			body = nil
		}
	}

	apiCfg := rt.GetAPIConfig()
	batchCfg := apiCfg.BatchConfig()

	// Extract seam string fields from the generic body map.
	seamInput := workflow.SeamStringsInput{
		OperationMode:  stringField(body, "operation_mode"),
		LinkMode:       stringField(body, "link_mode"),
		Preset:         stringField(body, "preset"),
		ScalarStrategy: stringField(body, "scalar_strategy"),
		ArrayStrategy:  stringField(body, "array_strategy"),
	}
	// Fall back to API config defaults when not specified in request.
	if seamInput.OperationMode == "" {
		seamInput.OperationMode = batchCfg.OperationMode
	}

	_, seamErr := workflow.ResolveSeamStrings(seamInput)
	if seamErr != nil {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: seamErr.Error()})
		return nil, seamErr
	}

	// Fetch job from store.
	job, ok := deps.GetJobStore().GetBatchJob(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	// Status check: reject if job is already running (unless skipped).
	if !cfg.skipRunningCheck && job.GetJobStatus() == models.JobStatusRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "job is already running"})
		return nil, fmt.Errorf("job %s is already running", jobID)
	}

	// Optional status check: require completed status.
	if cfg.requireCompleted {
		status := job.GetStatus()
		if status.Status != models.JobStatusCompleted {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: cfg.completedErrorMessage})
			return nil, fmt.Errorf("job %s not completed", jobID)
		}
	}

	return job, nil
}

// prepareConfig controls optional behavior in prepareBatchRequest.
type prepareConfig struct {
	requireCompleted      bool
	completedErrorMessage string
	skipRunningCheck      bool
}

func defaultPrepareConfig() prepareConfig {
	return prepareConfig{
		completedErrorMessage: "Job must be completed before this operation",
	}
}

// prepareOption is a functional option for prepareBatchRequest.
type prepareOption func(*prepareConfig)

// withSkipRunningCheck configures prepareBatchRequest to skip the running-status
// check. Useful for handlers like cancel that need to operate on running jobs.
func withSkipRunningCheck() prepareOption {
	return func(cfg *prepareConfig) {
		cfg.skipRunningCheck = true
	}
}

// withRequireCompleted configures prepareBatchRequest to reject jobs that
// are not in the completed state.
func withRequireCompleted(msg ...string) prepareOption {
	return func(cfg *prepareConfig) {
		cfg.requireCompleted = true
		if len(msg) > 0 {
			cfg.completedErrorMessage = msg[0]
		}
	}
}

// stringField extracts a string value from a generic map by key.
// Returns "" if the map is nil or the key is missing/not a string.
func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
