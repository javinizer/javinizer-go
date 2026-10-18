package testkit

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

func TestMockRepositoriesCloseOwnedDatabase(t *testing.T) {
	var movies *database.MovieRepository
	var actresses *database.ActressRepository
	t.Run("owner", func(t *testing.T) {
		movies = NewMockMovieRepo(t)
		actresses = NewMockActressRepo(t)
		_, err := movies.FindByID(context.Background(), "missing")
		require.ErrorIs(t, err, database.ErrNotFound)
		_, err = actresses.FindByID(context.Background(), 1)
		require.ErrorIs(t, err, database.ErrNotFound)
	})
	_, err := movies.FindByID(context.Background(), "missing")
	require.ErrorContains(t, err, "database is closed")
	_, err = actresses.FindByID(context.Background(), 1)
	require.ErrorContains(t, err, "database is closed")
}

func TestCleanupServerHubReleasesManualRuntime(t *testing.T) {
	deps := &core.APIDeps{}
	rt := GetTestRuntime(deps)
	ctx := rt.ServerCtx()
	done := rt.TrackBackgroundTask()
	joined := make(chan struct{})
	go func() {
		defer done()
		<-ctx.Done()
		close(joined)
	}()
	CleanupServerHub(t, rt)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	select {
	case <-joined:
	default:
		t.Fatal("cleanup returned before tracked work finished")
	}
	_, exists := runtimeMap.Load(deps)
	require.False(t, exists, "manual runtime must not remain globally reachable")
	CleanupServerHub(t, rt) // idempotent
}

func TestCleanupServerHubPreservesReplacement(t *testing.T) {
	deps := &core.APIDeps{}
	old := core.NewAPIRuntime(deps) // no runtime state is also valid
	replacement := GetTestRuntime(deps)
	CleanupServerHub(t, old)
	stored, exists := runtimeMap.Load(deps)
	require.True(t, exists)
	require.Same(t, replacement, stored)
	CleanupServerHub(t, replacement)
}

func TestCleanupTestRuntimeDoesNotRecreateAssociation(t *testing.T) {
	deps := &core.APIDeps{}
	rt := GetTestRuntime(deps)
	ctx := rt.ServerCtx()
	CleanupTestRuntime(t, deps)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	CleanupTestRuntime(t, deps)
	_, exists := runtimeMap.Load(deps)
	require.False(t, exists)
}
