package database

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMergeTranslationTransferRequiresMergedCanonicalSource(t *testing.T) {
	tests := []struct {
		name        string
		target      models.Actress
		source      models.Actress
		sourceName  string
		resolutions map[string]string
		wantRow     bool
	}{
		{
			name:       "target canonical retained invalidates source translation",
			target:     models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			source:     models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			sourceName: "Person Source",
		},
		{
			name:        "source canonical selected transfers source translation",
			target:      models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			source:      models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			sourceName:  "Person Source",
			resolutions: map[string]string{"first_name": MergeResolutionSource},
			wantRow:     true,
		},
		{
			name:       "normalized canonical match transfers source translation",
			target:     models.Actress{FirstName: "ＴＡＲＧＥＴ", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			source:     models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			sourceName: " person   target ",
			wantRow:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			require.NoError(t, db.Create(&test.target).Error)
			require.NoError(t, db.Create(&test.source).Error)
			translation := models.ActressTranslation{ActressID: test.source.ID, Language: "en", DisplayName: "Translated", SourceName: test.sourceName}
			require.NoError(t, db.Create(&translation).Error)

			_, err := repo.Merge(t.Context(), test.target.ID, test.source.ID, test.resolutions)
			require.NoError(t, err)

			var rows []models.ActressTranslation
			require.NoError(t, db.Where("actress_id = ?", test.target.ID).Find(&rows).Error)
			if test.wantRow {
				require.Len(t, rows, 1)
				require.Equal(t, translation.ID, rows[0].ID)
				require.Equal(t, test.sourceName, rows[0].SourceName)
			} else {
				require.Empty(t, rows)
			}
			var total int64
			require.NoError(t, db.Model(&models.ActressTranslation{}).Where("id = ?", translation.ID).Count(&total).Error)
			require.Equal(t, boolToCount(test.wantRow), total)
		})
	}
}

func TestMergeTranslationFreshnessPolicy(t *testing.T) {
	tests := []struct {
		name             string
		targetSourceName *string
		sourceSourceName *string
		want             string
	}{
		{name: "target only stale is deleted", targetSourceName: stringPointer("Person Target")},
		{name: "fresh target takes precedence over fresh source", targetSourceName: stringPointer("Person Source"), sourceSourceName: stringPointer("Person Source"), want: "target"},
		{name: "stale target is replaced by fresh source", targetSourceName: stringPointer("Person Target"), sourceSourceName: stringPointer("Person Source"), want: "source"},
		{name: "both stale are deleted", targetSourceName: stringPointer("Person Target"), sourceSourceName: stringPointer("Other Person")},
		{name: "source only fresh is moved", sourceSourceName: stringPointer("Person Source"), want: "source"},
		{name: "source only stale is deleted", sourceSourceName: stringPointer("Other Person")},
		{name: "empty target provenance is fresh and takes precedence", targetSourceName: stringPointer(""), sourceSourceName: stringPointer("Person Source"), want: "target"},
		{name: "empty source provenance is fresh but target takes precedence", targetSourceName: stringPointer("Person Source"), sourceSourceName: stringPointer(""), want: "target"},
		{name: "source only empty provenance is moved", sourceSourceName: stringPointer(""), want: "source"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			target := models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: ActressOriginUser}
			source := models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)

			var targetTranslation, sourceTranslation models.ActressTranslation
			if test.targetSourceName != nil {
				targetTranslation = models.ActressTranslation{ActressID: target.ID, Language: "en", DisplayName: "Target Translation", SourceName: *test.targetSourceName}
				require.NoError(t, db.Create(&targetTranslation).Error)
			}
			if test.sourceSourceName != nil {
				sourceTranslation = models.ActressTranslation{ActressID: source.ID, Language: "en", DisplayName: "Source Translation", SourceName: *test.sourceSourceName}
				require.NoError(t, db.Create(&sourceTranslation).Error)
			}

			_, err := repo.Merge(t.Context(), target.ID, source.ID, map[string]string{"first_name": MergeResolutionSource, "last_name": MergeResolutionSource})
			require.NoError(t, err)

			var rows []models.ActressTranslation
			require.NoError(t, db.Order("id").Find(&rows).Error)
			if test.want == "" {
				require.Empty(t, rows)
				return
			}
			require.Len(t, rows, 1)
			require.Equal(t, target.ID, rows[0].ActressID)
			if test.want == "target" {
				require.Equal(t, targetTranslation.ID, rows[0].ID)
				require.Equal(t, "Target Translation", rows[0].DisplayName)
			} else {
				require.Equal(t, sourceTranslation.ID, rows[0].ID)
				require.Equal(t, "Source Translation", rows[0].DisplayName)
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}

func boolToCount(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

type promotionCandidateReadContextKey struct{}

func TestPromoteCandidateRetriesReadThenWriterUpgradeConflict(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "promotion-race.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	repo := NewActressRepository(db)
	candidate := models.Actress{FirstName: "Candidate", LastName: "Before", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "promotion-writer-upgrade", ID: "promotion-writer-upgrade", RenderGeneration: 4}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}).Error)

	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), promotionCandidateReadContextKey{}, true), 10*time.Second)
	defer cancel()
	candidateRead := make(chan struct{})
	releasePromotion := make(chan struct{})
	var once sync.Once
	callbackName := "test:pause_promotion_candidate_read"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "actresses" || tx.Statement.Context.Value(promotionCandidateReadContextKey{}) != true {
			return
		}
		loaded, ok := tx.Statement.Dest.(*models.Actress)
		if !ok || loaded.ID != candidate.ID {
			return
		}
		once.Do(func() {
			close(candidateRead)
			select {
			case <-releasePromotion:
			case <-ctx.Done():
			}
		})
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	promotionErr := make(chan error, 1)
	go func() {
		promotionErr <- repo.PromoteCandidate(ctx, candidate.ID, "Stale", "Request", "", "stale-thumb")
	}()
	select {
	case <-candidateRead:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, repo.UpdateCanonicalFields(t.Context(), candidate.ID, "Catalog", "Winner", "勝者", "winner-thumb"))
	close(releasePromotion)
	select {
	case err := <-promotionErr:
		require.ErrorIs(t, err, ErrCandidateAlreadyVerified)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.Equal(t, "Catalog", stored.FirstName)
	require.Equal(t, "Winner", stored.LastName)
	require.Equal(t, "勝者", stored.JapaneseName)
	require.Equal(t, "winner-thumb", stored.ThumbURL)
	require.True(t, stored.Verified)
	var staleAliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Where("alias_name IN ? OR canonical_name IN ?", []string{"Request Stale", "Stale Request"}, []string{"Request Stale", "Stale Request"}).Count(&staleAliases).Error)
	require.Zero(t, staleAliases)
	var projectedIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &projectedIDs).Error)
	require.Equal(t, []uint{candidate.ID}, projectedIDs)
	require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
	require.Equal(t, int64(5), movie.RenderGeneration)
}
