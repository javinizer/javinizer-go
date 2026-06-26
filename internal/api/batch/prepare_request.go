package batch

import (
	"errors"
	"fmt"
	"io"
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
			// Only an empty/already-consumed body is benign (seam resolution then
			// falls back to defaults). A genuinely malformed payload must NOT be
			// treated as an empty body — return 400 so defaults are not silently
			// applied to an invalid request.
			if !errors.Is(err, io.EOF) {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "invalid request body"})
				return nil, fmt.Errorf("invalid request body: %w", err)
			}
			body = nil
		}
	}

	apiCfg := rt.GetAPIConfig()
	batchCfg := apiCfg.BatchConfig()

	// Extract seam string fields from the generic body map. A present but
	// non-string seam field (e.g. {"operation_mode": 42}) is a client error —
	// reject it with 400 instead of collapsing it to "" and silently falling
	// back to defaults. A genuinely missing field is still allowed to default.
	seamInput, fieldErr := seamStringsFromBody(body)
	if fieldErr != nil {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fieldErr.Error()})
		return nil, fieldErr
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

// seamStringsFromBody extracts the batch seam string fields from a generic
// request body. Missing fields are left as "" so callers can fall back to
// config defaults. A field that is present but not a string is rejected as a
// 400-level client error so a wrong-typed payload (e.g. {"operation_mode": 42})
// cannot silently fall back to defaults.
func seamStringsFromBody(body map[string]any) (workflow.SeamStringsInput, error) {
	stringOrErr := func(key string) (string, error) {
		if body == nil {
			return "", nil
		}
		v, ok := body[key]
		if !ok {
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("field %q must be a string", key)
		}
		return s, nil
	}
	var input workflow.SeamStringsInput
	var err error
	if input.OperationMode, err = stringOrErr("operation_mode"); err != nil {
		return input, err
	}
	if input.LinkMode, err = stringOrErr("link_mode"); err != nil {
		return input, err
	}
	if input.Preset, err = stringOrErr("preset"); err != nil {
		return input, err
	}
	if input.ScalarStrategy, err = stringOrErr("scalar_strategy"); err != nil {
		return input, err
	}
	if input.ArrayStrategy, err = stringOrErr("array_strategy"); err != nil {
		return input, err
	}
	return input, nil
}
