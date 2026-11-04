package database

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedUserCreditActress(t *testing.T, db *DB, name string) models.Actress {
	t.Helper()
	actress := models.Actress{FirstName: name, Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	return actress
}

func TestAddUserCreditAdditiveIdempotentAndScrapeResistant(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieCreditRepository(db)
	old := seedUserCreditActress(t, db, "Existing")
	added := seedUserCreditActress(t, db, "Added")
	movie := models.Movie{ContentID: "abc001", ID: "ABC-001", Title: "Test", RenderGeneration: 5}
	require.NoError(t, db.Create(&movie).Error)
	existing := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: old.ID, CreditedName: old.FullName(), Origin: "scrape", OrderIndex: 4}
	require.NoError(t, db.Create(&existing).Error)
	credit, err := repo.AddUserCredit(t.Context(), movie.ID, added.ID)
	require.NoError(t, err)
	require.Equal(t, "user", credit.Origin)
	require.Equal(t, 5, credit.OrderIndex)
	require.Equal(t, added.ID, credit.Actress.ID)
	after := loadArtifactPublicationMovie(t, db, movie.ContentID)
	require.True(t, after.RenderDirty)
	require.Equal(t, int64(6), after.RenderGeneration)
	called := false
	err = artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(t.Context(), movie.ContentID, 5, func(*models.Movie) error { called = true; return nil })
	require.ErrorIs(t, err, ErrApplyPublicationStale)
	require.False(t, called)
	again, err := repo.AddUserCredit(t.Context(), movie.ContentID, added.ID)
	require.NoError(t, err)
	require.Equal(t, credit.ID, again.ID)
	require.Equal(t, int64(6), loadArtifactPublicationMovie(t, db, movie.ContentID).RenderGeneration)
	var unchanged models.MovieCredit
	require.NoError(t, db.First(&unchanged, existing.ID).Error)
	require.Equal(t, existing.ID, unchanged.ID)
	require.Equal(t, existing.Origin, unchanged.Origin)
	require.Equal(t, existing.CreditedName, unchanged.CreditedName)
	require.Equal(t, existing.OrderIndex, unchanged.OrderIndex)
	require.True(t, existing.UpdatedAt.Equal(unchanged.UpdatedAt))
	// A subsequent scraper result omits the explicitly attached actress.
	scraped := creditMovie(movie.ContentID, []models.MovieCredit{{ActressID: old.ID, CreditedName: old.FullName(), Scraped: old}})
	_, err = NewMovieRepository(db).UpsertWithTranslations(t.Context(), scraped, nil, nil)
	require.NoError(t, err)
	retained, err := repo.FindByMovieAndActress(t.Context(), movie.ContentID, added.ID)
	require.NoError(t, err)
	require.Equal(t, credit.ID, retained.ID)
	require.Equal(t, "user", retained.Origin)
	var count int64
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ? AND actress_id = ?", movie.ContentID, added.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestAddUserCreditPreservesOverridesAndRejectsSuppressed(t *testing.T) {
	db := newCreditTestDB(t)
	actress := seedUserCreditActress(t, db, "Restore")
	seedArtifactPublicationMovie(t, db, "restore", 2, false)
	original := models.MovieCredit{MovieContentID: "restore", ActressID: actress.ID, Origin: "scrape", OverrideName: "Custom", UserOverride: true, OrderIndex: 17, OrderPinned: true, DisplayForceCanonical: true, CreditedName: "Reported"}
	require.NoError(t, db.Create(&original).Error)
	result, err := NewMovieCreditRepository(db).AddUserCredit(t.Context(), "restore", actress.ID)
	require.NoError(t, err)
	require.False(t, result.Suppressed)
	require.Equal(t, "user", result.Origin)
	require.Equal(t, original.OverrideName, result.OverrideName)
	require.True(t, result.UserOverride)
	require.True(t, result.OrderPinned)
	require.True(t, result.DisplayForceCanonical)
	require.Equal(t, original.OrderIndex, result.OrderIndex)
	require.Equal(t, original.CreditedName, result.CreditedName)
	// Ownership-only edits leave the rendered output and generation unchanged.
	require.Equal(t, int64(2), loadArtifactPublicationMovie(t, db, "restore").RenderGeneration)
	require.NoError(t, db.Model(&original).Update("suppressed", true).Error)
	result, err = NewMovieCreditRepository(db).AddUserCredit(t.Context(), "restore", actress.ID)
	require.ErrorIs(t, err, ErrMovieCreditSuppressed)
	require.Nil(t, result)
	var suppressed models.MovieCredit
	require.NoError(t, db.First(&suppressed, original.ID).Error)
	require.True(t, suppressed.Suppressed)
}

func TestAddUserCreditValidationAndAmbiguity(t *testing.T) {
	db := newCreditTestDB(t)
	actress := seedUserCreditActress(t, db, "Valid")
	candidate := models.Actress{FirstName: "Candidate"}
	require.NoError(t, db.Create(&candidate).Error)
	for _, movie := range []models.Movie{{ContentID: "one", ID: "DUP"}, {ContentID: "two", ID: "DUP"}, {ContentID: "DUP", ID: "unique"}} {
		require.NoError(t, db.Create(&movie).Error)
	}
	repo := NewMovieCreditRepository(db)
	for _, tc := range []struct {
		name, movie string
		actress     uint
		want        error
	}{
		{"blank", " ", actress.ID, ErrInvalidLookup}, {"zero", "one", 0, ErrInvalidLookup},
		{"missing movie", "absent", actress.ID, ErrNotFound}, {"missing actress", "one", 99999, ErrNotFound},
		{"candidate", "one", candidate.ID, ErrMovieCreditUnverified}, {"cross-field ambiguity", "DUP", actress.ID, ErrMovieCreditAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := repo.AddUserCredit(t.Context(), tc.movie, tc.actress)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, result)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := repo.AddUserCredit(ctx, "one", actress.ID)
	require.ErrorIs(t, err, context.Canceled)
	var count int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestAddUserCreditPreservesLegacyProjection(t *testing.T) {
	db := newCreditTestDB(t)
	old := seedUserCreditActress(t, db, "Legacy")
	added := seedUserCreditActress(t, db, "Explicit")
	seedArtifactPublicationMovie(t, db, "legacy", 1, false)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "legacy", old.ID).Error)
	repo := NewMovieCreditRepository(db)
	_, err := repo.AddUserCredit(t.Context(), "legacy", added.ID)
	require.NoError(t, err)
	credits, err := repo.ListByMovie(t.Context(), "legacy")
	require.NoError(t, err)
	require.Len(t, credits, 2)
	require.Equal(t, old.ID, credits[0].ActressID)
	require.True(t, credits[0].LegacyInferred)
	require.Equal(t, string(models.CreditOriginUser), credits[0].Origin)
	require.True(t, credits[0].RenderVisible())
}

func TestAddUserCreditRollsBackDependentFailures(t *testing.T) {
	for _, failure := range []string{"projection", "render", "quarantine"} {
		t.Run(failure, func(t *testing.T) {
			db := newCreditTestDB(t)
			actress := seedUserCreditActress(t, db, "Rollback")
			seedArtifactPublicationMovie(t, db, "rollback", 3, false)
			switch failure {
			case "projection":
				require.NoError(t, db.Exec("CREATE TRIGGER reject_projection BEFORE INSERT ON movie_actresses BEGIN SELECT RAISE(ABORT, 'projection failure'); END").Error)
			case "render":
				require.NoError(t, db.Exec("CREATE TRIGGER reject_render BEFORE UPDATE OF render_generation ON movies BEGIN SELECT RAISE(ABORT, 'render failure'); END").Error)
			case "quarantine":
				require.NoError(t, db.Exec("DELETE FROM actress_quarantine_state").Error)
			}
			result, err := NewMovieCreditRepository(db).AddUserCredit(t.Context(), "rollback", actress.ID)
			require.Error(t, err)
			require.Nil(t, result)
			var count int64
			require.NoError(t, db.Model(&models.MovieCredit{}).Count(&count).Error)
			require.Zero(t, count)
			require.NoError(t, db.Table("movie_actresses").Count(&count).Error)
			require.Zero(t, count)
			movie := loadArtifactPublicationMovie(t, db, "rollback")
			require.Equal(t, int64(3), movie.RenderGeneration)
			require.False(t, movie.RenderDirty)
		})
	}
}

func TestAddUserCreditSerializesIndependentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credits.db")
	first := lifecycleSyncOpen(t, path)
	defer first.Close()
	second := lifecycleSyncOpen(t, path)
	defer second.Close()
	require.NoError(t, second.Exec("PRAGMA busy_timeout = 0").Error)
	seedArtifactPublicationMovie(t, first, "concurrent", 1, false)
	a := seedUserCreditActress(t, first, "First")
	b := seedUserCreditActress(t, first, "Second")
	admitted, conflict, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var admitOnce, conflictOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	matches := func(tx *gorm.DB) bool {
		return strings.HasPrefix(tx.Statement.SQL.String(), "UPDATE movies SET content_id = content_id WHERE 0")
	}
	require.NoError(t, first.Callback().Raw().After("gorm:raw").Register("hold_credit_writer", func(tx *gorm.DB) {
		if matches(tx) && tx.Error == nil {
			admitOnce.Do(func() { close(admitted); <-release })
		}
	}))
	require.NoError(t, second.Callback().Raw().After("gorm:raw").Register("observe_credit_conflict", func(tx *gorm.DB) {
		if matches(tx) && isLocked(tx.Error) {
			conflictOnce.Do(func() { close(conflict) })
		}
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	results := make(chan error, 2)
	go func() {
		_, err := NewMovieCreditRepository(first).AddUserCredit(ctx, "concurrent", a.ID)
		results <- err
	}()
	select {
	case <-admitted:
	case <-ctx.Done():
		t.Fatal("first writer never acquired write admission")
	}
	go func() {
		_, err := NewMovieCreditRepository(second).AddUserCredit(ctx, "concurrent", b.ID)
		results <- err
	}()
	select {
	case <-conflict:
	case <-ctx.Done():
		t.Fatal("second writer never observed SQLite lock contention")
	}
	unblock()
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	credits, err := NewMovieCreditRepository(first).ListByMovie(t.Context(), "concurrent")
	require.NoError(t, err)
	require.Len(t, credits, 2)
	require.Equal(t, 0, credits[0].OrderIndex)
	require.Equal(t, 1, credits[1].OrderIndex)
	require.Equal(t, int64(3), loadArtifactPublicationMovie(t, first, "concurrent").RenderGeneration)
}
