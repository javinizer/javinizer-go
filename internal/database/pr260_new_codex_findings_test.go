package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPositiveDMMScrapesDoNotClaimVerifiedDMMlessIdentityAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "identity.db")
	open := func() *DB {
		db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
		require.NoError(t, err)
		require.NoError(t, db.RunMigrationsOnStartup(ctx))
		return db
	}

	db := open()
	verified := models.Actress{JapaneseName: "同名", FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&verified).Error)
	firstMovie := creditMovie("DMM-CLAIM-1", []models.MovieCredit{{
		CreditedName: "Same Person",
		Scraped:      models.Actress{DMMID: 111, JapaneseName: "同名", FirstName: "Same", LastName: "Person"},
	}})
	first, err := NewMovieRepository(db).Upsert(ctx, firstMovie)
	require.NoError(t, err)
	require.Len(t, first.Credits, 1)
	firstActressID := first.Credits[0].ActressID
	require.NotEqual(t, verified.ID, firstActressID)
	var candidate models.Actress
	require.NoError(t, db.First(&candidate, firstActressID).Error)
	require.True(t, candidate.AmbiguityQuarantined)
	openCounts, err := NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{first.ContentID})
	require.NoError(t, err)
	require.EqualValues(t, 1, openCounts[first.ContentID])
	require.ErrorIs(t, NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, first.ContentID, first.RenderGeneration, func(*models.Movie) error { return nil }), ErrApplyArtifactPublicationBlocked)
	require.NoError(t, db.Close())

	db = open()
	t.Cleanup(func() { _ = db.Close() })
	sameMovie := creditMovie("DMM-CLAIM-1", []models.MovieCredit{{
		CreditedName: "Same Person",
		Scraped:      models.Actress{DMMID: 111, JapaneseName: "同名", FirstName: "Same", LastName: "Person"},
	}})
	same, err := NewMovieRepository(db).Upsert(ctx, sameMovie)
	require.NoError(t, err)
	require.Equal(t, firstActressID, same.Credits[0].ActressID)

	secondMovie := creditMovie("DMM-CLAIM-2", []models.MovieCredit{{
		CreditedName: "Same Person",
		Scraped:      models.Actress{DMMID: 222, JapaneseName: "同名", FirstName: "Same", LastName: "Person"},
	}})
	second, err := NewMovieRepository(db).Upsert(ctx, secondMovie)
	require.NoError(t, err)
	require.Len(t, second.Credits, 1)
	require.NotEqual(t, verified.ID, second.Credits[0].ActressID)
	require.NotEqual(t, firstActressID, second.Credits[0].ActressID)

	repeatedMovie := creditMovie("DMM-CLAIM-3", []models.MovieCredit{{
		CreditedName: "Same Person",
		Scraped:      models.Actress{DMMID: 111, JapaneseName: "同名", FirstName: "Same", LastName: "Person"},
	}})
	repeated, err := NewMovieRepository(db).Upsert(ctx, repeatedMovie)
	require.NoError(t, err)
	require.Equal(t, firstActressID, repeated.Credits[0].ActressID)
	openCounts, err = NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{same.ContentID, repeated.ContentID})
	require.NoError(t, err)
	require.Zero(t, openCounts[same.ContentID])
	require.Zero(t, openCounts[repeated.ContentID])
	require.Empty(t, same.Actresses)
	require.Empty(t, repeated.Actresses)
	require.NoError(t, NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, repeated.ContentID, repeated.RenderGeneration, func(*models.Movie) error { return nil }))

	require.NoError(t, NewActressRepository(db).PromoteCandidate(ctx, firstActressID, "Resolved", "Person", "", ""))
	openCounts, err = NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{same.ContentID, repeated.ContentID})
	require.NoError(t, err)
	require.Zero(t, openCounts[same.ContentID])
	require.Zero(t, openCounts[repeated.ContentID])
	promoted, err := NewMovieRepository(db).FindByContentID(ctx, repeated.ContentID)
	require.NoError(t, err)
	require.Len(t, promoted.Actresses, 1)
	require.Equal(t, firstActressID, promoted.Actresses[0].ID)

	var verifiedAfter models.Actress
	require.NoError(t, db.First(&verifiedAfter, verified.ID).Error)
	require.Zero(t, verifiedAfter.DMMID)
	require.True(t, verifiedAfter.Verified)
}

func TestActressMergeTransitionsChangedTargetRepresentations(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{JapaneseName: "表示名", FirstName: "Old", LastName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{JapaneseName: "移動元", FirstName: "New", LastName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	alias := models.ActressAlias{AliasName: "Target table alias", CanonicalName: "Target Old"}
	require.NoError(t, db.Create(&alias).Error)

	_, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"first_name": "source", "last_name": "source"})
	require.NoError(t, err)

	for _, oldTarget := range []models.Actress{{FirstName: "Old", LastName: "Target"}, {FirstName: "Target", LastName: "Old"}} {
		found, outcome, err := ResolveActressIdentityTx(db.DB, &oldTarget)
		require.NoError(t, err)
		require.Equal(t, ResolutionMatched, outcome)
		require.Equal(t, target.ID, found.ID)
	}
	require.NoError(t, db.First(&alias, alias.ID).Error)
	require.Equal(t, "表示名", alias.CanonicalName)

	for _, oldSource := range []models.Actress{{JapaneseName: "移動元"}, {FirstName: "New", LastName: "Source"}} {
		sourceFound, sourceOutcome, err := ResolveActressIdentityTx(db.DB, &oldSource)
		require.NoError(t, err)
		require.Equal(t, ResolutionMatched, sourceOutcome)
		require.Equal(t, target.ID, sourceFound.ID)
	}
	var unchangedAliasCount int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Where("alias_name = ?", "表示名").Count(&unchangedAliasCount).Error)
	require.Zero(t, unchangedAliasCount)
}

func TestActressMergeTargetTransitionFailureRollsBackGraph(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{JapaneseName: "表示名", FirstName: "Old", LastName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{JapaneseName: "移動元", FirstName: "New", LastName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_target_alias BEFORE INSERT ON actress_aliases WHEN NEW.alias_name = 'Target Old' BEGIN SELECT RAISE(ABORT, 'injected target alias failure'); END").Error)

	_, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"first_name": "source", "last_name": "source"})
	require.Error(t, err)
	var targetAfter, sourceAfter models.Actress
	require.NoError(t, db.First(&targetAfter, target.ID).Error)
	require.NoError(t, db.First(&sourceAfter, source.ID).Error)
	require.Equal(t, "Old", targetAfter.FirstName)
	require.Equal(t, "Target", targetAfter.LastName)
	require.Equal(t, "New", sourceAfter.FirstName)
	var aliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
	require.Zero(t, aliases)
}

func TestPositiveDMMDoesNotBypassQuarantineThroughDMMlessAlias(t *testing.T) {
	db := newCreditTestDB(t)
	verified := models.Actress{JapaneseName: "Canonical", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&verified).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Known Alias", CanonicalName: "Canonical"}).Error)

	found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 333, JapaneseName: "Known Alias"})
	require.NoError(t, err)
	require.Equal(t, ResolutionAmbiguous, outcome)
	require.NotEqual(t, verified.ID, found.ID)
	require.Equal(t, 333, found.DMMID)
	require.False(t, found.Verified)
}

func TestQuarantinePositiveDMMNameMatchPropagatesLookupFailure(t *testing.T) {
	db := newCreditTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	found, outcome, err := quarantinePositiveDMMNameMatchTx(db.WithContext(ctx), &models.Actress{DMMID: 444, FirstName: "Failed"})
	require.Error(t, err)
	require.Nil(t, found)
	require.Equal(t, ResolutionAmbiguous, outcome)
}

func TestActressMergeRetargetLoadFailureRollsBackTransitions(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{JapaneseName: "表示名", FirstName: "Old", LastName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{JapaneseName: "移動元", FirstName: "New", LastName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, map[string]string{"first_name": "source", "last_name": "source"})
	require.NoError(t, err)
	injectDatabaseCallbackError(t, db, "query", "actresses", 5)

	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.Error(t, err)
	var targetAfter, sourceAfter models.Actress
	require.NoError(t, db.First(&targetAfter, target.ID).Error)
	require.NoError(t, db.First(&sourceAfter, source.ID).Error)
	require.Equal(t, "Old", targetAfter.FirstName)
	require.Equal(t, "New", sourceAfter.FirstName)
	var aliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
	require.Zero(t, aliases)
}
