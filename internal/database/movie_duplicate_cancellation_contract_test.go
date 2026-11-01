package database_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This public observation boundary cancels only after SQLite has actually
// rejected an insert. It neither injects DB errors nor rewrites query results.
type cancelRejectedMovieInsert struct {
	logger.Interface
	cancel     context.CancelFunc
	duplicates atomic.Int32
}

func (l *cancelRejectedMovieInsert) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		statement, _ := query()
		if strings.HasPrefix(statement, "INSERT INTO `movies`") {
			l.duplicates.Add(1)
			l.cancel()
		}
	}
	l.Interface.Trace(ctx, begin, query, err)
}

// The extra unique-title index is a caller-owned integrity policy, not the
// default application schema or a claim about ordinary concurrent creation.
// Cancellation after that real constraint failure must preserve durable data.
func TestMovieDuplicateRecoveryCancellationPreservesExistingPolicyWinner(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "policy.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	owner := database.NewMovieRepository(db)
	_, err = owner.Upsert(ctx, &models.Movie{ID: "WIN-001", ContentID: "winner001", Title: "Policy-owned title", Genres: []models.Genre{{Name: "Preserved genre"}}})
	require.NoError(t, err)
	before, err := owner.FindByID(ctx, "WIN-001")
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX contract_movie_title_policy ON movies(title)").Error)

	// Prove the additional policy is enforced by the native driver itself.
	require.False(t, db.Config.TranslateError)
	nativeErr := db.WithContext(ctx).Create(&models.Movie{ID: "PROBE-001", ContentID: "probe001", Title: before.Title}).Error
	var native sqlite3.Error
	require.ErrorAs(t, nativeErr, &native)
	require.Equal(t, sqlite3.ErrConstraintUnique, native.ExtendedCode)

	request, cancel := context.WithCancel(ctx)
	defer cancel()
	observer := &cancelRejectedMovieInsert{Interface: db.Config.Logger, cancel: cancel}
	pool, err := db.DB.DB()
	require.NoError(t, err)
	// A single owned connection also joins asynchronous database/sql rollback
	// before subsequent inspection obtains the connection; no sleeps/polling.
	pool.SetMaxOpenConns(1)
	facade, err := gorm.Open(sqlite.Dialector{Conn: pool}, &gorm.Config{TranslateError: true, Logger: observer, NowFunc: db.Config.NowFunc})
	require.NoError(t, err)
	caller := database.NewMovieRepository(&database.DB{DB: facade})
	rejected := &models.Movie{ID: "NEW-001", ContentID: "new001", Title: before.Title, Genres: []models.Genre{{Name: "Rejected genre"}}}
	result, err := caller.Upsert(request, rejected)
	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "find duplicate")
	// database/sql may finish its cancellation rollback before GORM starts the
	// fallback SELECT. Both documented canceled-transaction outcomes are valid.
	require.True(t, errors.Is(err, context.Canceled) || errors.Is(err, sql.ErrTxDone), "unexpected fallback error: %v", err)
	require.ErrorIs(t, request.Err(), context.Canceled)
	require.EqualValues(t, 1, observer.duplicates.Load())
	after, err := owner.FindByID(ctx, "WIN-001")
	require.NoError(t, err)
	require.Equal(t, before, after, "winner fields, timestamps and associations must remain intact")
	_, err = owner.FindByID(ctx, "NEW-001")
	require.True(t, database.IsNotFound(err))
	for table, want := range map[string]int64{"movies": 1, "genres": 1, "movie_genres": 1} {
		var count int64
		require.NoError(t, db.WithContext(ctx).Table(table).Count(&count).Error)
		require.Equal(t, want, count, table)
	}
	var rejectedGenres int64
	require.NoError(t, db.WithContext(ctx).Model(&models.Genre{}).Where("name = ?", "Rejected genre").Count(&rejectedGenres).Error)
	require.Zero(t, rejectedGenres)

	// The same repository and retained policy remain usable with a live request
	// and a non-conflicting title after the canceled transaction has rolled back.
	saved, err := caller.Upsert(ctx, &models.Movie{ID: "NEW-001", ContentID: "new001", Title: "Allowed distinct title", Genres: []models.Genre{{Name: "Recovered genre"}}})
	require.NoError(t, err)
	require.NotNil(t, saved)
	require.Equal(t, "new001", saved.ContentID)
	require.Equal(t, "Allowed distinct title", saved.Title)
	require.Len(t, saved.Genres, 1)
	require.Equal(t, "Recovered genre", saved.Genres[0].Name)
	require.EqualValues(t, 1, observer.duplicates.Load())
	after, err = owner.FindByID(ctx, "WIN-001")
	require.NoError(t, err)
	require.Equal(t, before, after)
}
