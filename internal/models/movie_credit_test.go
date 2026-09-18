package models

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMovieCreditDisplayAndOwnership(t *testing.T) {
	a := &Actress{FirstName: "Canonical", LastName: "Name", Verified: true}
	for _, tc := range []struct {
		name    string
		credit  MovieCredit
		actress *Actress
		want    string
	}{
		{"override", MovieCredit{UserOverride: true, OverrideName: "Mine"}, a, "Mine"},
		{"canonical", MovieCredit{CreditedName: "Reported"}, a, a.FullName()},
		{"missing identity", MovieCredit{CreditedName: "Reported"}, nil, "Reported"},
		{"candidate", MovieCredit{CreditedName: "Reported"}, &Actress{FirstName: "Candidate"}, "Reported"},
		{"empty report", MovieCredit{}, &Actress{FirstName: "Candidate"}, "Candidate"},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, tc.credit.DisplayName(tc.actress)) })
	}
	for _, tc := range []struct {
		credit MovieCredit
		origin string
		owned  bool
	}{{MovieCredit{}, "scrape", true}, {MovieCredit{Origin: "user"}, "user", false}, {MovieCredit{UserOverride: true}, "scrape", false}, {MovieCredit{Suppressed: true}, "scrape", false}} {
		require.Equal(t, tc.origin, tc.credit.EffectiveOrigin())
		require.Equal(t, tc.owned, tc.credit.IsScrapeOwned())
	}
	require.Equal(t, "movie_credits", (MovieCredit{}).TableName())
	require.Equal(t, "movie_credit_reassignments", (MovieCreditReassignment{}).TableName())
}
func TestCreditCollisionSources(t *testing.T) {
	c := CreditCollision{Status: CollisionStatusOpen}
	require.True(t, c.IsOpen())
	require.Zero(t, c.DistinctSourceCount())
	for _, source := range []string{"", "  ", " dmm ", "dmm", "javdb"} {
		c.AddSource(source)
	}
	require.Equal(t, "dmm,javdb", c.SourcesSeen)
	require.Equal(t, 2, c.DistinctSourceCount())
	c.Status = CollisionStatusResolved
	require.False(t, c.IsOpen())
	require.Equal(t, "credit_collisions", c.TableName())
}
