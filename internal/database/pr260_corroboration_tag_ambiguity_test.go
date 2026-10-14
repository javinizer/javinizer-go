package database

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func corroborationMovie(contentID, source string, dmmID int) *models.Movie {
	movie := creditMovie(contentID, []models.MovieCredit{{
		CreditedName: "Corroborated Alias",
		Source:       source,
		Scraped:      models.Actress{DMMID: dmmID, LastName: "Reported", FirstName: "Name"},
	}})
	movie.CreditPolicy = string(CollisionPolicyAutoAlias)
	return movie
}

func requireCorroborationState(t *testing.T, db *DB, contentID, resolution, sources string, occurrences int) models.CreditCollision {
	t.Helper()
	var collision models.CreditCollision
	require.NoError(t, db.Where("movie_content_id = ? AND field = ? AND reported_value = ?", contentID, models.CreditFieldCreditedName, "Corroborated Alias").Take(&collision).Error)
	require.Equal(t, models.CollisionStatusResolved, collision.Status)
	require.Equal(t, resolution, collision.Resolution)
	if sources != "" {
		require.Equal(t, sources, collision.SourcesSeen)
	}
	require.Equal(t, occurrences, collision.Occurrences)
	return collision
}

func TestMovieRepositoryAutoAliasReevaluatesCorroboratedCollision(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	actress := models.Actress{DMMID: 42601, LastName: "Canonical", FirstName: "Owner", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)

	first := corroborationMovie("corroborated", "sourceA", actress.DMMID)
	first.CreditPolicy = string(CollisionPolicyAutoKeep)
	_, err := repo.Upsert(t.Context(), first)
	require.NoError(t, err)
	requireCorroborationState(t, db, "corroborated", models.CollisionResolutionAutoKeep, "sourceA", 1)

	_, err = repo.Upsert(t.Context(), corroborationMovie("corroborated", "sourceB", actress.DMMID))
	require.NoError(t, err)
	requireCorroborationState(t, db, "corroborated", models.CollisionResolutionAutoAlias, "sourceA,sourceB", 2)

	var alias models.ActressAlias
	require.NoError(t, db.Where("alias_name_key = ?", models.NormalizeActressNameKey("Corroborated Alias")).Take(&alias).Error)
	require.Equal(t, actress.FullName(), alias.CanonicalName)
	var credit models.MovieCredit
	require.NoError(t, db.Where("movie_content_id = ?", "corroborated").Take(&credit).Error)
	require.True(t, credit.DisplayForceCanonical)
}

func TestMovieRepositoryAutoAliasSameSourceDoesNotCorroborate(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	actress := models.Actress{DMMID: 42602, LastName: "Canonical", FirstName: "Owner", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)

	for range 2 {
		_, err := repo.Upsert(t.Context(), corroborationMovie("same-source", "sourceA", actress.DMMID))
		require.NoError(t, err)
	}
	requireCorroborationState(t, db, "same-source", models.CollisionResolutionAutoKeep, "sourceA", 2)
	var count int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Where("alias_name_key = ?", models.NormalizeActressNameKey("Corroborated Alias")).Count(&count).Error)
	require.Zero(t, count)
}

func TestMovieRepositoryAutoAliasConcurrentDistinctSources(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "corroboration.sqlite"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	actress := models.Actress{DMMID: 42603, LastName: "Canonical", FirstName: "Owner", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, source := range []string{"sourceA", "sourceB"} {
		wg.Add(1)
		go func(source string) {
			defer wg.Done()
			<-start
			_, err := NewMovieRepository(db).Upsert(context.Background(), corroborationMovie("concurrent", source, actress.DMMID))
			errs <- err
		}(source)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	collision := requireCorroborationState(t, db, "concurrent", models.CollisionResolutionAutoAlias, "", 2)
	require.Equal(t, 2, collision.DistinctSourceCount())
	var aliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Where("alias_name_key = ?", models.NormalizeActressNameKey("Corroborated Alias")).Count(&aliases).Error)
	require.EqualValues(t, 1, aliases)
}

func TestMovieRepositoryAutoAliasTransitionRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	actress := models.Actress{DMMID: 42604, LastName: "Canonical", FirstName: "Owner", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	_, err := repo.Upsert(t.Context(), corroborationMovie("rollback", "sourceA", actress.DMMID))
	require.NoError(t, err)

	callback := "test:fail-corroborated-alias"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "actress_aliases" {
			_ = tx.AddError(errors.New("alias failure"))
		}
	}))
	_, err = repo.Upsert(t.Context(), corroborationMovie("rollback", "sourceB", actress.DMMID))
	require.Error(t, err)
	require.NoError(t, db.Callback().Create().Remove(callback))

	requireCorroborationState(t, db, "rollback", models.CollisionResolutionAutoKeep, "sourceA", 1)
	var aliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
	require.Zero(t, aliases)
	var credit models.MovieCredit
	require.NoError(t, db.Where("movie_content_id = ?", "rollback").Take(&credit).Error)
	require.Equal(t, "sourceA", credit.Source)
}

func TestMovieRepositoryAutoAliasOwnershipConflictRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	actress := models.Actress{DMMID: 42605, LastName: "Canonical", FirstName: "Owner", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Corroborated Alias", CanonicalName: "Different Owner"}).Error)
	_, err := repo.Upsert(t.Context(), corroborationMovie("ownership-rollback", "sourceA", actress.DMMID))
	require.NoError(t, err)
	_, err = repo.Upsert(t.Context(), corroborationMovie("ownership-rollback", "sourceB", actress.DMMID))
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
	requireCorroborationState(t, db, "ownership-rollback", models.CollisionResolutionAutoKeep, "sourceA", 1)
	var alias models.ActressAlias
	require.NoError(t, db.Where("alias_name_key = ?", models.NormalizeActressNameKey("Corroborated Alias")).Take(&alias).Error)
	require.Equal(t, "Different Owner", alias.CanonicalName)
}

func TestMovieRepositoryAutoAliasDoesNotOverrideProtectedResolutions(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		resolution string
		pinned     bool
	}{
		{"manual", models.CollisionStatusResolved, models.CollisionResolutionKeepIdentity, false},
		{"suppressed", models.CollisionStatusResolved, models.CollisionResolutionBySuppression, false},
		{"pinned", models.CollisionStatusResolved, models.CollisionResolutionAutoKeep, true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewMovieRepository(db)
			actress := models.Actress{DMMID: 42700 + i, LastName: "Canonical", FirstName: "Owner", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&actress).Error)
			contentID := "protected-" + tc.name
			_, err := repo.Upsert(t.Context(), corroborationMovie(contentID, "sourceA", actress.DMMID))
			require.NoError(t, err)
			require.NoError(t, db.Model(&models.CreditCollision{}).Where("movie_content_id = ?", contentID).Updates(map[string]any{
				"status": tc.status, "resolution": tc.resolution, "user_pinned": tc.pinned,
			}).Error)
			_, err = repo.Upsert(t.Context(), corroborationMovie(contentID, "sourceB", actress.DMMID))
			require.NoError(t, err)

			var collision models.CreditCollision
			require.NoError(t, db.Where("movie_content_id = ?", contentID).Take(&collision).Error)
			require.Equal(t, tc.status, collision.Status)
			require.Equal(t, tc.resolution, collision.Resolution)
			require.Equal(t, tc.pinned, collision.UserPinned)
			require.Equal(t, 2, collision.DistinctSourceCount())
			var aliases int64
			require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
			require.Zero(t, aliases)
		})
	}
}

func TestMovieTagExactContentIDDoesNotClaimAmbiguousLegacyRows(t *testing.T) {
	operations := []struct {
		name   string
		seed   func(*DB, models.Movie)
		mutate func(*MovieTagRepository, context.Context, models.Movie) error
		want   []string
	}{
		{"add", func(*DB, models.Movie) {}, func(repo *MovieTagRepository, ctx context.Context, movie models.Movie) error {
			return repo.AddTag(ctx, movie.ContentID, "added")
		}, []string{"added"}},
		{"remove", func(db *DB, movie models.Movie) {
			require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ContentID, Tag: "remove"}).Error)
		}, func(repo *MovieTagRepository, ctx context.Context, movie models.Movie) error {
			return repo.RemoveTag(ctx, movie.ContentID, "remove")
		}, []string{}},
		{"remove-all", func(db *DB, movie models.Movie) {
			require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ContentID, Tag: "remove-all"}).Error)
		}, func(repo *MovieTagRepository, ctx context.Context, movie models.Movie) error {
			return repo.RemoveAllTags(ctx, movie.ContentID)
		}, []string{}},
	}
	for owner := range 2 {
		for _, operation := range operations {
			t.Run(fmt.Sprintf("owner-%d-%s", owner, operation.name), func(t *testing.T) {
				db := newDatabaseTestDB(t)
				movies := []models.Movie{{ContentID: "duplicate-owner-a", ID: "DUPLICATE"}, {ContentID: "duplicate-owner-b", ID: "DUPLICATE"}}
				require.NoError(t, db.Create(&movies).Error)
				require.NoError(t, db.Create(&models.MovieTag{MovieID: "DUPLICATE", Tag: "ambiguous-legacy"}).Error)
				operation.seed(db, movies[owner])
				other := movies[1-owner]
				require.NoError(t, db.Create(&models.MovieTag{MovieID: other.ContentID, Tag: "other-canonical"}).Error)

				repo := NewMovieTagRepository(db)
				require.NoError(t, operation.mutate(repo, t.Context(), movies[owner]))

				var legacy []models.MovieTag
				require.NoError(t, db.Where("movie_id = ?", "DUPLICATE").Find(&legacy).Error)
				require.Len(t, legacy, 1)
				require.Equal(t, "ambiguous-legacy", legacy[0].Tag)
				tags, err := repo.GetTagsForMovie(t.Context(), movies[owner].ContentID)
				require.NoError(t, err)
				require.Equal(t, operation.want, tags)
				require.NotContains(t, tags, "ambiguous-legacy")
				otherTags, err := repo.GetTagsForMovie(t.Context(), other.ContentID)
				require.NoError(t, err)
				require.Equal(t, []string{"other-canonical"}, otherTags)
				_, targetGeneration := tagRenderState(t, db, movies[owner].ContentID)
				_, otherGeneration := tagRenderState(t, db, other.ContentID)
				require.EqualValues(t, 1, targetGeneration)
				require.Zero(t, otherGeneration)
			})
		}
	}
}
