package actressnamesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

type discoveryFailureRepo struct {
	Repository
	failure error
}

func (r *discoveryFailureRepo) DueOfflineNameSyncJobs(context.Context, time.Time, string, int) ([]database.ActressSyncJob, error) {
	return nil, r.failure
}

func TestQueueDiscoveryFailureExposesOnlySafeCurrentCategory(t *testing.T) {
	repo := &discoveryFailureRepo{failure: errors.New("synthetic SQL secret at private path")}
	service, err := New(t.Context(), repo, Options{})
	require.NoError(t, err)
	cursor := "name-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	next, snapshot := service.workPass(t.Context(), cursor, nil)
	require.Equal(t, cursor, next)
	require.Nil(t, snapshot)
	info, err := service.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, "queue_unavailable", info.WorkerProblemCode)
	repo.failure = nil
	next, snapshot = service.workPass(t.Context(), cursor, nil)
	require.Empty(t, next)
	require.Nil(t, snapshot)
	info, err = service.Info(t.Context())
	require.NoError(t, err)
	require.Empty(t, info.WorkerProblemCode)
}
