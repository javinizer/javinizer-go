package batch

// Patch-coverage top-up for prepareAndLaunchApply's post-StartApply-error
// persist leg: the job vanished while the apply was failing, so the persist
// answer is ErrJobGone — logged as a benign race (skip), not an error.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// persistSignalJobStore answers every PersistJobByID with worker.ErrJobGone
// and signals the first call, so the test can wait for the handler's
// fire-and-forget goroutine deterministically.
type persistSignalJobStore struct {
	worker.JobStoreInterface
	once sync.Once
	ch   chan struct{}
}

func (s *persistSignalJobStore) PersistJobByID(string) error {
	s.once.Do(func() { close(s.ch) })
	return worker.ErrJobGone
}

// TestPrepareAndLaunchApply_StartApplyErrorJobGonePersistSkipped pins the A13
// race inside prepareAndLaunchApply's goroutine: StartApply failed, and the
// follow-up PersistJobByID reports the job already deleted — the handler
// must swallow that as benign (no error path, no state to persist) while
// still answering 200 to the client (the apply launch itself succeeded).
func TestPrepareAndLaunchApply_StartApplyErrorJobGonePersistSkipped(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetID().Return("job-any")
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().StartApply(mock.Anything, mock.Anything).Return(errors.New("apply blew up"))

	store := &persistSignalJobStore{JobStoreInterface: deps.JobStore, ch: make(chan struct{})}
	deps.JobStore = store

	rt := testkit.GetTestRuntime(deps)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	prepareAndLaunchApply(c, rt, rt.Snapshot(), mockJob, worker.ApplyPhaseConfig{}, "started")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "started")

	select {
	case <-store.ch:
		// The goroutine ran the failed StartApply persist and hit ErrJobGone.
	case <-time.After(2 * time.Second):
		t.Fatal("the post-StartApply-error persist never ran")
	}
}
