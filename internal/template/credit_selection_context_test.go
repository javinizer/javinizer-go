package template

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestSelectedCreditNameStaysFrozenAcrossActressFormatting(t *testing.T) {
	identity := &models.Actress{ID: 41, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", ThumbURL: "identity.jpg", Verified: true}
	tests := []struct {
		name        string
		credit      models.MovieCredit
		useCredited bool
		want        string
	}{
		{
			name:        "credited alias equal to canonical",
			credit:      models.MovieCredit{ID: 7, ActressID: identity.ID, CreditedName: "Yui Hatano", ReportedThumbURL: "credit.jpg", Actress: identity},
			useCredited: true,
			want:        "Yui Hatano|Yui Hatano|Yui Hatano|Yui Hatano|Yui Hatano",
		},
		{
			name:   "explicit override equal to canonical",
			credit: models.MovieCredit{ID: 8, ActressID: identity.ID, OverrideName: "Yui Hatano", UserOverride: true, Actress: identity},
			want:   "Yui Hatano|Yui Hatano|Yui Hatano|Yui Hatano|Yui Hatano",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := &models.Movie{Credits: []models.MovieCredit{tt.credit}}
			ctx := NewContextFromMovieWithOptions(movie, ContextOptions{FirstNameOrder: true, RenderCredits: true, UseCreditedName: tt.useCredited})
			ctx.ActressLanguageJa = true

			got, err := NewEngine().Execute("<ACTORS>|<ACTRESSES>|<ACTRESS>|<ACTORNAME>|<ACTRESSNAME>", ctx)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.credit.ID, movie.Credits[0].ID)
			require.Equal(t, uint(41), movie.Credits[0].Actress.ID)
			require.Equal(t, "identity.jpg", movie.Credits[0].Actress.ThumbURL)
		})
	}
}

func TestCanonicalCreditFallbackRemainsFormatAware(t *testing.T) {
	identity := &models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true}
	movie := &models.Movie{Credits: []models.MovieCredit{{Actress: identity}}}
	ctx := NewContextFromMovieWithOptions(movie, ContextOptions{FirstNameOrder: true, RenderCredits: true, UseCreditedName: true})
	ctx.ActressLanguageJa = true

	got, err := NewEngine().Execute("<ACTORS>|<ACTRESS>", ctx)
	require.NoError(t, err)
	require.Equal(t, "波多野結衣|波多野結衣", got)
}

func TestSelectedCreditIgnoresNameOrderModifiersAndStillGroups(t *testing.T) {
	first := &models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true}
	second := &models.Actress{FirstName: "Aoi", LastName: "Tsukasa", JapaneseName: "葵つかさ", Verified: true}
	movie := &models.Movie{Credits: []models.MovieCredit{
		{CreditedName: "  Yui Hatano  ", Actress: first},
		{CreditedName: "Aoi Tsukasa", Actress: second},
	}}
	ctx := NewContextFromMovieWithOptions(movie, ContextOptions{FirstNameOrder: true, RenderCredits: true, UseCreditedName: true})
	ctx.ActressLanguageJa = true

	got, err := NewEngine().Execute("<ACTORS:LAST>|<ACTRESS:LAST>|<ACTORNAME:JA>", ctx)
	require.NoError(t, err)
	require.Equal(t, "Yui Hatano, Aoi Tsukasa|Yui Hatano|Yui Hatano", got)

	ctx.GroupActress = true
	ctx.GroupActressName = "Ensemble"
	got, err = NewEngine().Execute("<ACTORS>|<ACTRESS>", ctx)
	require.NoError(t, err)
	require.Equal(t, "Ensemble|Yui Hatano", got)
}
