package database

import (
	"context"
	"errors"
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
	require.Equal(t, 2, oneRecomputes)
	require.Equal(t, 2, tenRecomputes)
	require.Equal(t, 2, hundredRecomputes)
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
func TestQuarantineTransitionPropagationRepairsCastAndRender(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	verified := models.Actress{JapaneseName: "確定名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, repos.ActressRepo.Create(context.Background(), &verified))
	movie := creditMovie("propagate-quarantine", []models.MovieCredit{{
		CreditedName:         "舞台名",
		CreditedJapaneseName: "舞台名",
		Scraped:              models.Actress{DMMID: 991001, JapaneseName: "舞台名"},
	}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	candidateID := saved.Credits[0].ActressID

	var candidate models.Actress
	require.NoError(t, db.First(&candidate, candidateID).Error)
	require.False(t, candidate.AmbiguityQuarantined)
	joinIDs := func() []uint {
		var ids []uint
		require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", "propagate-quarantine").Pluck("actress_id", &ids).Error)
		return ids
	}
	require.Equal(t, []uint{candidateID}, joinIDs())
	renderState := func() (bool, int64) {
		var m models.Movie
		require.NoError(t, db.First(&m, "content_id = ?", "propagate-quarantine").Error)
		return m.RenderDirty, m.RenderGeneration
	}
	dirty, generation := renderState()
	require.False(t, dirty)
	require.Equal(t, int64(0), generation)

	aliasRepo := NewActressAliasRepository(db)
	require.NoError(t, aliasRepo.Upsert(context.Background(), &models.ActressAlias{AliasName: "舞台名", CanonicalName: "確定名"}))
	require.NoError(t, db.First(&candidate, candidateID).Error)
	require.True(t, candidate.AmbiguityQuarantined)
	require.Empty(t, joinIDs())
	dirty, generation = renderState()
	require.True(t, dirty)
	require.Equal(t, int64(1), generation)

	require.NoError(t, aliasRepo.Delete(context.Background(), "舞台名"))
	require.NoError(t, db.First(&candidate, candidateID).Error)
	require.False(t, candidate.AmbiguityQuarantined)
	require.Equal(t, []uint{candidateID}, joinIDs())
	dirty, generation = renderState()
	require.True(t, dirty)
	require.Equal(t, int64(2), generation)
}

func TestQuarantineTransitionWithoutCreditingMovies(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	candidate := models.Actress{DMMID: 991002, JapaneseName: "舞台名", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	verified := models.Actress{JapaneseName: "確定名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, repos.ActressRepo.Create(context.Background(), &verified))

	aliasRepo := NewActressAliasRepository(db)
	require.NoError(t, aliasRepo.Upsert(context.Background(), &models.ActressAlias{AliasName: "舞台名", CanonicalName: "確定名"}))
	require.NoError(t, db.First(&candidate, candidate.ID).Error)
	require.True(t, candidate.AmbiguityQuarantined)

	countCandidates := func() int64 {
		var count int64
		require.NoError(t, db.Model(&models.Movie{}).Where("render_dirty = ?", true).Count(&count).Error)
		return count
	}
	require.Zero(t, countCandidates())
}

func injectRawStatementError(t *testing.T, db *DB, fragment string) {
	t.Helper()
	name := fmt.Sprintf("quarantine-propagation:%s", fragment)
	seen := 0
	require.NoError(t, db.DB.Callback().Raw().Before("gorm:raw").Register(name, func(tx *gorm.DB) {
		if tx.Statement == nil || !strings.Contains(tx.Statement.SQL.String(), fragment) {
			return
		}
		seen++
		if seen == 1 {
			_ = tx.AddError(errors.New("injected raw statement error"))
		}
	}))
	t.Cleanup(func() { _ = db.DB.Callback().Raw().Remove(name) })
}

func propagationFlipFixture(t *testing.T, contentID string) *DB {
	t.Helper()
	db := newCreditTestDB(t)
	repos := db.Repositories()
	verified := models.Actress{JapaneseName: "確定名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, repos.ActressRepo.Create(context.Background(), &verified))
	movie := creditMovie(contentID, []models.MovieCredit{{
		CreditedName:         "舞台名",
		CreditedJapaneseName: "舞台名",
		Scraped:              models.Actress{DMMID: 991003, JapaneseName: "舞台名"},
	}})
	_, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	return db
}

func TestPropagateQuarantineTransitionsErrors(t *testing.T) {
	t.Run("collect quarantined", func(t *testing.T) {
		db := propagationFlipFixture(t, "propagate-err-collect-quarantine")
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		err := NewActressAliasRepository(db).Upsert(context.Background(), &models.ActressAlias{AliasName: "舞台名", CanonicalName: "確定名"})
		require.Error(t, err)
	})
	t.Run("collect unquarantined", func(t *testing.T) {
		db := propagationFlipFixture(t, "propagate-err-collect-unquarantine")
		aliasRepo := NewActressAliasRepository(db)
		require.NoError(t, aliasRepo.Upsert(context.Background(), &models.ActressAlias{AliasName: "舞台名", CanonicalName: "確定名"}))
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		require.Error(t, aliasRepo.Delete(context.Background(), "舞台名"))
	})
	t.Run("remove join", func(t *testing.T) {
		db := propagationFlipFixture(t, "propagate-err-remove")
		injectRawStatementError(t, db, "DELETE FROM movie_actresses")
		require.Error(t, NewActressAliasRepository(db).Upsert(context.Background(), &models.ActressAlias{AliasName: "舞台名", CanonicalName: "確定名"}))
	})
	t.Run("restore join", func(t *testing.T) {
		db := propagationFlipFixture(t, "propagate-err-restore")
		aliasRepo := NewActressAliasRepository(db)
		require.NoError(t, aliasRepo.Upsert(context.Background(), &models.ActressAlias{AliasName: "舞台名", CanonicalName: "確定名"}))
		injectRawStatementError(t, db, "INSERT OR IGNORE INTO movie_actresses")
		require.Error(t, aliasRepo.Delete(context.Background(), "舞台名"))
	})
	t.Run("mark render dirty", func(t *testing.T) {
		db := propagationFlipFixture(t, "propagate-err-dirty")
		injectDatabaseCallbackError(t, db, "update", "movies", 1)
		require.Error(t, NewActressAliasRepository(db).Upsert(context.Background(), &models.ActressAlias{AliasName: "舞台名", CanonicalName: "確定名"}))
	})
}
