package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func newCreditTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return db
}

func creditMovie(id string, credits []models.MovieCredit) *models.Movie {
	m := &models.Movie{ContentID: id, ID: id, Title: "Test " + id, Credits: credits}
	for i := range m.Credits {
		m.Credits[i].MovieContentID = id
		m.Credits[i].Origin = string(models.CreditOriginScrape)
	}
	return m
}

func TestUpsertWithCredits_CreatesCandidateAndCredit(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	movie := creditMovie("abc001", []models.MovieCredit{
		{CreditedName: "New Person", Scraped: models.Actress{LastName: "New", FirstName: "Person"}},
	})
	saved, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, saved.Credits)
	require.NotNil(t, saved.Credits[0].Actress)
	assert.False(t, saved.Credits[0].Actress.Verified, "unresolved scrape must land as a quarantined candidate")
	assert.Equal(t, "scrape", saved.Credits[0].Actress.Origin)

	_, err = repo.ActressRepo.FindByID(context.Background(), saved.Credits[0].ActressID)
	require.NoError(t, err)
}

func TestUpsertWithCredits_DMMIDMatchLeavesIdentityUnchanged(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()
	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		DMMID: 777, LastName: "Hatano", FirstName: "Yui", JapaneseName: "波多野結衣", Verified: true, Origin: "user",
	}))

	movie := creditMovie("abc002", []models.MovieCredit{
		{CreditedName: "Hatano Yui", Scraped: models.Actress{DMMID: 777, LastName: "Hatano", FirstName: "Yui"}},
	})
	_, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)

	actress, err := repo.ActressRepo.FindVerifiedByDMMID(context.Background(), 777)
	require.NoError(t, err)
	assert.Equal(t, "波多野結衣", actress.JapaneseName)

	credit, err := repo.MovieCreditRepo.ListByMovie(context.Background(), "abc002")
	require.NoError(t, err)
	require.Len(t, credit, 1)
	assert.Equal(t, actress.ID, credit[0].ActressID)

	open, err := repo.CreditCollisionRepo.ListOpenByMovie(context.Background(), "abc002")
	require.NoError(t, err)
	assert.Empty(t, open, "identical normalized name must not collide")
}

func TestUpsertWithCredits_NameConflictRecordsCollision(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()
	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		DMMID: 888, LastName: "Hatano", FirstName: "Yui", JapaneseName: "波多野結衣", Verified: true, Origin: "user",
	}))

	movie := creditMovie("abc003", []models.MovieCredit{
		{CreditedName: "Totally Different", Scraped: models.Actress{DMMID: 888, LastName: "Totally", FirstName: "Different"}},
	})
	_, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)

	open, err := repo.CreditCollisionRepo.ListOpenByMovie(context.Background(), "abc003")
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, models.CreditFieldCreditedName, open[0].Field)
	assert.Equal(t, "Totally Different", open[0].ReportedValue)
}

func TestUpsertWithCredits_ReconciliationDeletesAbsentScrapeCredits(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	first := creditMovie("abc004", []models.MovieCredit{
		{CreditedName: "Old Cast", Scraped: models.Actress{LastName: "Old", FirstName: "Cast"}},
	})
	_, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), first, nil, nil)
	require.NoError(t, err)

	second := creditMovie("abc004", []models.MovieCredit{
		{CreditedName: "New Cast", Scraped: models.Actress{LastName: "New", FirstName: "Cast"}},
	})
	_, err = repo.MovieRepo.UpsertWithTranslations(context.Background(), second, nil, nil)
	require.NoError(t, err)

	credits, err := repo.MovieCreditRepo.ListByMovie(context.Background(), "abc004")
	require.NoError(t, err)
	require.Len(t, credits, 1)
	assert.Equal(t, "New Cast", credits[0].CreditedName)
}

func TestUpsertWithCredits_OverridePinsMembership(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	first := creditMovie("abc005", []models.MovieCredit{
		{CreditedName: "Overridden", Scraped: models.Actress{LastName: "Over", FirstName: "Ridden"}},
	})
	saved, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), first, nil, nil)
	require.NoError(t, err)
	creditID := saved.Credits[0].ID
	require.NoError(t, repo.MovieCreditRepo.UpdateOverride(context.Background(), creditID, "My Name", true))

	second := creditMovie("abc005", []models.MovieCredit{
		{CreditedName: "Someone Else", Scraped: models.Actress{LastName: "Someone", FirstName: "Else"}},
	})
	_, err = repo.MovieRepo.UpsertWithTranslations(context.Background(), second, nil, nil)
	require.NoError(t, err)

	credits, err := repo.MovieCreditRepo.ListByMovie(context.Background(), "abc005")
	require.NoError(t, err)
	assert.Len(t, credits, 2, "overridden credit absent from new cast must be kept")
}

func TestUpsertWithCredits_SuppressedNotResurrected(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	first := creditMovie("abc006", []models.MovieCredit{
		{CreditedName: "Removed", Scraped: models.Actress{LastName: "Re", FirstName: "Moved"}},
	})
	saved, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), first, nil, nil)
	require.NoError(t, err)
	require.NoError(t, repo.MovieCreditRepo.UpdateSuppressed(context.Background(), saved.Credits[0].ID, true))

	second := creditMovie("abc006", []models.MovieCredit{
		{CreditedName: "Removed", Scraped: models.Actress{LastName: "Re", FirstName: "Moved"}},
	})
	_, err = repo.MovieRepo.UpsertWithTranslations(context.Background(), second, nil, nil)
	require.NoError(t, err)

	credits, err := repo.MovieCreditRepo.ListByMovie(context.Background(), "abc006")
	require.NoError(t, err)
	require.Len(t, credits, 1)
	assert.True(t, credits[0].Suppressed, "suppression tombstone must survive re-scrape of the same cast")
}

func TestUpsertWithCredits_OpenCollisionPinsCredit(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	first := creditMovie("abc007", []models.MovieCredit{
		{CreditedName: "Conflicting", Scraped: models.Actress{DMMID: 999, LastName: "Conflicting", FirstName: "Name"}},
	})
	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		DMMID: 999, LastName: "Real", FirstName: "Person", Verified: true, Origin: "user",
	}))
	_, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), first, nil, nil)
	require.NoError(t, err)
	open, err := repo.CreditCollisionRepo.ListOpenByMovie(context.Background(), "abc007")
	require.NoError(t, err)
	require.Len(t, open, 1)

	second := creditMovie("abc007", []models.MovieCredit{
		{CreditedName: "Someone Else", Scraped: models.Actress{LastName: "Someone", FirstName: "Else"}},
	})
	_, err = repo.MovieRepo.UpsertWithTranslations(context.Background(), second, nil, nil)
	require.NoError(t, err)

	credits, err := repo.MovieCreditRepo.ListByMovie(context.Background(), "abc007")
	require.NoError(t, err)
	assert.Len(t, credits, 2, "open collision must pin its credit from reconciliation deletion")

	stillOpen, err := repo.CreditCollisionRepo.ListOpenByMovie(context.Background(), "abc007")
	require.NoError(t, err)
	assert.NotEmpty(t, stillOpen)
}

func TestUpsertWithCredits_EmptyCastDeletesScrapeCredits(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	first := creditMovie("abc008", []models.MovieCredit{
		{CreditedName: "Gone", Scraped: models.Actress{LastName: "Gone", FirstName: "Soon"}},
	})
	_, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), first, nil, nil)
	require.NoError(t, err)

	second := creditMovie("abc008", []models.MovieCredit{})
	_, err = repo.MovieRepo.UpsertWithTranslations(context.Background(), second, nil, nil)
	require.NoError(t, err)

	credits, err := repo.MovieCreditRepo.ListByMovie(context.Background(), "abc008")
	require.NoError(t, err)
	assert.Empty(t, credits)
}

func TestCollisionPolicyAutoKeep(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()
	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		DMMID: 555, LastName: "Real", FirstName: "Name", Verified: true, Origin: "user",
	}))

	movie := creditMovie("abc009", []models.MovieCredit{
		{CreditedName: "Other Name", Scraped: models.Actress{DMMID: 555, LastName: "Other", FirstName: "Name"}},
	})
	movie.CreditPolicy = "auto_keep"
	_, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)

	open, err := repo.CreditCollisionRepo.ListOpenByMovie(context.Background(), "abc009")
	require.NoError(t, err)
	assert.Empty(t, open, "auto_keep must auto-resolve field collisions")

	credits, err := repo.MovieCreditRepo.ListByMovie(context.Background(), "abc009")
	require.NoError(t, err)
	require.Len(t, credits, 1)
	assert.True(t, credits[0].DisplayForceCanonical)
}

func TestCollisionPolicyAutoAliasCorroboration(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()
	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		DMMID: 444, LastName: "Real", FirstName: "Name", Verified: true, Origin: "user",
	}))

	single := creditMovie("abc010", []models.MovieCredit{
		{CreditedName: "Variant Name", Source: "javdb", Scraped: models.Actress{DMMID: 444, LastName: "Variant", FirstName: "Name"}},
	})
	single.CreditPolicy = "auto_alias"
	_, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), single, nil, nil)
	require.NoError(t, err)

	aliases, err := repo.ActressAliasRepo.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, aliases, "single untrusted source must not auto-alias")

	second := creditMovie("abc011", []models.MovieCredit{
		{CreditedName: "Variant Two", Source: "dmm", Scraped: models.Actress{DMMID: 444, LastName: "Variant", FirstName: "Two"}},
	})
	second.CreditPolicy = "auto_alias"
	second.TrustedCollisionSources = []string{"dmm"}
	_, err = repo.MovieRepo.UpsertWithTranslations(context.Background(), second, nil, nil)
	require.NoError(t, err)

	aliases, err = repo.ActressAliasRepo.List(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, aliases, "trusted source must auto-alias")
}

func TestResolutionAmbiguousHomonymReusesCandidate(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()
	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		JapaneseName: "双子", FirstName: "A", Verified: true, Origin: "user",
	}))
	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		JapaneseName: "双子", FirstName: "B", Verified: true, Origin: "user",
	}))

	first := creditMovie("abc012", []models.MovieCredit{
		{CreditedName: "双子", Scraped: models.Actress{JapaneseName: "双子"}},
	})
	_, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), first, nil, nil)
	require.NoError(t, err)

	candidates, err := repo.ActressRepo.ListCandidates(context.Background(), 100, 0)
	require.NoError(t, err)
	require.NotEmpty(t, candidates)

	second := creditMovie("abc013", []models.MovieCredit{
		{CreditedName: "双子", Scraped: models.Actress{JapaneseName: "双子"}},
	})
	_, err = repo.MovieRepo.UpsertWithTranslations(context.Background(), second, nil, nil)
	require.NoError(t, err)

	candidatesAfter, err := repo.ActressRepo.ListCandidates(context.Background(), 100, 0)
	require.NoError(t, err)
	assert.Len(t, candidatesAfter, len(candidates), "ambiguous scrapes must reuse the same candidate")

	collisions, err := repo.CreditCollisionRepo.ListOpenByMovie(context.Background(), "abc013")
	require.NoError(t, err)
	require.Len(t, collisions, 1)
	assert.Equal(t, models.CreditFieldIdentityLink, collisions[0].Field)
}

func TestSingleNameMatchDoesNotResolveToVerified(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()
	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		FirstName: "Aoi", LastName: "Sola", Verified: true, Origin: "user",
	}))

	movie := creditMovie("abc014", []models.MovieCredit{
		{CreditedName: "Aoi", Scraped: models.Actress{FirstName: "Aoi"}},
	})
	saved, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)

	require.Len(t, saved.Credits, 1)
	assert.False(t, saved.Credits[0].Actress.Verified, "single-name match must not link to a verified identity")
}

func TestPromoteCandidateMarksCreditingMoviesDirty(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	movie := creditMovie("abc015", []models.MovieCredit{
		{CreditedName: "Promote Me", Scraped: models.Actress{LastName: "Promote", FirstName: "Me"}},
	})
	saved, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	actressID := saved.Credits[0].ActressID

	require.NoError(t, repo.ActressRepo.PromoteCandidate(context.Background(), actressID, "Promote", "Me", "", ""))

	updated, err := repo.MovieRepo.FindByID(context.Background(), "abc015")
	require.NoError(t, err)
	assert.True(t, updated.RenderDirty, "promotion must dirty crediting movies")
	assert.Positive(t, updated.RenderGeneration)

	promoted, err := repo.ActressRepo.FindByID(context.Background(), actressID)
	require.NoError(t, err)
	assert.True(t, promoted.Verified)
	assert.Equal(t, "user", promoted.Origin)
}

func TestMergeCollidingCreditsKeepsTargetWithUserPayload(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	movie := creditMovie("abc016", []models.MovieCredit{
		{CreditedName: "Source Side", Scraped: models.Actress{LastName: "Source", FirstName: "Side"}},
	})
	saved, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	sourceCredit := saved.Credits[0]
	require.NoError(t, repo.MovieCreditRepo.UpdateOverride(context.Background(), sourceCredit.ID, "User Name", true))

	require.NoError(t, repo.ActressRepo.Create(context.Background(), &models.Actress{
		LastName: "Target", FirstName: "Side", Verified: true, Origin: "user",
	}))
	target, err := repo.ActressRepo.FindVerifiedByExactName(context.Background(), "", "Target", "Side")
	require.NoError(t, err)
	require.Len(t, target, 1)

	err = repo.MovieCreditRepo.ReassignCredit(context.Background(), &sourceCredit, target[0].ID)
	require.NoError(t, err)

	credits, err := repo.MovieCreditRepo.ListByMovie(context.Background(), "abc016")
	require.NoError(t, err)
	require.Len(t, credits, 1)
	assert.Equal(t, target[0].ID, credits[0].ActressID)
	assert.True(t, credits[0].UserOverride, "merge collision must carry the source's user payload to the target")
	assert.Equal(t, "User Name", credits[0].OverrideName)
}

func TestDeleteStaleCandidatesKeepsReferenced(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()

	movie := creditMovie("abc017", []models.MovieCredit{
		{CreditedName: "Still Credited", Scraped: models.Actress{LastName: "Still", FirstName: "Credited"}},
	})
	saved, err := repo.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	referencedID := saved.Credits[0].ActressID

	orphan := &models.Actress{LastName: "Orphan", FirstName: "Candidate", Verified: false, Origin: "scrape"}
	require.NoError(t, repo.ActressRepo.Create(context.Background(), orphan))

	pruned, err := repo.ActressRepo.DeleteStaleCandidates(context.Background(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), pruned)

	_, err = repo.ActressRepo.FindByID(context.Background(), referencedID)
	assert.NoError(t, err, "candidate with credits must not be pruned")
}
