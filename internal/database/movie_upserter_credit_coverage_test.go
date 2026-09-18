package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

func creditCoverageUpserter(db *DB) *MovieUpserter {
	return NewMovieUpserter(db.Repositories().MovieRepo.(*MovieRepository))
}

func creditCoverageMovie(t *testing.T, db *DB, id string) *models.Movie {
	t.Helper()
	movie := &models.Movie{ContentID: id, ID: id, Title: id}
	require.NoError(t, db.Create(movie).Error)
	return movie
}

func creditCoverageRepos(db *DB) (*CreditCollisionRepository, *ActressAliasRepository) {
	return NewCreditCollisionRepository(db), NewActressAliasRepository(db)
}

func TestPR260CreditLabels(t *testing.T) {
	require.Equal(t, "movie/7", movieCreditLabel(models.MovieCredit{MovieContentID: "movie", ActressID: 7}))
	require.Equal(t, "9/field", creditCollisionLabel(models.CreditCollision{CreditID: 9, Field: "field"}))
}

func TestMovieUpserterCreditHelpers(t *testing.T) {
	require.Equal(t, "First", scrapedFirstName(&models.MovieCredit{CreditedName: " Last  First "}))
	require.Equal(t, "Last", scrapedLastName(&models.MovieCredit{CreditedName: " Last  First "}))
	require.Equal(t, "Solo", scrapedFirstName(&models.MovieCredit{CreditedName: " Solo "}))
	require.Empty(t, scrapedLastName(&models.MovieCredit{CreditedName: " Solo "}))
	credit := &models.MovieCredit{Scraped: models.Actress{DMMID: 91}}
	require.Equal(t, 91, resolvedDMMIDFromCredit(credit))

	db := newCreditTestDB(t)
	loadCreditsIntoTx(db.DB, nil)
	movie := &models.Movie{ContentID: "load-error", Credits: []models.MovieCredit{{ID: 77}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loadCreditsIntoTx(db.WithContext(ctx), movie)
	require.Equal(t, uint(77), movie.Credits[0].ID)
}

func TestPersistCreditsTxUsesLoadedActressIdentity(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	movie := creditCoverageMovie(t, db, "credit-loaded-identity")
	canonical := models.Actress{DMMID: 7401, FirstName: "Canonical", LastName: "Identity", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&canonical).Error)
	movie.Credits = []models.MovieCredit{{
		MovieContentID: movie.ContentID,
		ActressID:      canonical.ID,
		Actress:        &canonical,
	}}

	require.NoError(t, u.persistCreditsTx(db.DB, movie))

	require.Len(t, movie.Credits, 1)
	require.Equal(t, canonical.ID, movie.Credits[0].ActressID)
	var saved models.MovieCredit
	require.NoError(t, db.First(&saved, movie.Credits[0].ID).Error)
	require.Equal(t, canonical.ID, saved.ActressID)
}

func TestPersistCreditsTxSuccessBranches(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	movie := creditCoverageMovie(t, db, "credit-success")
	verified := models.Actress{DMMID: 7101, LastName: "Canonical", FirstName: "Person", ThumbURL: "canonical.jpg", Verified: true, Origin: "user"}
	suppressedActress := models.Actress{DMMID: 7102, LastName: "Hidden", FirstName: "Person", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&verified).Error)
	require.NoError(t, db.Create(&suppressedActress).Error)
	pinned := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: verified.ID, CreditedName: "Old", Origin: "scrape", OrderIndex: 9, OrderPinned: true}
	suppressed := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: suppressedActress.ID, CreditedName: "Hidden Person", Origin: "user", Suppressed: true}
	require.NoError(t, db.Create(&pinned).Error)
	require.NoError(t, db.Create(&suppressed).Error)

	movie.CreditPolicy = "auto_alias"
	movie.TrustedCollisionSources = []string{" trusted "}
	movie.Credits = []models.MovieCredit{
		{ActressID: verified.ID, CreditedName: "Variant Person", ReportedThumbURL: "variant.jpg", Source: "trusted", Origin: "scrape", Scraped: models.Actress{DMMID: verified.DMMID}},
		{ActressID: suppressedActress.ID, CreditedName: "Hidden Person", Origin: "scrape", Scraped: models.Actress{DMMID: suppressedActress.DMMID}},
		{CreditedName: "Fallback Person", CreditedJapaneseName: "代替", Origin: "scrape"},
	}
	require.NoError(t, u.persistCreditsTx(db.DB, movie))
	require.Equal(t, 9, movie.Credits[0].OrderIndex)
	require.True(t, movie.Credits[1].Suppressed)
	require.NotZero(t, movie.Credits[2].ActressID)
	require.Len(t, movie.Actresses, 1)
	require.Equal(t, verified.ID, movie.Actresses[0].ID)

	var refreshed models.MovieCredit
	require.NoError(t, db.First(&refreshed, movie.Credits[0].ID).Error)
	require.True(t, refreshed.DisplayForceCanonical)
	var alias models.ActressAlias
	require.NoError(t, db.First(&alias, "alias_name = ?", "Variant Person").Error)
	require.Equal(t, "Canonical Person", alias.CanonicalName)
}

func TestPersistCreditsTxReconcileBranches(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	movie := creditCoverageMovie(t, db, "credit-reconcile")
	actresses := make([]models.Actress, 6)
	for i := range actresses {
		actresses[i] = models.Actress{FirstName: fmt.Sprintf("A%d", i), Verified: true}
		require.NoError(t, db.Create(&actresses[i]).Error)
	}
	credits := []models.MovieCredit{
		{MovieContentID: movie.ContentID, ActressID: actresses[0].ID, Origin: "user"},
		{MovieContentID: movie.ContentID, ActressID: actresses[1].ID, UserOverride: true},
		{MovieContentID: movie.ContentID, ActressID: actresses[2].ID, Suppressed: true},
		{MovieContentID: movie.ContentID, ActressID: actresses[3].ID, LegacyInferred: true},
		{MovieContentID: movie.ContentID, ActressID: actresses[4].ID, Origin: "scrape"},
		{MovieContentID: movie.ContentID, ActressID: actresses[5].ID, Origin: "scrape"},
	}
	for i := range credits {
		require.NoError(t, db.Create(&credits[i]).Error)
	}
	collision := models.CreditCollision{CreditID: credits[4].ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "pinned", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)
	movie.Credits = []models.MovieCredit{}
	require.NoError(t, u.persistCreditsTx(db.DB, movie))
	var remaining int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Where("movie_content_id = ?", movie.ContentID).Count(&remaining).Error)
	require.Equal(t, int64(5), remaining)

	skipMovie := creditCoverageMovie(t, db, "credit-skip")
	skip := models.MovieCredit{MovieContentID: skipMovie.ContentID, ActressID: actresses[0].ID, Origin: "scrape"}
	require.NoError(t, db.Create(&skip).Error)
	skipMovie.Credits = []models.MovieCredit{}
	skipMovie.SkipCreditReconcile = true
	require.NoError(t, u.persistCreditsTx(db.DB, skipMovie))
	require.NoError(t, db.First(&skip, skip.ID).Error)

	associationMovie := creditCoverageMovie(t, db, "credit-empty-association")
	associationMovie.Actresses = []models.Actress{actresses[0]}
	require.NoError(t, db.Model(associationMovie).Association("Actresses").Replace(associationMovie.Actresses))
	associationMovie.Credits = []models.MovieCredit{}
	require.NoError(t, u.persistCreditsTx(db.DB, associationMovie))
	count := db.Model(associationMovie).Association("Actresses").Count()
	require.Zero(t, count)
}

func TestRecordFieldCollisionsTxBranches(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	movie := creditCoverageMovie(t, db, "field-branches")
	actress := models.Actress{LastName: "Real", FirstName: "Name", ThumbURL: "real.jpg", Verified: true}
	require.NoError(t, db.Create(&actress).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, Scraped: models.Actress{LastName: "Real", FirstName: "Name", ThumbURL: "real.jpg"}}
	require.NoError(t, db.Create(&credit).Error)
	collisions, aliases := creditCoverageRepos(db)
	require.NoError(t, u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, CollisionPolicyBlock, nil))

	credit.CreditedName = "Different Name"
	credit.ReportedThumbURL = "different.jpg"
	require.NoError(t, u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, CollisionPolicyBlock, nil))
	var existing models.CreditCollision
	require.NoError(t, db.First(&existing, "credit_id = ? AND field = ?", credit.ID, models.CreditFieldCreditedName).Error)
	existing.Status = models.CollisionStatusResolved
	require.NoError(t, db.Save(&existing).Error)
	require.NoError(t, u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, CollisionPolicyAutoKeep, nil))
}

func TestRecordFieldCollisionsHonorsAcceptedAlias(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	actress := models.Actress{FirstName: "Truth", LastName: "Original", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Stage Name", CanonicalName: "Original Truth"}).Error)
	movie := creditCoverageMovie(t, db, "field-alias-match")
	movie.Credits = []models.MovieCredit{{
		CreditedName: "Stage Name",
		Source:       "dmm",
		Scraped:      models.Actress{FirstName: "Stage", LastName: "Name"},
	}}

	require.NoError(t, u.persistCreditsTx(db.DB, movie))
	open, err := NewCreditCollisionRepository(db).ListOpenByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Empty(t, open)
	require.Equal(t, actress.ID, movie.Credits[0].ActressID)
}

func TestRecordFieldCollisionsAliasLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	movie := creditCoverageMovie(t, db, "field-alias-error")
	actress := models.Actress{FirstName: "Truth", LastName: "Original", Verified: true}
	require.NoError(t, db.Create(&actress).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Different Name"}
	require.NoError(t, db.Create(&credit).Error)
	collisions, aliases := creditCoverageRepos(db)
	injectDatabaseCallbackError(t, db, "query", "actress_aliases", 1)
	require.Error(t, u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, CollisionPolicyBlock, nil))
}

func TestRecordFieldCollisionsMatchesCanonicalJapaneseName(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	movie := creditCoverageMovie(t, db, "field-japanese-match")
	actress := models.Actress{
		FirstName:    "Sakura",
		LastName:     "Yamada",
		JapaneseName: "山田さくら",
		Verified:     true,
		Origin:       ActressOriginUser,
	}
	require.NoError(t, db.Create(&actress).Error)
	credit := models.MovieCredit{
		MovieContentID:       movie.ContentID,
		ActressID:            actress.ID,
		CreditedJapaneseName: "山田さくら",
	}
	require.NoError(t, db.Create(&credit).Error)
	collisions, aliases := creditCoverageRepos(db)
	require.NoError(t, u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, CollisionPolicyBlock, nil))
	open, err := collisions.ListOpenByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Empty(t, open)
}

func TestRecordFieldCollisionsRetiresSuperseded(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	movie := creditCoverageMovie(t, db, "superseded-field-collision")
	actress := models.Actress{FirstName: "Canonical", ThumbURL: "canonical.jpg", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Reported A", ReportedThumbURL: "reported.jpg"}
	require.NoError(t, db.Create(&credit).Error)
	collisions, aliases := creditCoverageRepos(db)
	require.NoError(t, u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, CollisionPolicyBlock, nil))

	credit.CreditedName = "Reported B"
	require.NoError(t, u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, CollisionPolicyBlock, nil))
	open, err := collisions.ListOpenByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, open, 2)
	for _, collision := range open {
		if collision.Field == models.CreditFieldCreditedName {
			require.Equal(t, "Reported B", collision.ReportedValue)
		}
	}

	credit.CreditedName = actress.FullName()
	credit.ReportedThumbURL = actress.ThumbURL
	require.NoError(t, u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, CollisionPolicyBlock, nil))
	open, err = collisions.ListOpenByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Empty(t, open)
}

func TestResolveSupersededOpenPreservesPinned(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	collision.UserPinned = true
	require.NoError(t, db.Save(&collision).Error)
	require.NoError(t, service.Collisions.resolveSupersededOpenTx(db.DB, credit.ID, collision.Field, ""))
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
}

func TestResolveSupersededOpenError(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	injectDatabaseCallbackError(t, db, "update", "credit_collisions", 1)
	err := service.Collisions.resolveSupersededOpenTx(db.DB, credit.ID, collision.Field, "new")
	require.Error(t, err)
}

func TestPersistCreditsTxQueryAndIdentityErrors(t *testing.T) {
	t.Run("credit list", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Migrator().DropTable(&models.MovieCredit{}))
		err := creditCoverageUpserter(db).persistCreditsTx(db.DB, &models.Movie{ContentID: "missing", Credits: []models.MovieCredit{}})
		require.Error(t, err)
	})
	t.Run("collision list", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Migrator().DropTable(&models.CreditCollision{}))
		err := creditCoverageUpserter(db).persistCreditsTx(db.DB, &models.Movie{ContentID: "missing", Credits: []models.MovieCredit{}})
		require.Error(t, err)
	})
	t.Run("identity", func(t *testing.T) {
		db := newCreditTestDB(t)
		name := "coverage:identity-query"
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "actresses" {
				_ = tx.AddError(errors.New("identity query failed"))
			}
		}))
		defer func() { _ = db.Callback().Query().Remove(name) }()
		err := creditCoverageUpserter(db).persistCreditsTx(db.DB, &models.Movie{ContentID: "identity-error", Credits: []models.MovieCredit{{CreditedName: "New Person"}}})
		require.Error(t, err)
	})
	t.Run("identity collision reconciliation", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := creditCoverageMovie(t, db, "identity-collision-reconcile-error")
		injectDatabaseCallbackError(t, db, "update", "credit_collisions", 1)
		movie.Credits = []models.MovieCredit{{CreditedName: "New Person"}}
		require.Error(t, creditCoverageUpserter(db).persistCreditsTx(db.DB, movie))
	})
}

func TestPersistCreditsTxWriteErrors(t *testing.T) {
	t.Run("credit upsert through public path", func(t *testing.T) {
		db := newCreditTestDB(t)
		name := "coverage:credit-create"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "movie_credits" {
				_ = tx.AddError(errors.New("credit create failed"))
			}
		}))
		defer func() { _ = db.Callback().Create().Remove(name) }()
		movie := creditMovie("credit-write-error", []models.MovieCredit{{CreditedName: "New Person"}})
		_, err := db.Repositories().MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
		require.Error(t, err)
	})
	t.Run("ambiguous collision", func(t *testing.T) {
		db := newCreditTestDB(t)
		u := creditCoverageUpserter(db)
		movie := creditCoverageMovie(t, db, "ambiguous-write-error")
		for _, first := range []string{"One", "Two"} {
			a := models.Actress{JapaneseName: "同名", FirstName: first, Verified: true}
			require.NoError(t, db.Create(&a).Error)
		}
		name := "coverage:ambiguous-collision-create"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "credit_collisions" {
				_ = tx.AddError(errors.New("collision create failed"))
			}
		}))
		defer func() { _ = db.Callback().Create().Remove(name) }()
		movie.Credits = []models.MovieCredit{{CreditedName: "同名", Scraped: models.Actress{JapaneseName: "同名"}}}
		require.Error(t, u.persistCreditsTx(db.DB, movie))
	})
	t.Run("field collision", func(t *testing.T) {
		db := newCreditTestDB(t)
		u := creditCoverageUpserter(db)
		movie := creditCoverageMovie(t, db, "field-write-error")
		a := models.Actress{DMMID: 7201, LastName: "Real", FirstName: "Name", Verified: true}
		require.NoError(t, db.Create(&a).Error)
		name := "coverage:field-collision-create"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "credit_collisions" {
				_ = tx.AddError(errors.New("collision create failed"))
			}
		}))
		defer func() { _ = db.Callback().Create().Remove(name) }()
		movie.Credits = []models.MovieCredit{{CreditedName: "Wrong Name", Scraped: models.Actress{DMMID: a.DMMID}}}
		require.Error(t, u.persistCreditsTx(db.DB, movie))
	})
}

func TestRecordFieldCollisionsTxErrors(t *testing.T) {
	cases := []struct {
		name       string
		operation  string
		table      string
		policy     CollisionPolicy
		trusted    map[string]bool
		createFail bool
	}{
		{name: "record", operation: "create", table: "credit_collisions", policy: CollisionPolicyBlock, createFail: true},
		{name: "reconcile", operation: "update", table: "credit_collisions", policy: CollisionPolicyBlock},
		{name: "resolve", operation: "update", table: "credit_collisions", policy: CollisionPolicyAutoKeep},
		{name: "force canonical", operation: "update", table: "movie_credits", policy: CollisionPolicyAutoKeep},
		{name: "alias", operation: "create", table: "actress_aliases", policy: CollisionPolicyAutoAlias, trusted: map[string]bool{"trusted": true}, createFail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			u := creditCoverageUpserter(db)
			movie := creditCoverageMovie(t, db, "field-error-"+strings.ReplaceAll(tc.name, " ", "-"))
			actress := models.Actress{LastName: "Real", FirstName: "Name", Verified: true}
			require.NoError(t, db.Create(&actress).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Wrong Name", Source: "trusted"}
			require.NoError(t, db.Create(&credit).Error)
			collisions, aliases := creditCoverageRepos(db)
			callbackName := "coverage:field-error:" + tc.name
			updates := 0
			inject := func(tx *gorm.DB) {
				if tx.Statement == nil || tx.Statement.Table != tc.table {
					return
				}
				updates++
				if tc.name != "resolve" || updates == 3 {
					_ = tx.AddError(errors.New("injected " + tc.name))
				}
			}
			if tc.operation == "create" {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, inject))
				defer func() { _ = db.Callback().Create().Remove(callbackName) }()
			} else {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, inject))
				defer func() { _ = db.Callback().Update().Remove(callbackName) }()
			}
			err := u.recordFieldCollisionsTx(db.DB, collisions, aliases, &credit, &actress, tc.policy, tc.trusted)
			require.Error(t, err)
		})
	}
}

func TestPersistCreditsTxProjectionErrors(t *testing.T) {
	t.Run("delete collisions", func(t *testing.T) {
		db := newCreditTestDB(t)
		u := creditCoverageUpserter(db)
		movie := creditCoverageMovie(t, db, "reconcile-delete-collisions-error")
		a := models.Actress{FirstName: "Absent", Verified: true}
		require.NoError(t, db.Create(&a).Error)
		credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: a.ID, Origin: "scrape"}
		require.NoError(t, db.Create(&credit).Error)
		name := "coverage:delete-collisions"
		require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && strings.Contains(tx.Statement.SQL.String(), "DELETE FROM credit_collisions") {
				_ = tx.AddError(errors.New("delete collisions failed"))
			}
		}))
		defer func() { _ = db.Callback().Raw().Remove(name) }()
		movie.Credits = []models.MovieCredit{}
		require.Error(t, u.persistCreditsTx(db.DB, movie))
	})
	t.Run("ensure actresses", func(t *testing.T) {
		db := newCreditTestDB(t)
		u := creditCoverageUpserter(db)
		movie := creditCoverageMovie(t, db, "ensure-actresses-error")
		a := models.Actress{DMMID: 7301, LastName: "Real", FirstName: "Name", Verified: true}
		require.NoError(t, db.Create(&a).Error)
		queries := 0
		name := "coverage:ensure-actresses"
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "actresses" {
				queries++
				if queries == 3 {
					_ = tx.AddError(errors.New("ensure actresses failed"))
				}
			}
		}))
		defer func() { _ = db.Callback().Query().Remove(name) }()
		movie.Credits = []models.MovieCredit{{CreditedName: "Real Name", Scraped: models.Actress{DMMID: a.DMMID}}}
		require.Error(t, u.persistCreditsTx(db.DB, movie))
	})
	t.Run("replace association", func(t *testing.T) {
		db := newCreditTestDB(t)
		u := creditCoverageUpserter(db)
		movie := creditCoverageMovie(t, db, "replace-association-error")
		a := models.Actress{DMMID: 7302, LastName: "Real", FirstName: "Name", Verified: true}
		require.NoError(t, db.Create(&a).Error)
		name := "coverage:replace-association"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "movie_actresses" {
				_ = tx.AddError(errors.New("replace association failed"))
			}
		}))
		defer func() { _ = db.Callback().Create().Remove(name) }()
		movie.Credits = []models.MovieCredit{{CreditedName: "Real Name", Scraped: models.Actress{DMMID: a.DMMID}}}
		require.Error(t, u.persistCreditsTx(db.DB, movie))
	})
}

func TestPersistCreditsTxReconcileErrors(t *testing.T) {
	cases := []struct {
		name      string
		operation string
		table     string
	}{
		{name: "close collisions", operation: "update", table: "credit_collisions"},
		{name: "delete credit", operation: "delete", table: "movie_credits"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			u := creditCoverageUpserter(db)
			movie := creditCoverageMovie(t, db, "reconcile-error-"+strings.ReplaceAll(tc.name, " ", "-"))
			a := models.Actress{FirstName: "Absent", Verified: true}
			require.NoError(t, db.Create(&a).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: a.ID, Origin: "scrape"}
			require.NoError(t, db.Create(&credit).Error)
			callbackName := "coverage:reconcile-error:" + tc.name
			inject := func(tx *gorm.DB) {
				if tx.Statement != nil && tx.Statement.Table == tc.table {
					_ = tx.AddError(errors.New("injected " + tc.name))
				}
			}
			if tc.operation == "update" {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, inject))
				defer func() { _ = db.Callback().Update().Remove(callbackName) }()
			} else {
				require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callbackName, inject))
				defer func() { _ = db.Callback().Delete().Remove(callbackName) }()
			}
			movie.Credits = []models.MovieCredit{}
			require.Error(t, u.persistCreditsTx(db.DB, movie))
		})
	}
}
