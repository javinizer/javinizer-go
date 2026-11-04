package database

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The public logger boundary runs after the missing lookup releases its rows.
// It coordinates a real competing repository write without changing SQL,
// query results, or driver errors.
type movieTranslationLookupRendezvous struct {
	logger.Interface
	miss       chan struct{}
	writerDone chan struct{}
	once       sync.Once
	duplicates atomic.Int32
}

func (l *movieTranslationLookupRendezvous) Trace(ctx context.Context, begin time.Time, sql func() (string, int64), err error) {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		l.duplicates.Add(1)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		query, _ := sql()
		if strings.Contains(query, "`movie_translations`") {
			l.once.Do(func() {
				close(l.miss)
				select {
				case <-l.writerDone:
				case <-ctx.Done():
				}
			})
		}
	}
	l.Interface.Trace(ctx, begin, sql, err)
}

// Movie translations have a private repository constructor, so this sibling of
// the external actress/genre contract lives in package database. TranslateError
// remains an explicit caller option, not a change to database.New's defaults.
func TestMovieTranslationDuplicateRecoveryPreservesWinnerWhenUpdatePolicyRejects(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "race.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	require.False(t, db.Config.TranslateError)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	movie := createTestMovie("MOVIE-TRANSLATION-RACE")
	require.NoError(t, NewMovieRepository(db).Create(ctx, movie))
	gate := &movieTranslationLookupRendezvous{Interface: db.Config.Logger, miss: make(chan struct{}), writerDone: make(chan struct{})}
	pool, err := db.DB.DB()
	require.NoError(t, err)
	facade, err := gorm.Open(sqlite.Dialector{Conn: pool}, &gorm.Config{TranslateError: true, Logger: gate, NowFunc: db.Config.NowFunc})
	require.NoError(t, err)
	ordinary := newMovieTranslationRepository(db)
	competing := newMovieTranslationRepository(&DB{DB: facade})
	created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	winner := &models.MovieTranslation{MovieID: movie.ContentID, Language: "en", Title: "Winning title", Description: "Winning description", SourceName: "winner", CreatedAt: created}
	loser := &models.MovieTranslation{MovieID: movie.ContentID, Language: "en", Title: "Replacement title", Description: "Replacement description", SourceName: "loser"}
	require.NoError(t, db.Exec("CREATE TRIGGER reject_movie_translation_replacement BEFORE UPDATE ON movie_translations BEGIN SELECT RAISE(ABORT, 'movie translation replacement policy'); END").Error)
	writerResult := make(chan error, 1)
	var winnerSnapshot *models.MovieTranslation
	go func() {
		defer close(gate.writerDone)
		select {
		case <-gate.miss:
			err := ordinary.Upsert(ctx, winner)
			if err == nil {
				winnerSnapshot, err = ordinary.FindByMovieAndLanguage(ctx, movie.ContentID, "en")
			}
			writerResult <- err
		case <-ctx.Done():
			writerResult <- ctx.Err()
		}
	}()
	t.Cleanup(func() { cancel(); <-gate.writerDone })
	err = competing.Upsert(ctx, loser)
	require.NoError(t, <-writerResult)
	require.EqualValues(t, 1, gate.duplicates.Load(), "losing insert must hit a real translated unique constraint")
	var constraint sqlite3.Error
	require.ErrorAs(t, err, &constraint)
	require.Equal(t, sqlite3.ErrConstraintTrigger, constraint.ExtendedCode)
	require.ErrorContains(t, err, "update translation "+movie.ContentID+"/en")
	require.ErrorContains(t, err, "movie translation replacement policy")
	require.Equal(t, winner.ID, loser.ID)
	require.Equal(t, winner.CreatedAt, loser.CreatedAt)
	stored, err := ordinary.FindByMovieAndLanguage(ctx, movie.ContentID, "en")
	require.NoError(t, err)
	require.Equal(t, winnerSnapshot, stored, "failed recovery must preserve winner fields, identity, and timestamps")
	var count int64
	require.NoError(t, db.Model(&models.MovieTranslation{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Exec("DROP TRIGGER reject_movie_translation_replacement").Error)
	require.NoError(t, competing.Upsert(ctx, loser))
	stored, err = ordinary.FindByMovieAndLanguage(ctx, movie.ContentID, "en")
	require.NoError(t, err)
	require.Equal(t, winner.ID, stored.ID)
	require.Equal(t, winner.CreatedAt, stored.CreatedAt)
	require.Equal(t, loser.Title, stored.Title)
	require.Equal(t, loser.Description, stored.Description)
	require.Equal(t, loser.SourceName, stored.SourceName)
	require.NoError(t, db.Model(&models.MovieTranslation{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.False(t, db.Config.TranslateError, "facade options must not mutate the owning database")
}
