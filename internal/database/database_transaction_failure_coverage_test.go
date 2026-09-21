package database

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// cancelOnFenceGuardContext makes the repository's explicit cancellation
// boundaries deterministic without timers or process-wide hooks. Calls made
// by database/sql and nested helpers still observe the underlying context.
type cancelOnFenceGuardContext struct {
	context.Context
	mu           sync.Mutex
	guardCalls   int
	callerSuffix string
}

func (c *cancelOnFenceGuardContext) Err() error {
	pc, _, _, ok := runtime.Caller(1)
	if !ok || !strings.HasSuffix(runtime.FuncForPC(pc).Name(), c.callerSuffix) {
		return c.Context.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.guardCalls++
	return context.Canceled
}

func (c *cancelOnFenceGuardContext) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.guardCalls
}

func TestDatabaseFailureBranchesRemainAtomic(t *testing.T) {
	t.Run("import quarantine finalizer rolls back saved identity", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		original := models.Actress{DMMID: 730001, FirstName: "Original", LastName: "Identity", Verified: true, Origin: ActressOriginImport}
		require.NoError(t, db.Create(&original).Error)

		armed, fired := false, false
		const updateHook = "import_finalizer_arm"
		const queryHook = "import_finalizer_fault"
		require.NoError(t, db.Callback().Update().After("gorm:update").Register(updateHook, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "actresses" && tx.Error == nil {
				armed = true
			}
		}))
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(queryHook, func(tx *gorm.DB) {
			if armed && !fired && tx.Statement != nil && tx.Statement.Table == "actresses" {
				fired = true
				_ = tx.AddError(context.Canceled)
			}
		}))
		t.Cleanup(func() {
			_ = db.Callback().Update().Remove(updateHook)
			_ = db.Callback().Query().Remove(queryHook)
		})

		incoming := models.Actress{ID: original.ID, DMMID: original.DMMID, FirstName: "Changed", LastName: "Identity"}
		err := repo.ImportUpsert(t.Context(), &incoming)
		require.ErrorIs(t, err, context.Canceled)
		require.True(t, fired, "fault must occur only after the import save")
		var stored models.Actress
		require.NoError(t, db.First(&stored, original.ID).Error)
		require.Equal(t, original.FirstName, stored.FirstName)
		require.Equal(t, original.Origin, stored.Origin)
	})

	t.Run("alias lookup error aborts identity resolution", func(t *testing.T) {
		db := newCreditTestDB(t)
		owner := models.Actress{JapaneseName: "正規名", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&owner).Error)
		require.NoError(t, db.Create(&models.ActressAlias{AliasName: "別名", CanonicalName: owner.JapaneseName}).Error)
		injectDatabaseCallbackError(t, db, "query", "actress_aliases", 1)

		resolved, _, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "別名"})
		require.ErrorContains(t, err, "resolve alias")
		require.Nil(t, resolved)
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
		require.EqualValues(t, 1, count)
	})

	t.Run("candidate creation error leaves no identity", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "create", "actresses", 1)

		resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "未登録"})
		require.ErrorContains(t, err, "create candidate")
		require.Nil(t, resolved)
		require.Equal(t, ResolutionCandidateLinked, outcome)
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Where("japanese_name = ?", "未登録").Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("reassign target lookup error rolls back resolved collision", func(t *testing.T) {
		db, service, credit, collision := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&target).Error)

		armed, fired := false, false
		const updateHook = "reassign_target_arm"
		const queryHook = "reassign_target_fault"
		require.NoError(t, db.Callback().Update().After("gorm:update").Register(updateHook, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "credit_collisions" && tx.Error == nil {
				armed = true
			}
		}))
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(queryHook, func(tx *gorm.DB) {
			if armed && !fired && tx.Statement != nil && tx.Statement.Table == "actresses" {
				fired = true
				_ = tx.AddError(context.Canceled)
			}
		}))
		t.Cleanup(func() {
			_ = db.Callback().Update().Remove(updateHook)
			_ = db.Callback().Query().Remove(queryHook)
		})

		_, err := service.Resolve(t.Context(), collision.ID, models.CollisionResolutionReassign, target.ID)
		require.ErrorIs(t, err, context.Canceled)
		require.True(t, fired)
		var storedCollision models.CreditCollision
		require.NoError(t, db.First(&storedCollision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusOpen, storedCollision.Status)
		require.Empty(t, storedCollision.Resolution)
		var storedCredit models.MovieCredit
		require.NoError(t, db.First(&storedCredit, credit.ID).Error)
		require.Equal(t, credit.ActressID, storedCredit.ActressID)
		var reassignments int64
		require.NoError(t, db.Model(&models.MovieCreditReassignment{}).Count(&reassignments).Error)
		require.Zero(t, reassignments)
	})

	t.Run("collision lookup error records no evidence", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
		collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: credit.MovieContentID, Field: models.CreditFieldReportedThumb, ReportedValue: "new evidence"}

		err := NewCreditCollisionRepository(db).RecordTx(db.DB, &collision, "fault-test")
		require.ErrorContains(t, err, "find collision")
		var count int64
		require.NoError(t, db.Model(&models.CreditCollision{}).Where("reported_value = ?", collision.ReportedValue).Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("credit lookup error prevents insert", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := models.Movie{ContentID: "credit-lookup-fault", ID: "credit-lookup-fault"}
		actress := models.Actress{FirstName: "Candidate", Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Create(&actress).Error)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID}

		err := NewMovieCreditRepository(db).UpsertTx(db.DB, &credit)
		require.ErrorContains(t, err, "find")
		var count int64
		require.NoError(t, db.Model(&models.MovieCredit{}).Where("movie_content_id = ?", movie.ContentID).Count(&count).Error)
		require.Zero(t, count)
	})

	for _, tc := range []struct {
		name   string
		delete func(*MovieCreditRepository, *gorm.DB, models.MovieCredit) error
	}{
		{name: "credit pair delete error preserves row", delete: func(repo *MovieCreditRepository, tx *gorm.DB, credit models.MovieCredit) error {
			return repo.DeleteTx(tx, credit.MovieContentID, credit.ActressID)
		}},
		{name: "credit id delete error preserves row", delete: func(repo *MovieCreditRepository, tx *gorm.DB, credit models.MovieCredit) error {
			return repo.DeleteByIDTx(tx, credit.ID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _, credit, _ := collisionFixture(t)
			injectDatabaseCallbackError(t, db, "delete", "movie_credits", 1)

			err := tc.delete(NewMovieCreditRepository(db), db.DB, credit)
			require.ErrorContains(t, err, "delete")
			var stored models.MovieCredit
			require.NoError(t, db.First(&stored, credit.ID).Error)
			require.Equal(t, credit.ActressID, stored.ActressID)
		})
	}
}

func TestArtifactPublicationCancellationGuardsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name          string
		callerSuffix  string
		wantPublished int
	}{
		{name: "before publisher", callerSuffix: "(*MovieRepository).WithApplyArtifactPublicationFence.func1", wantPublished: 0},
		{name: "before final clean", callerSuffix: "(*MovieRepository).WithApplyArtifactPublicationFence.func2", wantPublished: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupBaseRepoTestDB(t)
			contentID := "explicit-context-guard-" + tc.name
			seedArtifactPublicationMovie(t, db, contentID, 12, true)
			ctx := &cancelOnFenceGuardContext{Context: context.Background(), callerSuffix: tc.callerSuffix}
			published := 0

			err := NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, contentID, 12, func(*models.Movie) error {
				published++
				return nil
			})
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, tc.wantPublished, published)
			require.Equal(t, 1, ctx.callCount())
			stored := loadArtifactPublicationMovie(t, db, contentID)
			require.True(t, stored.RenderDirty, "cancellation must never finalize publication")
			require.EqualValues(t, 12, stored.RenderGeneration)
		})
	}
}
