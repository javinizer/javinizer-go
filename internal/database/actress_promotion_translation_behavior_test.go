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

func TestMergeTranslationsFollowSurvivingCanonicalIdentity(t *testing.T) {
	tests := []struct {
		name        string
		target      models.Actress
		source      models.Actress
		resolutions map[string]string
		want        []string
	}{
		{
			name:   "target canonical retains only target translations",
			target: models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			source: models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			want:   []string{"target-en"},
		},
		{
			name:        "source canonical moves all source translations",
			target:      models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			source:      models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			resolutions: map[string]string{colFirstName: MergeResolutionSource, colLastName: MergeResolutionSource},
			want:        []string{"source-en", "source-ja"},
		},
		{
			name:   "same canonical combines with target language precedence",
			target: models.Actress{FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			source: models.Actress{FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			want:   []string{"target-en", "source-ja"},
		},
		{
			name:        "mixed canonical invalidates both owners",
			target:      models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: ActressOriginUser},
			source:      models.Actress{FirstName: "Source", LastName: "Identity", Verified: true, Origin: ActressOriginUser},
			resolutions: map[string]string{colLastName: MergeResolutionSource},
			want:        nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			require.NoError(t, db.Create(&test.target).Error)
			require.NoError(t, db.Create(&test.source).Error)
			translations := []models.ActressTranslation{
				{ActressID: test.target.ID, Language: "en", DisplayName: "target-en", SourceName: "translation:openai"},
				{ActressID: test.source.ID, Language: "en", DisplayName: "source-en", SourceName: "translation:local"},
				{ActressID: test.source.ID, Language: "ja", DisplayName: "source-ja", SourceName: ""},
			}
			for i := range translations {
				require.NoError(t, db.Create(&translations[i]).Error)
			}

			_, err := repo.Merge(t.Context(), test.target.ID, test.source.ID, test.resolutions)
			require.NoError(t, err)
			var rows []models.ActressTranslation
			require.NoError(t, db.Order("language, id").Find(&rows).Error)
			got := make([]string, len(rows))
			for i := range rows {
				require.Equal(t, test.target.ID, rows[i].ActressID)
				got[i] = rows[i].DisplayName
			}
			require.ElementsMatch(t, test.want, got)
			for _, row := range rows {
				if row.DisplayName == "target-en" {
					require.Equal(t, "translation:openai", row.SourceName)
				}
				if row.DisplayName == "source-en" {
					require.Equal(t, "translation:local", row.SourceName)
				}
			}
		})
	}
}

func TestCanonicalTranslationInvalidationRollsBackWithMutation(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	actress := models.Actress{FirstName: "Before", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	translation := models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Before EN", SourceName: "translation:openai"}
	require.NoError(t, db.Create(&translation).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_translation_delete BEFORE DELETE ON actress_translations BEGIN SELECT RAISE(ABORT, 'injected translation delete failure'); END").Error)

	err := repo.UpdateCanonicalFields(t.Context(), actress.ID, "After", "", "", "")
	require.Error(t, err)
	var stored models.Actress
	require.NoError(t, db.First(&stored, actress.ID).Error)
	require.Equal(t, "Before", stored.FirstName)
	require.NoError(t, db.First(&translation, translation.ID).Error)
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

func TestCanonicalTranslationKeyCoversEmptyAndNormalizedIdentity(t *testing.T) {
	require.Empty(t, actressCanonicalTranslationKey(nil))
	require.Empty(t, actressCanonicalTranslationKey(&models.Actress{}))
	left := actressCanonicalTranslationKey(&models.Actress{FirstName: " ＴＡＲＧＥＴ ", LastName: "Person", JapaneseName: " 人 "})
	right := actressCanonicalTranslationKey(&models.Actress{FirstName: "target", LastName: " person ", JapaneseName: "人"})
	require.Equal(t, left, right)
}

func TestUnchangedCanonicalEditPreservesTranslationProvider(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	actress := models.Actress{FirstName: "Same", LastName: "Person", ThumbURL: "before.jpg", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	translation := models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Same EN", SourceName: "translation:openai"}
	require.NoError(t, db.Create(&translation).Error)

	require.NoError(t, repo.UpdateCanonicalFields(t.Context(), actress.ID, actress.FirstName, actress.LastName, actress.JapaneseName, "after.jpg"))
	var stored models.ActressTranslation
	require.NoError(t, db.First(&stored, translation.ID).Error)
	require.Equal(t, "translation:openai", stored.SourceName)
}

func TestMergeTranslationReconciliationDeleteFailuresReturnError(t *testing.T) {
	t.Run("mixed canonical target invalidation", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "delete", "actress_translations", 1)
		require.Error(t, reconcileMergedActressTranslationsTx(db.DB, 1, 2, "target", "source", "mixed"))
	})
	t.Run("equal canonical overlap", func(t *testing.T) {
		db := newCreditTestDB(t)
		target := models.Actress{FirstName: "Same"}
		source := models.Actress{FirstName: "Same"}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&source).Error)
		require.NoError(t, db.Create(&models.ActressTranslation{ActressID: target.ID, Language: "en"}).Error)
		require.NoError(t, db.Create(&models.ActressTranslation{ActressID: source.ID, Language: "en"}).Error)
		injectDatabaseCallbackError(t, db, "delete", "actress_translations", 1)
		require.Error(t, reconcileMergedActressTranslationsTx(db.DB, target.ID, source.ID, "same", "same", "same"))
	})
}

func TestCanonicalMutationTranslationDeleteFailuresRollBack(t *testing.T) {
	tests := []struct {
		name string
		seed func(*testing.T, *DB) (*models.Actress, func(*ActressRepository) error)
	}{
		{
			name: "update",
			seed: func(t *testing.T, db *DB) (*models.Actress, func(*ActressRepository) error) {
				a := &models.Actress{FirstName: "Before", Verified: true, Origin: ActressOriginUser}
				require.NoError(t, db.Create(a).Error)
				return a, func(repo *ActressRepository) error {
					changed := *a
					changed.FirstName = "After"
					return repo.Update(t.Context(), &changed)
				}
			},
		},
		{
			name: "rename",
			seed: func(t *testing.T, db *DB) (*models.Actress, func(*ActressRepository) error) {
				a := &models.Actress{FirstName: "Before", Verified: true, Origin: ActressOriginUser}
				require.NoError(t, db.Create(a).Error)
				return a, func(repo *ActressRepository) error { return repo.RenameNameFields(t.Context(), a.ID, "After", "", "") }
			},
		},
		{
			name: "promote",
			seed: func(t *testing.T, db *DB) (*models.Actress, func(*ActressRepository) error) {
				a := &models.Actress{FirstName: "Before", Origin: ActressOriginScrape}
				require.NoError(t, db.Create(a).Error)
				return a, func(repo *ActressRepository) error {
					return repo.PromoteCandidate(t.Context(), a.ID, "After", "", "", "")
				}
			},
		},
		{
			name: "import",
			seed: func(t *testing.T, db *DB) (*models.Actress, func(*ActressRepository) error) {
				a := &models.Actress{DMMID: 991991, FirstName: "Before", Origin: ActressOriginScrape}
				require.NoError(t, db.Create(a).Error)
				return a, func(repo *ActressRepository) error {
					incoming := &models.Actress{DMMID: a.DMMID, FirstName: "After"}
					return repo.ImportUpsert(t.Context(), incoming)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			actress, mutate := test.seed(t, db)
			translation := models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Before EN", SourceName: "translation:openai"}
			require.NoError(t, db.Create(&translation).Error)
			injectDatabaseCallbackError(t, db, "delete", "actress_translations", 1)
			require.Error(t, mutate(NewActressRepository(db)))
			var stored models.Actress
			require.NoError(t, db.First(&stored, actress.ID).Error)
			require.Equal(t, "Before", stored.FirstName)
			require.NoError(t, db.First(&translation, translation.ID).Error)
		})
	}
}

func TestCollisionCanonicalInvalidationLateFailuresRollBack(t *testing.T) {
	t.Run("reload current identity", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		callbackName := "test:fail_post_adoption_actress_reload"
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actresses" {
				return
			}
			if _, ok := tx.Statement.Dest.(*models.Actress); ok {
				_ = tx.AddError(gorm.ErrInvalidDB)
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
		_, err := service.Resolve(t.Context(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
		require.Error(t, err)
		var stored models.CreditCollision
		require.NoError(t, db.First(&stored, collision.ID).Error)
		require.Equal(t, models.CollisionStatusOpen, stored.Status)
	})
	t.Run("delete translations", func(t *testing.T) {
		db, service, credit, collision := collisionFixture(t)
		translation := models.ActressTranslation{ActressID: credit.ActressID, Language: "en", SourceName: "translation:openai"}
		require.NoError(t, db.Create(&translation).Error)
		injectDatabaseCallbackError(t, db, "delete", "actress_translations", 1)
		_, err := service.Resolve(t.Context(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
		require.Error(t, err)
		require.NoError(t, db.First(&translation, translation.ID).Error)
		var stored models.CreditCollision
		require.NoError(t, db.First(&stored, collision.ID).Error)
		require.Equal(t, models.CollisionStatusOpen, stored.Status)
	})
}
