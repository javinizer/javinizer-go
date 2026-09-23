package database

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRuntimeQuarantineVerifiedDMMlessHomonymBlocksExactDMMImmediately(t *testing.T) {
	db := newCreditTestDB(t)
	ctx := context.Background()
	candidate, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 9912601, JapaneseName: "Runtime Homonym"})
	require.NoError(t, err)
	require.Equal(t, ResolutionCandidateLinked, outcome)
	require.False(t, candidate.AmbiguityQuarantined)

	verified := &models.Actress{JapaneseName: "Runtime Homonym", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, NewActressRepository(db).Create(ctx, verified))

	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.True(t, stored.AmbiguityQuarantined)
	resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: candidate.DMMID, JapaneseName: candidate.JapaneseName})
	require.NoError(t, err)
	require.Equal(t, ResolutionAmbiguous, outcome)
	require.Equal(t, candidate.ID, resolved.ID)
}

func TestRuntimeQuarantineValidReassignmentClearsCollisionOnlyCandidateImmediately(t *testing.T) {
	db := newCreditTestDB(t)
	ctx := context.Background()
	candidate := &models.Actress{DMMID: 9912602, JapaneseName: "Collision Candidate", Origin: ActressOriginScrape}
	target := &models.Actress{DMMID: 9912603, JapaneseName: "Verified Target", Verified: true, Origin: ActressOriginUser}
	repo := NewActressRepository(db)
	require.NoError(t, repo.Create(ctx, candidate))
	require.NoError(t, repo.Create(ctx, target))
	movie := models.Movie{ContentID: "runtime-quarantine-reassign", ID: "RQR-1"}
	require.NoError(t, db.Create(&movie).Error)
	var credit models.MovieCredit
	var collision models.CreditCollision
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		credit = models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: candidate.JapaneseName}
		if err := NewMovieCreditRepository(db).UpsertTx(tx, &credit); err != nil {
			return err
		}
		collision = models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: candidate.JapaneseName, CanonicalValue: candidate.JapaneseName}
		return NewCreditCollisionRepository(db).RecordTx(tx, &collision, "test")
	}))
	require.NoError(t, db.First(candidate, candidate.ID).Error)
	require.True(t, candidate.AmbiguityQuarantined)
	open, err := NewCreditCollisionRepository(db).HasOpenForMovie(ctx, movie.ContentID)
	require.NoError(t, err)
	require.True(t, open)

	remaining, err := NewCollisionService(db).Resolve(ctx, collision.ID, models.CollisionResolutionReassign, target.ID)
	require.NoError(t, err)
	require.Zero(t, remaining)
	require.NoError(t, db.First(candidate, candidate.ID).Error)
	require.False(t, candidate.AmbiguityQuarantined)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusResolved, collision.Status)
	open, err = NewCreditCollisionRepository(db).HasOpenForMovie(ctx, movie.ContentID)
	require.NoError(t, err)
	require.False(t, open)
}

func TestRuntimeQuarantineConcurrentVerifiedCreateAndScrapeFailsClosed(t *testing.T) {
	db := newCreditTestDB(t)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("Concurrent Homonym %d", i)
		dmmID := 9922600 + i
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			errs <- retryOnLocked(func() error {
				return NewActressRepository(db).Create(ctx, &models.Actress{JapaneseName: name, Verified: true, Origin: ActressOriginUser})
			})
		}()
		go func() {
			defer wg.Done()
			<-start
			errs <- retryOnLocked(func() error {
				return db.Transaction(func(tx *gorm.DB) error {
					_, _, err := ResolveActressIdentityTx(tx, &models.Actress{DMMID: dmmID, JapaneseName: name})
					return err
				})
			})
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}

		var candidate models.Actress
		require.NoError(t, db.Where("verified = ? AND dmm_id = ?", false, dmmID).First(&candidate).Error)
		require.True(t, candidate.AmbiguityQuarantined)
		resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: dmmID, JapaneseName: name})
		require.NoError(t, err)
		require.Equal(t, ResolutionAmbiguous, outcome)
		require.Equal(t, candidate.ID, resolved.ID)
	}
}
