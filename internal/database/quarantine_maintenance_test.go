package database

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func countMovieUpsertQueries(t *testing.T, creditCount int) (int, int) {
	t.Helper()
	db := newCreditTestDB(t)
	catalog := make([]models.Actress, 1000)
	for i := range catalog {
		catalog[i] = models.Actress{DMMID: 700000 + i, JapaneseName: fmt.Sprintf("Catalog %04d", i), Origin: ActressOriginScrape}
	}
	require.NoError(t, db.CreateInBatches(&catalog, 200).Error)

	queries, recomputes := 0, 0
	callbackName := fmt.Sprintf("quarantine-scaling-%d", creditCount)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		queries++
		sql := tx.Statement.SQL.String()
		if tx.Statement.Table == "actresses" && strings.Contains(sql, "verified = ?") && strings.Contains(sql, "ORDER BY id") && len(tx.Statement.Vars) > 0 && tx.Statement.Vars[0] == false {
			recomputes++
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	movie := &models.Movie{ContentID: fmt.Sprintf("quarantine-scaling-%d", creditCount), ID: fmt.Sprintf("QS-%d", creditCount), Title: "Scaling", Credits: make([]models.MovieCredit, creditCount)}
	for i := range movie.Credits {
		movie.Credits[i] = models.MovieCredit{Scraped: catalog[i], CreditedName: catalog[i].JapaneseName, Source: "query-probe"}
	}
	_, err := NewMovieRepository(db).Upsert(context.Background(), movie)
	require.NoError(t, err)
	return queries, recomputes
}

func TestMovieUpsertQuarantineMaintenanceScalesWithCreditWork(t *testing.T) {
	oneQueries, oneRecomputes := countMovieUpsertQueries(t, 1)
	tenQueries, tenRecomputes := countMovieUpsertQueries(t, 10)
	hundredQueries, hundredRecomputes := countMovieUpsertQueries(t, 100)
	require.Equal(t, 1, oneRecomputes)
	require.Equal(t, 1, tenRecomputes)
	require.Equal(t, 1, hundredRecomputes)
	require.LessOrEqual(t, tenQueries-oneQueries, 15*9, "1=%d 10=%d", oneQueries, tenQueries)
	require.LessOrEqual(t, hundredQueries-tenQueries, 15*90, "10=%d 100=%d", tenQueries, hundredQueries)
	t.Logf("queries: 1=%d 10=%d 100=%d; recomputes: %d/%d/%d", oneQueries, tenQueries, hundredQueries, oneRecomputes, tenRecomputes, hundredRecomputes)
}

func TestStandaloneMutationFinalizerFailureRollsBack(t *testing.T) {
	t.Run("credit", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := models.Movie{ContentID: "finalize-credit", ID: "FINALIZE-CREDIT"}
		candidate := models.Actress{DMMID: 810001, JapaneseName: "Credit Candidate", Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Create(&candidate).Error)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}
		require.Error(t, NewMovieCreditRepository(db).UpsertTx(db.DB, &credit))
		var count int64
		require.NoError(t, db.Model(&models.MovieCredit{}).Where("movie_content_id = ?", movie.ContentID).Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("collision", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: credit.MovieContentID, Field: models.CreditFieldIdentityLink, ReportedValue: "rollback"}
		require.Error(t, NewCreditCollisionRepository(db).RecordTx(db.DB, &collision, "test"))
		var count int64
		require.NoError(t, db.Model(&models.CreditCollision{}).Where("reported_value = ?", "rollback").Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("alias", func(t *testing.T) {
		db := newCreditTestDB(t)
		owner := models.Actress{JapaneseName: "Alias Owner", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&owner).Error)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		alias := models.ActressAlias{AliasName: "Rollback Alias", CanonicalName: owner.JapaneseName}
		require.Error(t, NewActressAliasRepository(db).UpsertTx(db.DB, &alias))
		var count int64
		require.NoError(t, db.Model(&models.ActressAlias{}).Where("alias_name = ?", alias.AliasName).Count(&count).Error)
		require.Zero(t, count)
	})
}

func TestStandaloneAliasImmediatelyMaintainsCandidateQuarantine(t *testing.T) {
	db := newCreditTestDB(t)
	candidate := models.Actress{DMMID: 810002, JapaneseName: "Candidate Canonical", Origin: ActressOriginScrape}
	owner := models.Actress{JapaneseName: "Verified Canonical", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&candidate).Error)
	require.NoError(t, db.Create(&owner).Error)
	alias := models.ActressAlias{AliasName: candidate.JapaneseName, CanonicalName: owner.JapaneseName}
	require.NoError(t, NewActressAliasRepository(db).UpsertTx(db.DB, &alias))
	require.NoError(t, db.First(&candidate, candidate.ID).Error)
	require.True(t, candidate.AmbiguityQuarantined)
}

func TestStandaloneCreditAndCollisionImmediatelyMaintainCandidateQuarantine(t *testing.T) {
	t.Run("collision sets", func(t *testing.T) {
		db := newCreditTestDB(t)
		candidate := models.Actress{DMMID: 810003, JapaneseName: "Collision Candidate", Origin: ActressOriginScrape}
		movie := models.Movie{ContentID: "standalone-collision", ID: "STANDALONE-COLLISION"}
		require.NoError(t, db.Create(&candidate).Error)
		require.NoError(t, db.Create(&movie).Error)
		credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}
		require.NoError(t, db.Create(&credit).Error)
		collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: candidate.JapaneseName}
		require.NoError(t, NewCreditCollisionRepository(db).RecordTx(db.DB, &collision, "test"))
		require.NoError(t, db.First(&candidate, candidate.ID).Error)
		require.True(t, candidate.AmbiguityQuarantined)
	})

	t.Run("credit delete clears", func(t *testing.T) {
		db := newCreditTestDB(t)
		candidate := models.Actress{DMMID: 810004, JapaneseName: "Deleted Credit Candidate", Origin: ActressOriginScrape}
		movie := models.Movie{ContentID: "standalone-credit-delete", ID: "STANDALONE-CREDIT-DELETE"}
		require.NoError(t, db.Create(&candidate).Error)
		require.NoError(t, db.Create(&movie).Error)
		credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}
		require.NoError(t, db.Create(&credit).Error)
		collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: candidate.JapaneseName, Status: models.CollisionStatusOpen}
		require.NoError(t, db.Create(&collision).Error)
		require.NoError(t, recomputeActressCandidateQuarantineTx(db.DB))
		require.NoError(t, db.First(&candidate, candidate.ID).Error)
		require.True(t, candidate.AmbiguityQuarantined)
		require.NoError(t, NewMovieCreditRepository(db).DeleteByIDTx(db.DB, credit.ID))
		require.NoError(t, db.First(&candidate, candidate.ID).Error)
		require.False(t, candidate.AmbiguityQuarantined)
	})
}
