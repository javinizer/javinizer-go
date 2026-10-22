package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestLegacyNativeNameResolutionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name             string
		stored, incoming models.Actress
		want             ResolutionOutcome
		reuse            bool
	}{
		{"native single", models.Actress{FirstName: "依本しおり", Verified: true}, models.Actress{JapaneseName: "依本しおり"}, ResolutionMatched, true},
		{"native spaced", models.Actress{FirstName: "田中", LastName: "麻美", Verified: true}, models.Actress{JapaneseName: " 田中　 麻美 "}, ResolutionMatched, true},
		{"native normalization", models.Actress{FirstName: "ガ", Verified: true}, models.Actress{JapaneseName: "ｶﾞ"}, ResolutionMatched, true},
		{"proper shape", models.Actress{JapaneseName: "依本しおり", Verified: true}, models.Actress{JapaneseName: "依本しおり"}, ResolutionMatched, true},
		{"no reverse guess", models.Actress{FirstName: "麻美", LastName: "田中", Verified: true}, models.Actress{JapaneseName: "田中 麻美"}, ResolutionCandidateLinked, false},
		{"no space deletion", models.Actress{FirstName: "田中", LastName: "麻美", Verified: true}, models.Actress{JapaneseName: "田中麻美"}, ResolutionCandidateLinked, false},
		{"no Latin bridge", models.Actress{FirstName: "Jane", LastName: "Smith", Verified: true}, models.Actress{JapaneseName: "Jane Smith"}, ResolutionCandidateLinked, false},
		{"different Japanese", models.Actress{FirstName: "依本しおり", JapaneseName: "別人", Verified: true}, models.Actress{JapaneseName: "依本しおり"}, ResolutionCandidateLinked, false},
		{"not last only", models.Actress{LastName: "依本しおり", Verified: true}, models.Actress{JapaneseName: "依本しおり"}, ResolutionCandidateLinked, false},
		{"not mixed input", models.Actress{FirstName: "依本しおり", Verified: true}, models.Actress{JapaneseName: "依本しおり", FirstName: "Other"}, ResolutionCandidateLinked, false},
		{"same positive DMM wins", models.Actress{DMMID: 383, FirstName: "依本しおり", Verified: true}, models.Actress{DMMID: 383, JapaneseName: "別名"}, ResolutionMatched, true},
		{"different positive DMM", models.Actress{DMMID: 383, FirstName: "依本しおり", Verified: true}, models.Actress{DMMID: 384, JapaneseName: "依本しおり"}, ResolutionCandidateLinked, false},
		{"new positive DMM quarantines", models.Actress{FirstName: "依本しおり", Verified: true}, models.Actress{DMMID: 384, JapaneseName: "依本しおり"}, ResolutionAmbiguous, false},
		{"unverified remains candidate", models.Actress{FirstName: "依本しおり"}, models.Actress{JapaneseName: "依本しおり"}, ResolutionCandidateLinked, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			require.NoError(t, db.Create(&tc.stored).Error)
			found, outcome, err := ResolveActressIdentityTx(db.DB, &tc.incoming)
			require.NoError(t, err)
			require.Equal(t, tc.want, outcome)
			require.Equal(t, tc.reuse, found.ID == tc.stored.ID)
			require.Equal(t, tc.reuse && tc.stored.Verified, found.Verified)
			if tc.want == ResolutionAmbiguous {
				require.True(t, found.AmbiguityQuarantined)
			}
			var stored models.Actress
			require.NoError(t, db.First(&stored, tc.stored.ID).Error)
			require.Equal(t, tc.stored.FirstName, stored.FirstName)
			require.Equal(t, tc.stored.LastName, stored.LastName)
			require.Equal(t, tc.stored.JapaneseName, stored.JapaneseName)
			require.Equal(t, tc.stored.DMMID, stored.DMMID)
			require.Equal(t, tc.stored.Verified, stored.Verified)
		})
	}
}

func TestLegacyNativeNameMultipleOwners(t *testing.T) {
	for _, kind := range []string{"legacy", "canonical", "alias"} {
		t.Run(kind, func(t *testing.T) {
			db := newCreditTestDB(t)
			legacy := models.Actress{FirstName: "依本しおり", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&legacy).Error)
			other := models.Actress{JapaneseName: "依本しおり", Verified: true, Origin: ActressOriginUser}
			switch kind {
			case "legacy":
				other.JapaneseName = ""
				other.FirstName = "依本しおり"
			case "alias":
				other.JapaneseName = "別人"
			}
			require.NoError(t, db.Create(&other).Error)
			if kind == "alias" {
				require.NoError(t, db.Create(&models.ActressAlias{AliasName: "依本しおり", CanonicalName: "別人"}).Error)
			}
			incoming := models.Actress{JapaneseName: "依本しおり"}
			candidate, outcome, err := ResolveActressIdentityTx(db.DB, &incoming)
			require.NoError(t, err)
			require.Equal(t, ResolutionAmbiguous, outcome)
			require.False(t, candidate.Verified)
			require.NotEqual(t, legacy.ID, candidate.ID)
			require.NotEqual(t, other.ID, candidate.ID)
			repeated, outcome, err := ResolveActressIdentityTx(db.DB, &incoming)
			require.NoError(t, err)
			require.Equal(t, ResolutionAmbiguous, outcome)
			require.Equal(t, candidate.ID, repeated.ID)
			// Removing a competing owner must not rewrite or promote the candidate.
			require.NoError(t, NewActressRepository(db).Delete(t.Context(), other.ID))
			found, outcome, err := ResolveActressIdentityTx(db.DB, &incoming)
			require.NoError(t, err)
			require.Equal(t, ResolutionMatched, outcome)
			require.Equal(t, legacy.ID, found.ID)
			require.NoError(t, db.First(candidate, candidate.ID).Error)
			require.False(t, candidate.Verified)
		})
	}
}

func TestLegacyNativeQuarantinedCandidateIsNotPromoted(t *testing.T) {
	db := newCreditTestDB(t)
	legacy := models.Actress{FirstName: "依本しおり", Verified: false, AmbiguityQuarantined: true, Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&legacy).Error)
	_, _, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "依本しおり"})
	require.ErrorIs(t, err, ErrActressCandidateAmbiguous)
	var stored models.Actress
	require.NoError(t, db.First(&stored, legacy.ID).Error)
	require.Equal(t, legacy, stored)
}

func TestLegacyNativeRescrapeAmbiguityKeepsCollision(t *testing.T) {
	db := newCreditTestDB(t)
	legacy := models.Actress{FirstName: "依本しおり", Verified: true, Origin: ActressOriginUser}
	canonical := models.Actress{JapaneseName: "依本しおり", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&legacy).Error)
	require.NoError(t, db.Create(&canonical).Error)
	var candidateID uint
	var creditID uint
	for i := 0; i < 2; i++ {
		movie := creditMovie("legacy-native-ambiguous", []models.MovieCredit{
			{CreditedJapaneseName: "依本しおり", Source: "javlibrary", Scraped: models.Actress{JapaneseName: "依本しおり"}},
		})
		saved, err := creditCoverageUpserter(db).Upsert(t.Context(), movie)
		require.NoError(t, err)
		require.Len(t, saved.Credits, 1)
		credit := saved.Credits[0]
		require.NotEqual(t, legacy.ID, credit.ActressID)
		require.NotEqual(t, canonical.ID, credit.ActressID)
		if i == 0 {
			candidateID = credit.ActressID
			creditID = credit.ID
		}
		require.Equal(t, candidateID, credit.ActressID)
		require.Equal(t, creditID, credit.ID)
		var candidate models.Actress
		require.NoError(t, db.First(&candidate, candidateID).Error)
		require.False(t, candidate.Verified)
		require.True(t, candidate.AmbiguityQuarantined)
		var collisions []models.CreditCollision
		require.NoError(t, db.Where("credit_id = ? AND field = ? AND status = ?", creditID, models.CreditFieldIdentityLink, models.CollisionStatusOpen).Find(&collisions).Error)
		require.Len(t, collisions, 1)
	}
}
