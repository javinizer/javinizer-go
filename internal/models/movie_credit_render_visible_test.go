package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMovieCredit_RenderVisible(t *testing.T) {
	testCases := []struct {
		name   string
		credit *MovieCredit
		want   bool
	}{
		{"nil credit", nil, false},
		{"nil actress", &MovieCredit{}, false},
		{"verified actress", &MovieCredit{Actress: &Actress{Verified: true}}, true},
		{"verified and quarantined", &MovieCredit{Actress: &Actress{Verified: true, AmbiguityQuarantined: true}}, true},
		{"unverified unquarantined", &MovieCredit{Actress: &Actress{}}, true},
		{"unverified quarantined", &MovieCredit{Actress: &Actress{AmbiguityQuarantined: true}}, false},
		{"suppressed verified", &MovieCredit{Suppressed: true, Actress: &Actress{Verified: true}}, false},
		{"suppressed unverified quarantined", &MovieCredit{Suppressed: true, Actress: &Actress{AmbiguityQuarantined: true}}, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.credit.RenderVisible())
		})
	}
}
