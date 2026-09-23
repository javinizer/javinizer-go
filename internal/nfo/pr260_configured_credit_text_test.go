package nfo

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPR260ConfiguredTextUsesCreditDisplayPolicy(t *testing.T) {
	canonical := models.Actress{FirstName: "Canonical", LastName: "One", Verified: true}
	forced := models.Actress{FirstName: "Canonical", LastName: "Two", Verified: true}
	overridden := models.Actress{FirstName: "Canonical", LastName: "Three", Verified: true}
	quarantined := models.Actress{FirstName: "Quarantined", LastName: "Person", Verified: false}
	suppressed := models.Actress{FirstName: "Suppressed", LastName: "Person", Verified: true}
	movie := &models.Movie{
		ID:        "CREDIT-TEXT",
		Actresses: []models.Actress{{FirstName: "Legacy", LastName: "Projection"}},
		Credits: []models.MovieCredit{
			{Actress: &canonical, CreditedName: "Credited One"},
			{Actress: &forced, CreditedName: "Credited Two", DisplayForceCanonical: true},
			{Actress: &overridden, CreditedName: "Credited Three", UserOverride: true, OverrideName: "Editor Override"},
			{Actress: &quarantined, CreditedName: "Quarantined Credit"},
			{Actress: &suppressed, CreditedName: "Suppressed Credit", Suppressed: true},
		},
	}

	for _, tc := range []struct {
		name, want string
		credited   bool
	}{
		{name: "canonical", credited: false, want: "Canonical One / Canonical Two / Editor Override"},
		{name: "credited with force and override precedence", credited: true, want: "Credited One / Canonical Two / Editor Override"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(afero.NewMemMapFs(), &Config{
				Tagline: "<ACTRESSES>", Tag: []string{"<ACTRESSES>"},
				FirstNameOrder: true, ActressDelimiter: " / ", UseCreditedName: tc.credited,
			})
			nfo, err := g.movieToNFO(context.Background(), movie, "", "", 0, false, nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, nfo.Tagline)
			require.Contains(t, nfo.Tags, tc.want)
			require.NotContains(t, nfo.Tagline, "Legacy Projection")
			require.NotContains(t, nfo.Tagline, "Quarantined")
			require.NotContains(t, nfo.Tagline, "Suppressed")
		})
	}
}
