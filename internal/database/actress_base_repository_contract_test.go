package database_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// The exported BaseRepository supports low-level generic reads. All
// successful writes below still go through the domain
// repository. Only already-cancelled writes exercise the exported base API.
func TestActressEmbeddedBaseRepositoryOwnsFreshEntityQueries(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "base.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repo := database.NewActressRepository(db)
	base := repo.BaseRepository
	count, err := base.Count(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	actor := models.Actress{JapaneseName: "基本契約", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, repo.Create(t.Context(), &actor))
	before, err := repo.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	count, err = base.Count(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	count, err = base.Count(cancelled)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, count)
	require.ErrorIs(t, base.Delete(cancelled, actor.ID), context.Canceled)
	require.ErrorIs(t, base.Create(cancelled, &models.Actress{JapaneseName: "Not stored"}), context.Canceled)
	after, err := repo.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	count, err = base.Count(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "cancelled generic operations must not poison subsequent count queries")

	require.NoError(t, repo.Delete(t.Context(), actor.ID))
	count, err = base.Count(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = repo.FindByID(t.Context(), actor.ID)
	require.True(t, database.IsNotFound(err))
}
