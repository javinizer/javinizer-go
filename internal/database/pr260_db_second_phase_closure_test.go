package database

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Admission commits before the artifact fence opens its second transaction.
// Inject changes at that exact boundary, not by racing SQLite writers.
func TestPR260ArtifactSecondPhaseAuthority(t *testing.T) {
	for _, mode := range []string{"missing", "stale", "open-collision", "lookup-error", "lock-error", "collision-query-error"} {
		t.Run(mode, func(t *testing.T) {
			db := setupBaseRepoTestDB(t)
			seedArtifactPublicationMovie(t, db, "second-phase", 4, false)
			actress := models.Actress{FirstName: "Boundary", Verified: true}
			require.NoError(t, db.Create(&actress).Error)
			credit := models.MovieCredit{MovieContentID: "second-phase", ActressID: actress.ID, CreditedName: "Boundary"}
			require.NoError(t, db.Create(&credit).Error)
			const hook = "pr260_second_phase_boundary"
			admitted := false
			require.NoError(t, db.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) {
				if admitted || tx.Error != nil || tx.Statement.Table != "movies" || !strings.Contains(tx.Statement.SQL.String(), "render_dirty") {
					return
				}
				admitted = true
				var err error
				switch mode {
				case "missing":
					err = tx.Session(&gorm.Session{NewDB: true}).Exec("DELETE FROM movies WHERE content_id = ?", "second-phase").Error
				case "stale":
					err = tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE movies SET render_generation = 5 WHERE content_id = ?", "second-phase").Error
				case "open-collision":
					err = tx.Session(&gorm.Session{NewDB: true}).Exec("INSERT INTO credit_collisions (credit_id, movie_content_id, status, field) VALUES (?, ?, ?, ?)", credit.ID, "second-phase", models.CollisionStatusOpen, models.CreditFieldCreditedName).Error
				case "collision-query-error":
					err = tx.Session(&gorm.Session{NewDB: true}).Exec("DROP TABLE credit_collisions").Error
				}
				if err != nil {
					tx.AddError(err)
				}
			}))
			t.Cleanup(func() { db.Callback().Update().Remove(hook) })
			const lockHook = "pr260_second_phase_lock_fault"
			if mode == "lock-error" {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(lockHook, func(tx *gorm.DB) {
					if admitted && tx.Statement.Table == "movies" {
						tx.AddError(errors.New("second lock fault"))
					}
				}))
				t.Cleanup(func() { db.Callback().Update().Remove(lockHook) })
			}
			const queryHook = "pr260_second_phase_lookup_fault"
			if mode == "lookup-error" {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register(queryHook, func(tx *gorm.DB) {
					if admitted && tx.Statement.Table == "movies" {
						tx.AddError(errors.New("second lookup fault"))
					}
				}))
				t.Cleanup(func() { db.Callback().Query().Remove(queryHook) })
			}
			called := false
			err := NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), "second-phase", 4, func(*models.Movie) error { called = true; return nil })
			require.True(t, admitted, "first-phase admission must have committed")
			require.False(t, called, "never publish on second-phase authority failure")
			switch mode {
			case "missing":
				require.ErrorIs(t, err, ErrNotFound)
			case "stale":
				require.ErrorIs(t, err, ErrApplyPublicationStale)
			case "open-collision":
				require.ErrorIs(t, err, ErrApplyArtifactPublicationBlocked)
			case "lookup-error":
				require.ErrorContains(t, err, "second lookup fault")
			case "lock-error":
				require.ErrorContains(t, err, "second lock fault")
			case "collision-query-error":
				require.ErrorContains(t, err, "credit_collisions")
			}
			if mode != "missing" {
				if mode == "lookup-error" {
					db.Callback().Query().Remove(queryHook)
				}
				saved := loadArtifactPublicationMovie(t, db, "second-phase")
				require.True(t, saved.RenderDirty, "committed admission cannot be cleared by failed publication")
				if mode == "stale" {
					require.EqualValues(t, 5, saved.RenderGeneration)
				} else {
					require.EqualValues(t, 4, saved.RenderGeneration)
				}
			}
		})
	}
}

func TestPR260ArtifactSecondPhaseCallbackAndCleanFailure(t *testing.T) {
	for _, mode := range []string{"publisher-error", "clean-error", "context-canceled"} {
		t.Run(mode, func(t *testing.T) {
			db, err := New(&Config{
				Type:     "sqlite",
				DSN:      filepath.Join(t.TempDir(), "publication.db"),
				LogLevel: "silent",
			})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
			seedArtifactPublicationMovie(t, db, "second-clean", 6, false)
			if mode == "clean-error" {
				require.NoError(t, db.Exec("CREATE TRIGGER pr260_clean_fault BEFORE UPDATE OF render_dirty ON movies WHEN NEW.render_dirty = 0 BEGIN SELECT RAISE(ABORT, 'second clean fault'); END").Error)
				t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS pr260_clean_fault").Error) })
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			called := 0
			err = NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, "second-clean", 6, func(*models.Movie) error {
				called++
				if mode == "publisher-error" {
					return errors.New("publisher fault")
				}
				if mode == "context-canceled" {
					cancel()
				}
				return nil
			})
			require.Equal(t, 1, called)
			switch mode {
			case "publisher-error":
				require.ErrorContains(t, err, "publisher fault")
			case "clean-error":
				require.ErrorContains(t, err, "second clean fault")
			case "context-canceled":
				require.ErrorIs(t, err, context.Canceled)
			}
			// A new context observes the committed first-phase dirty flag.
			saved := loadArtifactPublicationMovie(t, db, "second-clean")
			require.True(t, saved.RenderDirty)
			require.EqualValues(t, 6, saved.RenderGeneration)
		})
	}
}
