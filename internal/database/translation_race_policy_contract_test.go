package database_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
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

// Trace is a public GORM observation boundary after the first lookup has
// released its rows. It only rendezvous with an independent real repository
// writer; it never injects errors, edits query results, or writes SQL itself.
type translationLookupRendezvous struct {
	logger.Interface
	table      string
	miss       chan struct{}
	writerDone chan struct{}
	once       sync.Once
	duplicates atomic.Int32
}

func (l *translationLookupRendezvous) Trace(ctx context.Context, begin time.Time, sql func() (string, int64), err error) {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		l.duplicates.Add(1)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		query, _ := sql()
		if strings.Contains(query, "`"+l.table+"`") {
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

// TranslateError is opt-in caller composition, not database.New's default.
// The competing write, uniqueness violation and update-policy rejection all
// come from the real SQLite driver; the rendezvous removes scheduling luck.
func TestTranslationDuplicateRecoveryPreservesWinnerWhenUpdatePolicyRejects(t *testing.T) {
	for _, kind := range []string{"actress", "genre"} {
		t.Run(kind, func(t *testing.T) {
			db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "race.db"), LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			require.False(t, db.Config.TranslateError)
			table := kind + "_translations"
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			gate := &translationLookupRendezvous{Interface: db.Config.Logger, table: table, miss: make(chan struct{}), writerDone: make(chan struct{})}
			pool, err := db.DB.DB()
			require.NoError(t, err)
			facade, err := gorm.Open(sqlite.Dialector{Conn: pool}, &gorm.Config{TranslateError: true, Logger: gate, NowFunc: db.Config.NowFunc})
			require.NoError(t, err)
			caller := &database.DB{DB: facade}
			ordinary := db.Repositories()
			competing := caller.Repositories()
			created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
			var writeWinner func() error
			var writeLoser func() error
			var readWinner func() (any, error)
			var assertRecoveredIdentity func()
			var assertSuccessfulRetry func(any)
			switch kind {
			case "actress":
				actor := models.Actress{JapaneseName: "競合契約", Verified: true, Origin: database.ActressOriginUser}
				require.NoError(t, ordinary.ActressRepo.Create(ctx, &actor))
				winner := &models.ActressTranslation{ActressID: actor.ID, Language: "en", DisplayName: "Winning name", SourceName: "winner", CreatedAt: created}
				loser := &models.ActressTranslation{ActressID: actor.ID, Language: " EN ", DisplayName: "Replacement name", SourceName: "loser"}
				writeWinner = func() error { return ordinary.ActressTranslationRepo.Upsert(ctx, winner) }
				writeLoser = func() error { return competing.ActressTranslationRepo.Upsert(ctx, loser) }
				readWinner = func() (any, error) {
					return ordinary.ActressTranslationRepo.FindByActressAndLanguage(ctx, actor.ID, "en")
				}
				assertRecoveredIdentity = func() {
					require.Equal(t, winner.ID, loser.ID)
					require.Equal(t, winner.CreatedAt, loser.CreatedAt)
					require.Equal(t, "en", loser.Language)
				}
				assertSuccessfulRetry = func(value any) {
					stored := value.(*models.ActressTranslation)
					require.Equal(t, winner.ID, stored.ID)
					require.Equal(t, winner.CreatedAt, stored.CreatedAt)
					require.Equal(t, loser.DisplayName, stored.DisplayName)
					require.Equal(t, "loser", stored.SourceName)
				}
			case "genre":
				genre, err := ordinary.GenreRepo.FindOrCreate(ctx, "Race contract")
				require.NoError(t, err)
				winner := &models.GenreTranslation{GenreID: genre.ID, Language: "en", Name: "Winning name", SourceName: "winner", CreatedAt: created}
				loser := &models.GenreTranslation{GenreID: genre.ID, Language: "en", Name: "Replacement name", SourceName: "loser"}
				writeWinner = func() error { return ordinary.GenreTranslationRepo.Upsert(ctx, winner) }
				writeLoser = func() error { return competing.GenreTranslationRepo.Upsert(ctx, loser) }
				readWinner = func() (any, error) { return ordinary.GenreTranslationRepo.FindByGenreAndLanguage(ctx, genre.ID, "en") }
				assertRecoveredIdentity = func() { require.Equal(t, winner.ID, loser.ID); require.Equal(t, winner.CreatedAt, loser.CreatedAt) }
				assertSuccessfulRetry = func(value any) {
					stored := value.(*models.GenreTranslation)
					require.Equal(t, winner.ID, stored.ID)
					require.Equal(t, winner.CreatedAt, stored.CreatedAt)
					require.Equal(t, loser.Name, stored.Name)
					require.Equal(t, "loser", stored.SourceName)
				}
			}
			require.NoError(t, db.Exec("CREATE TRIGGER reject_translation_replacement BEFORE UPDATE ON "+table+" BEGIN SELECT RAISE(ABORT, 'translation replacement policy'); END").Error)
			writerResult := make(chan error, 1)
			var winnerSnapshot any
			go func() {
				defer close(gate.writerDone)
				select {
				case <-gate.miss:
					err := writeWinner()
					if err == nil {
						winnerSnapshot, err = readWinner()
					}
					writerResult <- err
				case <-ctx.Done():
					writerResult <- ctx.Err()
				}
			}()
			t.Cleanup(func() { cancel(); <-gate.writerDone })
			err = writeLoser()
			require.NoError(t, <-writerResult)
			require.EqualValues(t, 1, gate.duplicates.Load(), "the losing insert must have hit a real translated unique constraint")
			var constraint sqlite3.Error
			require.ErrorAs(t, err, &constraint)
			require.Equal(t, sqlite3.ErrConstraintTrigger, constraint.ExtendedCode)
			require.ErrorContains(t, err, "update ")
			require.ErrorContains(t, err, "translation replacement policy")
			assertRecoveredIdentity()
			stored, err := readWinner()
			require.NoError(t, err)
			require.Equal(t, winnerSnapshot, stored, "failed loser update must preserve winner fields, identity and timestamps")
			var count int64
			require.NoError(t, db.Table(table).Count(&count).Error)
			require.EqualValues(t, 1, count)
			require.NoError(t, db.Exec("DROP TRIGGER reject_translation_replacement").Error)
			require.NoError(t, writeLoser())
			stored, err = readWinner()
			require.NoError(t, err)
			assertSuccessfulRetry(stored)
			require.NoError(t, db.Table(table).Count(&count).Error)
			require.EqualValues(t, 1, count)
			require.False(t, db.Config.TranslateError, "facade options must not mutate the owning database")
		})
	}
}
