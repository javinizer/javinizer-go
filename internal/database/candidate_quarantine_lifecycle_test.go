package database

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCandidateQuarantineDerivedCollisionLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name                                        string
		status                                      string
		pinned, suppressed, detached, deleted, want bool
	}{
		{name: "open", status: models.CollisionStatusOpen, want: true},
		{name: "pinned open", status: models.CollisionStatusOpen, pinned: true, want: true},
		{name: "resolved", status: models.CollisionStatusResolved},
		{name: "suppressed", status: models.CollisionStatusOpen, suppressed: true},
		{name: "deleted collision", status: models.CollisionStatusOpen, deleted: true},
		{name: "detached collision", status: models.CollisionStatusOpen, detached: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			candidate := models.Actress{DMMID: 73001, JapaneseName: "Lifecycle Candidate", Origin: ActressOriginScrape, AmbiguityQuarantined: true}
			require.NoError(t, db.Create(&candidate).Error)
			movie := models.Movie{ContentID: "quarantine-lifecycle", ID: "QL-1"}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, Suppressed: tc.suppressed}
			require.NoError(t, db.Create(&credit).Error)
			creditID := credit.ID
			if tc.detached {
				creditID += 100000
			}
			collision := models.CreditCollision{CreditID: creditID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: tc.status, UserPinned: tc.pinned}
			require.NoError(t, db.Create(&collision).Error)
			if tc.deleted {
				require.NoError(t, db.Delete(&collision).Error)
			}
			require.NoError(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
			require.NoError(t, db.First(&candidate, candidate.ID).Error)
			require.Equal(t, tc.want, candidate.AmbiguityQuarantined)
		})
	}
}

func TestCandidateQuarantineDerivedVerifiedDMMlessRepresentations(t *testing.T) {
	for _, tc := range []struct {
		name                string
		verified, candidate models.Actress
		alias               *models.ActressAlias
	}{
		{name: "Japanese", verified: models.Actress{JapaneseName: "同名"}, candidate: models.Actress{JapaneseName: "同名"}},
		{name: "English first last", verified: models.Actress{FirstName: "Jane", LastName: "Doe"}, candidate: models.Actress{FirstName: "Jane", LastName: "Doe"}},
		{name: "English last first", verified: models.Actress{FirstName: "Jane", LastName: "Doe"}, candidate: models.Actress{FirstName: "Doe", LastName: "Jane"}},
		{name: "Unicode", verified: models.Actress{JapaneseName: "が"}, candidate: models.Actress{JapaneseName: "か\u3099"}},
		{name: "curated alias", verified: models.Actress{JapaneseName: "Canonical"}, candidate: models.Actress{JapaneseName: "Stage Name"}, alias: &models.ActressAlias{AliasName: "Stage Name", CanonicalName: "Canonical"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			tc.verified.Verified, tc.verified.Origin = true, ActressOriginUser
			tc.candidate.DMMID, tc.candidate.Origin = 74001, ActressOriginScrape
			require.NoError(t, db.Create(&tc.verified).Error)
			if tc.alias != nil {
				require.NoError(t, db.Create(tc.alias).Error)
			}
			require.NoError(t, db.Create(&tc.candidate).Error)
			require.NoError(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
			require.NoError(t, db.First(&tc.candidate, tc.candidate.ID).Error)
			require.True(t, tc.candidate.AmbiguityQuarantined)
			resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: tc.candidate.DMMID, FirstName: tc.candidate.FirstName, LastName: tc.candidate.LastName, JapaneseName: tc.candidate.JapaneseName})
			require.NoError(t, err)
			require.Equal(t, ResolutionAmbiguous, outcome)
			require.Equal(t, tc.candidate.ID, resolved.ID)
		})
	}
}

func TestCandidateBackfillEvidenceReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name, table string
		occurrence  int
	}{
		{name: "live collisions", table: "credit_collisions", occurrence: 1},
		{name: "verified identities", table: "actresses", occurrence: 2},
		{name: "aliases", table: "actress_aliases", occurrence: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			injectDatabaseCallbackError(t, db, "query", tc.table, tc.occurrence)
			require.Error(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
		})
	}
}

func TestCandidateBackfillFixedReadsAndIdempotentWrites(t *testing.T) {
	for _, count := range []int{0, 1, 1000} {
		t.Run(fmt.Sprintf("candidates-%d", count), func(t *testing.T) {
			db := newCreditTestDB(t)
			candidates := make([]models.Actress, count)
			for i := range candidates {
				candidates[i] = models.Actress{JapaneseName: fmt.Sprintf("Candidate %04d", i), Origin: ActressOriginScrape}
			}
			if count > 0 {
				require.NoError(t, db.CreateInBatches(&candidates, 200).Error)
			}
			var reads, writes atomic.Int64
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("count_candidate_backfill_reads", func(*gorm.DB) { reads.Add(1) }))
			require.NoError(t, db.Callback().Update().After("gorm:update").Register("count_candidate_backfill_updates", func(*gorm.DB) { writes.Add(1) }))
			require.NoError(t, db.Callback().Raw().After("gorm:raw").Register("count_candidate_backfill_raw", func(tx *gorm.DB) {
				if tx.Statement != nil && len(tx.Statement.SQL.String()) >= 6 && tx.Statement.SQL.String()[:6] == "UPDATE" {
					writes.Add(1)
				}
			}))
			require.NoError(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
			require.EqualValues(t, 4, reads.Load())
			require.LessOrEqual(t, writes.Load(), int64(4))
			reads.Store(0)
			writes.Store(0)
			require.NoError(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
			require.EqualValues(t, 4, reads.Load())
			require.Zero(t, writes.Load())
		})
	}
}
