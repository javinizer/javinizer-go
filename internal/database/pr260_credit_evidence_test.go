package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCreditHasIdentityEvidence(t *testing.T) {
	cases := []struct {
		name   string
		credit *models.MovieCredit
		want   bool
	}{
		{"nil credit", nil, false},
		{"no evidence", &models.MovieCredit{}, false},
		{"credited name", &models.MovieCredit{CreditedName: "Name"}, true},
		{"credited japanese name", &models.MovieCredit{CreditedJapaneseName: "名"}, true},
		{"override name", &models.MovieCredit{OverrideName: "Override"}, true},
		{"actress id", &models.MovieCredit{ActressID: 7}, true},
		{"actress pointer", &models.MovieCredit{Actress: &models.Actress{}}, true},
		{"scraped name", &models.MovieCredit{Scraped: models.Actress{LastName: "Scraped"}}, true},
		{"scraped dmm id", &models.MovieCredit{Scraped: models.Actress{DMMID: 9}}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, creditHasIdentityEvidence(tc.credit))
		})
	}
}
