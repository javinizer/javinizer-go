package database

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func injectDatabaseCallbackError(t *testing.T, db *DB, operation, table string, occurrence int) {
	t.Helper()
	name := fmt.Sprintf("pr260:%s:%s:%d", operation, table, occurrence)
	seen := 0
	callback := func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != table {
			return
		}
		seen++
		if seen == occurrence {
			_ = tx.AddError(errors.New("injected database error"))
		}
	}
	switch operation {
	case "query":
		require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(name, callback))
		t.Cleanup(func() { _ = db.DB.Callback().Query().Remove(name) })
	case "create":
		require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(name, callback))
		t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(name) })
	case "update":
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, callback))
		t.Cleanup(func() { _ = db.DB.Callback().Update().Remove(name) })
	case "delete":
		require.NoError(t, db.DB.Callback().Delete().Before("gorm:delete").Register(name, callback))
		t.Cleanup(func() { _ = db.DB.Callback().Delete().Remove(name) })
	}
}

func TestPR260RepositoryFactoriesAndPolicyGuards(t *testing.T) {
	db := newCreditTestDB(t)
	require.IsType(t, models.ActressAlias{}, NewActressAliasRepository(db).newEntity())
	require.IsType(t, models.CreditCollision{}, NewCreditCollisionRepository(db).newEntity())
	require.IsType(t, models.MovieCredit{}, NewMovieCreditRepository(db).newEntity())
	require.Equal(t, PolicyDecision{}, ApplyFieldCollisionPolicy(CollisionPolicyAutoKeep, nil, "", nil))
	require.Equal(t, PolicyDecision{AutoResolved: false}, ApplyFieldCollisionPolicy(CollisionPolicyAutoKeep, &models.CreditCollision{UserPinned: true}, "", nil))
	require.Equal(t, PolicyDecision{AutoResolved: false}, ApplyFieldCollisionPolicy(CollisionPolicyAutoKeep, &models.CreditCollision{Field: models.CreditFieldIdentityLink}, "", nil))
}

func TestPR260PromoteCandidateErrorPaths(t *testing.T) {
	t.Run("missing candidate", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		require.Error(t, repo.PromoteCandidate(context.Background(), 999999, "First", "Last", "", ""))
	})
	t.Run("alias persistence", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		candidate := models.Actress{FirstName: "Old", LastName: "Stage", Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&candidate).Error)
		injectDatabaseCallbackError(t, db, "create", "actress_aliases", 1)
		require.Error(t, repo.PromoteCandidate(context.Background(), candidate.ID, "New", "Canonical", "", ""))
	})
}

func TestPR260RenameNameFieldsSuccess(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "Before"}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "rename-credit", ID: "rename-credit"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID}).Error)

	repo := NewActressRepository(db)
	require.NoError(t, repo.RenameNameFields(t.Context(), actress.ID, "After", "Last", "日本名"))
	require.NoError(t, db.First(&actress, actress.ID).Error)
	require.Equal(t, "After", actress.FirstName)
	require.Equal(t, "Last", actress.LastName)
	require.Equal(t, "日本名", actress.JapaneseName)
	require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
	require.True(t, movie.RenderDirty)
}

func TestPR260AliasUpsertTxPaths(t *testing.T) {
	t.Run("existing", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressAliasRepository(db)
		existing := models.ActressAlias{AliasName: "old", CanonicalName: "first"}
		require.NoError(t, db.Create(&existing).Error)
		updated := models.ActressAlias{AliasName: "old", CanonicalName: "second", UpdatedAt: time.Now().UTC()}
		require.NoError(t, repo.UpsertTx(db.DB, &updated))
		require.Equal(t, existing.ID, updated.ID)
	})
	t.Run("create", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressAliasRepository(db)
		alias := models.ActressAlias{AliasName: "new", CanonicalName: "canonical"}
		require.NoError(t, repo.UpsertTx(db.DB, &alias))
		require.NotZero(t, alias.ID)
	})
	for _, tc := range []struct {
		name, operation string
		existing        bool
	}{
		{"find error", "query", false},
		{"update error", "update", true},
		{"create error", "create", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressAliasRepository(db)
			if tc.existing {
				require.NoError(t, db.Create(&models.ActressAlias{AliasName: "alias", CanonicalName: "old"}).Error)
			}
			injectDatabaseCallbackError(t, db, tc.operation, "actress_aliases", 1)
			require.Error(t, repo.UpsertTx(db.DB, &models.ActressAlias{AliasName: "alias", CanonicalName: "new"}))
		})
	}
}

func TestPR260CollisionRecordErrors(t *testing.T) {
	for _, tc := range []struct {
		name, operation string
		existing        bool
	}{
		{"update", "update", true},
		{"create", "create", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, service, credit, collision := collisionFixture(t)
			if !tc.existing {
				require.NoError(t, db.Delete(&collision).Error)
				collision.ID = 0
			}
			injectDatabaseCallbackError(t, db, tc.operation, "credit_collisions", 1)
			require.Error(t, service.Collisions.RecordTx(db.DB, &models.CreditCollision{CreditID: credit.ID, MovieContentID: credit.MovieContentID, Field: collision.Field, ReportedValue: collision.ReportedValue}, "dmm"))
		})
	}
}

func TestPR260MovieCreditUpsertRaceRecovery(t *testing.T) {
	db, service, credit, _ := collisionFixture(t)
	require.NoError(t, db.Delete(&credit).Error)
	incoming := models.MovieCredit{MovieContentID: credit.MovieContentID, ActressID: credit.ActressID, CreditedName: "race"}
	injected := false
	name := "pr260:credit-duplicate"
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if injected || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "movie_credits" {
			return
		}
		injected = true
		require.NoError(t, db.DB.Exec("INSERT INTO movie_credits (movie_content_id, actress_id, credited_name, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", incoming.MovieContentID, incoming.ActressID, "winner").Error)
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(name) })
	require.NoError(t, service.Credits.UpsertTx(db.DB, &incoming))
	require.NotZero(t, incoming.ID)
}

func TestPR260CandidateRaceRecovery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scraped models.Actress
		insert  string
		args    []any
	}{
		{"dmm", models.Actress{DMMID: 81234, FirstName: "Race"}, "INSERT INTO actresses (dmm_id, first_name, verified, origin, created_at, updated_at) VALUES (?, ?, 0, 'scrape', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", []any{81234, "Winner"}},
		{"name key", models.Actress{FirstName: "Key", LastName: "Race"}, "INSERT INTO actresses (first_name, last_name, verified, origin, name_key, created_at, updated_at) VALUES (?, ?, 0, 'scrape', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", []any{"Key", "Race", "race key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			name := "pr260:candidate-duplicate:" + tc.name
			fired := false
			require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
				if fired || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actresses" {
					return
				}
				fired = true
				require.NoError(t, db.DB.Exec(tc.insert, tc.args...).Error)
				_ = tx.AddError(gorm.ErrDuplicatedKey)
			}))
			t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(name) })
			found, err := createCandidateTx(db.DB, &tc.scraped, actressNameKey(&tc.scraped))
			require.NoError(t, err)
			require.NotZero(t, found.ID)
		})
	}
}

func TestPR260IdentityResolutionRemainingOutcomes(t *testing.T) {
	t.Run("new dmm candidate", func(t *testing.T) {
		db := newCreditTestDB(t)
		found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 92345, FirstName: "New"})
		require.NoError(t, err)
		require.Equal(t, ResolutionCandidateLinked, outcome)
		require.NotZero(t, found.ID)
	})
	t.Run("ambiguous name", func(t *testing.T) {
		db := newCreditTestDB(t)
		for i := 0; i < 2; i++ {
			require.NoError(t, db.Create(&models.Actress{FirstName: "Same", LastName: "Name", JapaneseName: fmt.Sprintf("別%d", i), Verified: true}).Error)
		}
		found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{FirstName: "Same", LastName: "Name"})
		require.NoError(t, err)
		require.Equal(t, ResolutionAmbiguous, outcome)
		require.False(t, found.Verified)
	})
	t.Run("duplicate aliases skip identity", func(t *testing.T) {
		db := newCreditTestDB(t)
		a := models.Actress{JapaneseName: "正名", Verified: true}
		require.NoError(t, db.Create(&a).Error)
		for _, alias := range []string{"別名", "Alias Name"} {
			require.NoError(t, db.Create(&models.ActressAlias{AliasName: alias, CanonicalName: "正名"}).Error)
		}
		matches, err := findVerifiedByAliasTx(db.DB, "別名", "Name", "Alias")
		require.NoError(t, err)
		require.Len(t, matches, 1)
	})
}

func TestPR260ActressRepositoryRemainingPaths(t *testing.T) {
	t.Run("alias second query error", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		require.NoError(t, db.Create(&models.ActressAlias{AliasName: "alias", CanonicalName: "canonical"}).Error)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		_, err := repo.FindVerifiedByAlias(context.Background(), "alias")
		require.Error(t, err)
	})
	t.Run("import create error", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		injectDatabaseCallbackError(t, db, "create", "actresses", 1)
		require.Error(t, repo.ImportUpsert(context.Background(), &models.Actress{DMMID: 99111, FirstName: "new"}))
	})
	t.Run("import save error", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		existing := models.Actress{DMMID: 99112, FirstName: "old", Verified: false}
		require.NoError(t, db.Create(&existing).Error)
		injectDatabaseCallbackError(t, db, "update", "actresses", 1)
		require.Error(t, repo.ImportUpsert(context.Background(), &models.Actress{DMMID: existing.DMMID, FirstName: "new"}))
	})
	t.Run("fresh translation filtering", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		a := models.Actress{FirstName: "First", LastName: "Last", Verified: true}
		require.NoError(t, db.Create(&a).Error)
		for i, source := range []string{"Last First", "stale", ""} {
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: a.ID, Language: fmt.Sprintf("x%d", i), SourceName: source}).Error)
		}
		fresh, err := repo.FreshTranslationsByActress(context.Background(), a.ID)
		require.NoError(t, err)
		require.Len(t, fresh, 2)
	})
	t.Run("translation query error", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		a := models.Actress{FirstName: "First", Verified: true}
		require.NoError(t, db.Create(&a).Error)
		injectDatabaseCallbackError(t, db, "query", "actress_translations", 1)
		_, err := repo.FreshTranslationsByActress(context.Background(), a.ID)
		require.Error(t, err)
	})
}

func TestPR260ReconcileActressCollisionsErrors(t *testing.T) {
	t.Run("actress lookup", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		require.Error(t, reconcileActressCollisionsTx(db.DB, 1))
	})
	t.Run("collision lookup", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
		require.Error(t, reconcileActressCollisionsTx(db.DB, credit.ActressID))
	})
	t.Run("collision update", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "update", "credit_collisions", 1)
		require.Error(t, reconcileActressCollisionsTx(db.DB, credit.ActressID))
	})
}
func TestPR260CollisionServiceDatabaseErrors(t *testing.T) {
	cases := []struct {
		name, operation, table string
		occurrence             int
		resolution             string
		field                  string
	}{
		{"resolve update", "update", "credit_collisions", 1, models.CollisionResolutionKeepIdentity, models.CreditFieldCreditedName},
		{"load collision", "query", "credit_collisions", 1, models.CollisionResolutionKeepIdentity, models.CreditFieldCreditedName},
		{"load credit", "query", "movie_credits", 1, models.CollisionResolutionKeepIdentity, models.CreditFieldCreditedName},
		{"force canonical", "update", "movie_credits", 1, models.CollisionResolutionKeepIdentity, models.CreditFieldCreditedName},
		{"identity adoption", "update", "actresses", 1, models.CollisionResolutionAdoptCanonical, models.CreditFieldIdentityLink},
		{"thumb adoption", "update", "actresses", 1, models.CollisionResolutionAdoptCanonical, models.CreditFieldReportedThumb},
		{"cjk adoption", "update", "actresses", 1, models.CollisionResolutionAdoptCanonical, models.CreditFieldCreditedName},
		{"western adoption", "update", "actresses", 1, models.CollisionResolutionAdoptCanonical, models.CreditFieldCreditedName},
		{"alias force canonical", "update", "movie_credits", 1, models.CollisionResolutionAdoptAlias, models.CreditFieldCreditedName},
		{"canonical dirty", "update", "movies", 1, models.CollisionResolutionAdoptCanonical, models.CreditFieldCreditedName},
		{"movie dirty", "update", "movies", 1, models.CollisionResolutionKeepIdentity, models.CreditFieldIdentityLink},
		{"remaining count", "query", "credit_collisions", 2, models.CollisionResolutionKeepIdentity, models.CreditFieldIdentityLink},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, service, _, collision := collisionFixture(t)
			collision.Field = tc.field
			if tc.name == "cjk adoption" {
				collision.ReportedValue = "日本名"
			} else {
				collision.ReportedValue = "Last First"
			}
			require.NoError(t, db.Save(&collision).Error)
			if tc.table == "movies" {
				require.NoError(t, db.Exec("CREATE TRIGGER pr260_fail_movie_update BEFORE UPDATE ON movies BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
			} else {
				injectDatabaseCallbackError(t, db, tc.operation, tc.table, tc.occurrence)
			}
			_, err := service.Resolve(context.Background(), collision.ID, tc.resolution, 0)
			require.Error(t, err)
		})
	}
}

func TestPR260CollisionServiceMutationErrors(t *testing.T) {
	t.Run("override update", func(t *testing.T) {
		db, service, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		require.Error(t, service.UpdateCreditOverride(context.Background(), credit.ID, "x", true))
	})
	t.Run("override pluck", func(t *testing.T) {
		db, service, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		require.Error(t, service.UpdateCreditOverride(context.Background(), credit.ID, "x", true))
	})
	t.Run("suppressed update", func(t *testing.T) {
		db, service, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		require.Error(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	})
	t.Run("close collisions", func(t *testing.T) {
		db, service, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "update", "credit_collisions", 1)
		require.Error(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	})
	t.Run("suppressed pluck", func(t *testing.T) {
		db, service, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		require.Error(t, service.SetCreditSuppressed(context.Background(), credit.ID, false))
	})
	t.Run("suppressed legacy association delete", func(t *testing.T) {
		db, service, credit, _ := collisionFixture(t)
		movie := models.Movie{ContentID: credit.MovieContentID}
		require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{{ID: credit.ActressID}}))
		require.NoError(t, db.Exec("CREATE TRIGGER pr260_fail_suppressed_legacy_delete BEFORE DELETE ON movie_actresses BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
		require.Error(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	})
	t.Run("unsuppressed legacy association insert", func(t *testing.T) {
		db, service, credit, _ := collisionFixture(t)
		require.NoError(t, db.Exec("CREATE TRIGGER pr260_fail_unsuppressed_legacy_insert BEFORE INSERT ON movie_actresses BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
		require.Error(t, service.SetCreditSuppressed(context.Background(), credit.ID, false))
	})
}

func TestPR260CreditReassignmentPersistenceErrors(t *testing.T) {
	t.Run("mapping update", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Migrator().DropTable(&models.MovieCreditReassignment{}))
		require.Error(t, reassignCreditTx(db.DB, &credit, target.ID))
	})
	t.Run("mapping insert", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_credit_reassignment_insert BEFORE INSERT ON movie_credit_reassignments BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
		require.Error(t, reassignCreditTx(db.DB, &credit, target.ID))
	})
	t.Run("mapping target lookup", func(t *testing.T) {
		db := newCreditTestDB(t)
		u := creditCoverageUpserter(db)
		movie := creditCoverageMovie(t, db, "credit-reassignment-target")
		source := models.Actress{FirstName: "Source", Verified: true}
		target := models.Actress{FirstName: "Candidate", Verified: false}
		require.NoError(t, db.Create(&source).Error)
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&models.MovieCreditReassignment{MovieContentID: movie.ContentID, SourceActressID: source.ID, TargetActressID: target.ID}).Error)
		movie.Credits = []models.MovieCredit{{CreditedName: source.FullName(), Scraped: source}}
		require.Error(t, u.persistCreditsTx(db.DB, movie))
	})
}

func TestPersistCreditsTxReassignmentQueryError(t *testing.T) {
	db := newCreditTestDB(t)
	u := creditCoverageUpserter(db)
	movie := creditCoverageMovie(t, db, "credit-reassignment-query")
	require.NoError(t, db.Migrator().DropTable(&models.MovieCreditReassignment{}))
	require.Error(t, u.persistCreditsTx(db.DB, movie))
}

func TestPR260CollisionHelpersErrors(t *testing.T) {
	t.Run("alias helpers", func(t *testing.T) {
		db := newCreditTestDB(t)
		existing := models.ActressAlias{AliasName: "alias", CanonicalName: "old"}
		require.NoError(t, db.Create(&existing).Error)
		injectDatabaseCallbackError(t, db, "update", "actress_aliases", 1)
		require.Error(t, upsertAliasTx(db.DB, &models.ActressAlias{AliasName: "alias", CanonicalName: "new"}))
	})
	t.Run("alias find", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "query", "actress_aliases", 1)
		require.Error(t, upsertAliasTx(db.DB, &models.ActressAlias{AliasName: "alias"}))
	})
	t.Run("alias create", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "create", "actress_aliases", 1)
		require.Error(t, upsertAliasTx(db.DB, &models.ActressAlias{AliasName: "alias"}))
	})
	t.Run("reassign lookup", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		require.Error(t, reassignCreditTx(db.DB, &credit, credit.ActressID+10))
	})
	t.Run("reassign update", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		require.Error(t, reassignCreditTx(db.DB, &credit, credit.ActressID+10))
	})
	t.Run("transfer source query", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
		require.Error(t, transferCollisionsTx(db.DB, 1, 2))
	})
	t.Run("transfer target query", func(t *testing.T) {
		db, _, credit, collision := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 2)
		require.Error(t, transferCollisionsTx(db.DB, credit.ID, credit.ID+1))
		require.NotZero(t, collision.ID)
	})
	t.Run("transfer move update", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "update", "credit_collisions", 1)
		require.Error(t, transferCollisionsTx(db.DB, credit.ID, credit.ID+1))
	})
	require.Equal(t, "b", mergeSourceLists("", "b"))
	require.Equal(t, "a", mergeSourceLists("a", ""))
	require.Equal(t, "a,b", mergeSourceLists("a,,a", " b "))
}

func TestPR260VerifiedNameFirstLastFallback(t *testing.T) {
	db := newCreditTestDB(t)
	existing := models.Actress{FirstName: "a", LastName: "b c", Verified: true}
	require.NoError(t, db.Create(&existing).Error)
	matches, err := findVerifiedByNameTx(db.DB, "", "a b", "c")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, existing.ID, matches[0].ID)
}

func TestPR260IdentityResolutionErrorOutcomes(t *testing.T) {
	t.Run("verified alias catalog error", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Create(&models.ActressAlias{AliasName: "alias", CanonicalName: "canonical"}).Error)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		_, err := findVerifiedByAliasTx(db.DB, "alias", "", "")
		require.Error(t, err)
	})
	t.Run("candidate duplicate reload miss", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "create", "actresses", 1)
		_, err := createCandidateTx(db.DB, &models.Actress{DMMID: 42}, "")
		require.Error(t, err)
	})
	t.Run("ambiguous alias candidate error", func(t *testing.T) {
		db := newCreditTestDB(t)
		for _, a := range []models.Actress{{JapaneseName: "一", Verified: true}, {JapaneseName: "二", Verified: true}} {
			require.NoError(t, db.Create(&a).Error)
		}
		require.NoError(t, db.Create(&models.ActressAlias{AliasName: "別", CanonicalName: "一"}).Error)
		require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Name Alias", CanonicalName: "二"}).Error)
		injectDatabaseCallbackError(t, db, "create", "actresses", 1)
		_, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "別", FirstName: "Alias", LastName: "Name"})
		require.Equal(t, ResolutionAmbiguous, outcome)
		require.Error(t, err)
	})
	t.Run("verified name query error", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		_, _, err := ResolveActressIdentityTx(db.DB, &models.Actress{FirstName: "First", LastName: "Last"})
		require.Error(t, err)
	})
	t.Run("ambiguous name candidate error", func(t *testing.T) {
		db := newCreditTestDB(t)
		for i := 0; i < 2; i++ {
			require.NoError(t, db.Create(&models.Actress{FirstName: "Same", LastName: "Name", JapaneseName: fmt.Sprintf("名%d", i), Verified: true}).Error)
		}
		injectDatabaseCallbackError(t, db, "create", "actresses", 1)
		_, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{FirstName: "Same", LastName: "Name"})
		require.Equal(t, ResolutionAmbiguous, outcome)
		require.Error(t, err)
	})
	t.Run("dmm candidate create error", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "create", "actresses", 1)
		_, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 98765})
		require.Equal(t, ResolutionCandidateLinked, outcome)
		require.Error(t, err)
	})
}

func TestPR260MovieCreditUpsertUpdateError(t *testing.T) {
	db, service, credit, _ := collisionFixture(t)
	incoming := credit
	incoming.CreditedName = "changed"
	injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
	require.Error(t, service.Credits.UpsertTx(db.DB, &incoming))
	require.Equal(t, "movie/0", NewMovieCreditRepository(db).labelFunc(models.MovieCredit{MovieContentID: "movie"}))
	require.Equal(t, "0/", NewCreditCollisionRepository(db).labelFunc(models.CreditCollision{}))
	require.Equal(t, "", NewActressAliasRepository(db).labelFunc(models.ActressAlias{}))
}

func TestPR260CollisionResolutionNestedErrors(t *testing.T) {
	t.Run("alias upsert", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "create", "actress_aliases", 1)
		_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptAlias, 0)
		require.Error(t, err)
	})
	t.Run("reassign", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 2)
		_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
		require.Error(t, err)
	})
}

func TestPR260ReassignAndTransferMutationErrors(t *testing.T) {
	t.Run("destination update", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}).Error)
		source.UserOverride = true
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		require.Error(t, reassignCreditTx(db.DB, &source, target.ID))
	})
	t.Run("collision transfer", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}).Error)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
		require.Error(t, reassignCreditTx(db.DB, &source, target.ID))
	})
	t.Run("direct credit update", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		require.Error(t, reassignCreditTx(db.DB, &source, target.ID))
	})
	t.Run("merged source credit delete", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}).Error)
		injectDatabaseCallbackError(t, db, "delete", "movie_credits", 1)
		require.Error(t, reassignCreditTx(db.DB, &source, target.ID))
	})
	t.Run("legacy association insert", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Migrator().DropTable("movie_actresses"))
		require.Error(t, reassignCreditTx(db.DB, &source, target.ID))
	})
	t.Run("legacy association delete", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		movie := models.Movie{ContentID: source.MovieContentID}
		require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{{ID: source.ActressID}}))
		require.NoError(t, db.Exec("CREATE TRIGGER pr260_fail_legacy_delete BEFORE DELETE ON movie_actresses BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
		require.Error(t, reassignCreditTx(db.DB, &source, target.ID))
	})
	for _, operation := range []string{"update", "delete"} {
		t.Run("merged collision "+operation, func(t *testing.T) {
			db, _, source, sourceCollision := collisionFixture(t)
			target := models.Actress{FirstName: "Target", Verified: true}
			require.NoError(t, db.Create(&target).Error)
			targetCredit := models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}
			require.NoError(t, db.Create(&targetCredit).Error)
			require.NoError(t, db.Create(&models.CreditCollision{CreditID: targetCredit.ID, MovieContentID: source.MovieContentID, Field: sourceCollision.Field, ReportedValue: sourceCollision.ReportedValue, Status: models.CollisionStatusOpen}).Error)
			injectDatabaseCallbackError(t, db, operation, "credit_collisions", 1)
			require.Error(t, transferCollisionsTx(db.DB, source.ID, targetCredit.ID))
		})
	}
}

func TestPR260MoveCreditsErrors(t *testing.T) {
	t.Run("source credits query", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		require.Error(t, moveCredits(db.DB, 1, 2))
	})
	t.Run("destination lookup", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 2)
		require.Error(t, moveCredits(db.DB, source.ActressID, source.ActressID+1))
	})
	t.Run("destination update", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target"}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}).Error)
		source.UserOverride = true
		require.NoError(t, db.Save(&source).Error)
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		require.Error(t, moveCredits(db.DB, source.ActressID, target.ID))
	})
	t.Run("collision transfer", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target"}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}).Error)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
		require.Error(t, moveCredits(db.DB, source.ActressID, target.ID))
	})
	t.Run("source credit delete", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target"}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}).Error)
		injectDatabaseCallbackError(t, db, "delete", "movie_credits", 1)
		require.Error(t, moveCredits(db.DB, source.ActressID, target.ID))
	})
	t.Run("credit move", func(t *testing.T) {
		db, _, source, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		require.Error(t, moveCredits(db.DB, source.ActressID, source.ActressID+1))
	})
	t.Run("source translations query", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "query", "actress_translations", 1)
		require.Error(t, moveCredits(db.DB, 1, 2))
	})
	for _, operation := range []string{"delete", "update"} {
		t.Run("translation "+operation, func(t *testing.T) {
			db := newCreditTestDB(t)
			source := models.Actress{FirstName: "Source"}
			target := models.Actress{FirstName: "Target"}
			require.NoError(t, db.Create(&source).Error)
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: source.ID, Language: "en"}).Error)
			if operation == "delete" {
				require.NoError(t, db.Create(&models.ActressTranslation{ActressID: target.ID, Language: "en"}).Error)
			}
			injectDatabaseCallbackError(t, db, operation, "actress_translations", 1)
			require.Error(t, moveCredits(db.DB, source.ID, target.ID))
		})
	}
	t.Run("target translation lookup", func(t *testing.T) {
		db := newCreditTestDB(t)
		source := models.Actress{FirstName: "Source"}
		target := models.Actress{FirstName: "Target"}
		require.NoError(t, db.Create(&source).Error)
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&models.ActressTranslation{ActressID: source.ID, Language: "en"}).Error)
		injectDatabaseCallbackError(t, db, "query", "actress_translations", 2)
		require.Error(t, moveCredits(db.DB, source.ID, target.ID))
	})
}

func TestPR260ExecuteMergeLateErrors(t *testing.T) {
	for _, stage := range []string{"credits", "dirty"} {
		t.Run(stage, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			target := models.Actress{FirstName: "Target", Verified: true}
			source := models.Actress{FirstName: "Source", Verified: true}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)
			movie := models.Movie{ContentID: "merge-late-" + stage, ID: "merge-late-" + stage}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: target.ID}).Error)
			plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, nil)
			require.NoError(t, err)
			if stage == "credits" {
				injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
			} else {
				require.NoError(t, db.Exec("CREATE TRIGGER pr260_fail_merge_dirty BEFORE UPDATE ON movies BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
			}
			_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
			require.Error(t, err)
		})
	}
}

func TestPR260DuplicateRecoveryReloadErrors(t *testing.T) {
	t.Run("candidate", func(t *testing.T) {
		db := newCreditTestDB(t)
		name := "pr260:candidate-duplicate-reload-error"
		require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "actresses" {
				_ = tx.AddError(gorm.ErrDuplicatedKey)
			}
		}))
		t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(name) })
		_, err := createCandidateTx(db.DB, &models.Actress{DMMID: 777001}, "")
		require.Error(t, err)
	})
	t.Run("credit", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewMovieCreditRepository(db)
		name := "pr260:credit-duplicate-reload-error"
		require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "movie_credits" {
				_ = tx.AddError(gorm.ErrDuplicatedKey)
			}
		}))
		t.Cleanup(func() { _ = db.DB.Callback().Create().Remove(name) })
		err := repo.UpsertTx(db.DB, &models.MovieCredit{MovieContentID: "missing", ActressID: 777001})
		require.Error(t, err)
	})
}
