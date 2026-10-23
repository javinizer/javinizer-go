package batch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type applyReceiptCountingStore struct {
	worker.JobStoreInterface
	job      worker.BatchJobInterface
	persists atomic.Int32
}

func (s *applyReceiptCountingStore) GetBatchJob(string) (worker.BatchJobInterface, bool) {
	return s.job, true
}
func (s *applyReceiptCountingStore) PersistJobByID(string) error { s.persists.Add(1); return nil }

func TestApplyStartResponse_RequestScopedReceipt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct {
		name        string
		handler     func(*core.APIRuntime) gin.HandlerFunc
		body        string
		message     string
		statusReads int
	}{
		{"organize", organizeJob, `{"operation_mode":"in-place","skip_nfo":true,"skip_download":true,"expected_apply_generation":12}`, "Organization started", 3},
		{"update", updateBatchJob, `{"skip_nfo":true,"skip_download":true,"expected_apply_generation":12}`, "Update started", 2},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			deps := createTestDeps(t, &config.Config{}, "")
			rt := testkit.GetTestRuntime(deps)
			job := workermocks.NewMockBatchJobInterface(t)
			job.EXPECT().GetID().Return("receipt-job")
			job.EXPECT().GetJobStatus().Return(models.JobStatusCompleted).Maybe()
			var globalGeneration atomic.Uint64
			globalGeneration.Store(12)
			job.EXPECT().GetStatus().RunAndReturn(func() *worker.BatchJobStatus {
				generation := globalGeneration.Load()
				assert.Equal(t, uint64(12), generation, "status reads must precede admission")
				return &worker.BatchJobStatus{Status: models.JobStatusCompleted, ApplyGeneration: generation}
			})
			job.EXPECT().SetWorkflow(mock.Anything).Return().Once()
			job.EXPECT().StartApply(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, cfg worker.ApplyPhaseConfig) error {
				require.NotNil(t, cfg.ApplyGenerationRef)
				require.NotNil(t, cfg.ExpectedApplyGeneration)
				assert.Equal(t, uint64(12), *cfg.ExpectedApplyGeneration)
				// The request wins generation 41. Before its HTTP response is
				// built, another apply advances the global status to 42. Neither
				// that snapshot nor the pre-request generation + 1 is a receipt.
				atomic.StoreUint64(cfg.ApplyGenerationRef, 41)
				globalGeneration.Store(42)
				return nil
			}).Once()
			job.EXPECT().Wait().Return(nil).Once()
			deps.JobStore = &excludeEdgeStore{JobStoreInterface: deps.JobStore, job: job}
			router := gin.New()
			router.POST("/batch/:id/"+endpoint.name, endpoint.handler(rt))
			req := httptest.NewRequest(http.MethodPost, "/batch/receipt-job/"+endpoint.name, strings.NewReader(endpoint.body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)

			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.JSONEq(t, `{"message":"`+endpoint.message+`","apply_generation":41}`, response.Body.String())
			// Existing clients decoding only message remain compatible.
			var legacy struct {
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &legacy))
			assert.Equal(t, endpoint.message, legacy.Message)
			assert.Equal(t, uint64(42), globalGeneration.Load())
			job.AssertNumberOfCalls(t, "GetStatus", endpoint.statusReads)
			require.True(t, rt.WaitBackgroundTasks(time.Second))
		})
	}
}

func TestApplyStartResponse_WaitsForAdmissionNotCompletion(t *testing.T) {
	deps := createTestDeps(t, &config.Config{}, "")
	rt := testkit.GetTestRuntime(deps)
	job := workermocks.NewMockBatchJobInterface(t)
	job.EXPECT().GetID().Return("receipt-timing-job")
	job.EXPECT().SetWorkflow(mock.Anything).Return().Once()
	admissionEntered := make(chan struct{})
	allowAdmission := make(chan struct{})
	phaseWaiting := make(chan struct{})
	finishPhase := make(chan struct{})
	var releaseAdmission, releasePhase sync.Once
	t.Cleanup(func() {
		releaseAdmission.Do(func() { close(allowAdmission) })
		releasePhase.Do(func() { close(finishPhase) })
	})
	var generation uint64
	c, response := newGinCtx(t)
	job.EXPECT().StartApply(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, cfg worker.ApplyPhaseConfig) error {
		assert.Same(t, &generation, cfg.ApplyGenerationRef)
		close(admissionEntered)
		<-allowAdmission
		assert.False(t, c.Writer.Written(), "no response before admission finishes")
		atomic.StoreUint64(cfg.ApplyGenerationRef, 73)
		return nil
	}).Once()
	job.EXPECT().Wait().RunAndReturn(func() error {
		close(phaseWaiting)
		<-finishPhase
		return nil
	}).Once()
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		prepareAndLaunchApply(c, rt, rt.Snapshot(), job, worker.ApplyPhaseConfig{ApplyGenerationRef: &generation}, "started")
	}()

	select {
	case <-admissionEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("StartApply did not reach admission")
	}
	select {
	case <-handlerDone:
		t.Fatal("HTTP response returned before admission finished")
	default:
	}
	releaseAdmission.Do(func() { close(allowAdmission) })
	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP response waited for long-running apply completion")
	}
	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"message":"started","apply_generation":73}`, response.Body.String())
	select {
	case <-phaseWaiting:
	case <-time.After(3 * time.Second):
		t.Fatal("apply phase was not joined in the background")
	}
	releasePhase.Do(func() { close(finishPhase) })
	require.True(t, rt.WaitBackgroundTasks(time.Second))
}

func TestApplyStartResponse_NoReceiptWithoutSuccessfulAdmission(t *testing.T) {
	for _, tc := range []struct {
		name       string
		withRef    bool
		generation uint64
		startErr   error
	}{
		{name: "nil reference"},
		{name: "canceled admission returns nil", withRef: true},
		{name: "rejected admission", withRef: true, startErr: worker.ErrEditNotAdmitted},
		{name: "canceled admission returns error", withRef: true, startErr: context.Canceled},
		{name: "rejected after generation stamp", withRef: true, generation: 51, startErr: errors.New("phase-entry persistence failed")},
		{name: "stale generation conflict", withRef: true, startErr: errors.Join(errors.New("stale retry"), worker.ErrApplyGenerationMismatch)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := createTestDeps(t, &config.Config{}, "")
			rt := testkit.GetTestRuntime(deps)
			job := workermocks.NewMockBatchJobInterface(t)
			job.EXPECT().GetID().Return("receipt-rejected-job")
			job.EXPECT().SetWorkflow(mock.Anything).Return().Once()
			opts := worker.ApplyPhaseConfig{}
			if tc.withRef {
				opts.ApplyGenerationRef = new(uint64)
			}
			job.EXPECT().StartApply(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, cfg worker.ApplyPhaseConfig) error {
				if cfg.ApplyGenerationRef != nil {
					atomic.StoreUint64(cfg.ApplyGenerationRef, tc.generation)
				}
				return tc.startErr
			}).Once()
			if tc.startErr == nil {
				job.EXPECT().Wait().Return(nil).Once()
			}
			store := &applyReceiptCountingStore{JobStoreInterface: deps.JobStore, job: job}
			deps.JobStore = store
			c, response := newGinCtx(t)
			prepareAndLaunchApply(c, rt, rt.Snapshot(), job, opts, "started")

			var body map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			assert.NotContains(t, body, "apply_generation", "an unproven or rejected admission has no receipt")
			if tc.startErr == nil {
				assert.Equal(t, http.StatusOK, response.Code)
				assert.JSONEq(t, `{"message":"started"}`, response.Body.String())
			} else if errors.Is(tc.startErr, worker.ErrApplyGenerationMismatch) {
				assert.Equal(t, http.StatusConflict, response.Code)
				assert.Equal(t, "APPLY_GENERATION_MISMATCH", body["code"])
				assert.NotContains(t, body, "message")
				job.AssertNotCalled(t, "Wait")
				assert.Zero(t, store.persists.Load(), "stale retries must not persist an unchanged job")
			} else {
				assert.Equal(t, http.StatusInternalServerError, response.Code)
				assert.Equal(t, "APPLY_NOT_STARTED", body["code"])
				assert.NotContains(t, body, "message")
				job.AssertNotCalled(t, "Wait")
			}
			require.True(t, rt.WaitBackgroundTasks(time.Second))
		})
	}
}

func TestApplyStartResponse_ExpectedGenerationConfig(t *testing.T) {
	deps := createTestDeps(t, &config.Config{}, "")
	rt := testkit.GetTestRuntime(deps)
	snap := rt.Snapshot()
	factory := snap.BatchJobFactory()
	job := &stubControlledJob{status: &worker.BatchJobStatus{Status: models.JobStatusCompleted, ApplyGeneration: 9}}
	for _, data := range []string{`{}`, `{"expected_apply_generation":0}`, `{"expected_apply_generation":27}`} {
		t.Run(data, func(t *testing.T) {
			var organizeRequest contracts.OrganizeRequest
			var updateRequest contracts.UpdateRequest
			require.NoError(t, json.Unmarshal([]byte(data), &organizeRequest))
			require.NoError(t, json.Unmarshal([]byte(data), &updateRequest))
			organizeRequest.OperationMode = string(operationmode.OperationModeInPlace)
			organize, err := resolveOrganizeApplyConfig(snap, factory, job, organizeRequest)
			require.NoError(t, err)
			update, err := resolveUpdateApplyConfig(snap, factory, job, updateRequest)
			require.NoError(t, err)
			require.NotNil(t, organize.ExpectedApplyGeneration)
			assert.Equal(t, uint64(9), organize.OrganizeOptions.VideoCollisionGeneration)
			if organizeRequest.ExpectedApplyGeneration == nil {
				assert.Equal(t, uint64(9), *organize.ExpectedApplyGeneration)
			} else {
				assert.Equal(t, *organizeRequest.ExpectedApplyGeneration, *organize.ExpectedApplyGeneration)
			}
			assert.Equal(t, updateRequest.ExpectedApplyGeneration, update.ExpectedApplyGeneration)
			if data == `{}` {
				assert.Nil(t, update.ExpectedApplyGeneration)
			} else {
				require.NotNil(t, organize.ExpectedApplyGeneration)
				require.NotNil(t, update.ExpectedApplyGeneration)
			}
		})
	}
}
