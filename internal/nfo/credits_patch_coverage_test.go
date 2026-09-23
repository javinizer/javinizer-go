package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestBuildActorsForMoviePatchBranches(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{FirstName: "Legacy"}}}
	withoutCredits := (&Generator{config: &Config{}}).buildActorsForMovie(movie)
	assert.Len(t, withoutCredits, 1)
	assert.False(t, (&Generator{}).creditAwareActors())

	movie.Credits = []models.MovieCredit{{CreditedName: "Credit"}}
	withConfig := (&Generator{config: &Config{}}).buildActorsForMovie(movie)
	assert.Len(t, withConfig, 1)
	assert.Equal(t, "Credit", withConfig[0].Name)
	assert.True(t, (&Generator{config: &Config{}}).creditAwareActors())
}

func TestResolveCreditDisplayNamePatchBranches(t *testing.T) {
	canonical := &models.Actress{FirstName: "Yui", LastName: "Hatano", Verified: true}

	tests := []struct {
		name   string
		config Config
		credit models.MovieCredit
		want   string
	}{
		{
			name: "user override",
			credit: models.MovieCredit{
				UserOverride: true,
				OverrideName: "  Override  ",
				Actress:      canonical,
			},
			want: "Override",
		},
		{
			name:   "credited name enabled",
			config: Config{UseCreditedName: true},
			credit: models.MovieCredit{CreditedName: "  Credited  ", Actress: canonical},
			want:   "Credited",
		},
		{
			name:   "verified canonical name",
			credit: models.MovieCredit{CreditedName: "Credited", Actress: canonical},
			want:   "Hatano Yui",
		},
		{
			name:   "credited name without identity",
			credit: models.MovieCredit{CreditedName: "  Unlinked  "},
			want:   "Unlinked",
		},
		{
			name: "empty unlinked credit",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Generator{config: &tt.config}
			assert.Equal(t, tt.want, g.resolveCreditDisplayName(tt.credit))
		})
	}
}

func TestBuildActorsFromCreditsPatchBranches(t *testing.T) {
	g := &Generator{config: &Config{AddGenericRole: true, AltNameRole: true}}
	assert.Nil(t, g.buildActorsFromCredits(nil))

	primary := &models.Actress{
		FirstName:    "Yui",
		LastName:     "Hatano",
		JapaneseName: "波多野結衣",
		ThumbURL:     "https://example.com/yui.jpg",
		Verified:     true,
	}
	credits := []models.MovieCredit{
		{Suppressed: true, Actress: primary},
		{},
		{Actress: primary},
		{CreditedName: "Hatano Yui"},
		{CreditedName: "Aoi", Actress: &models.Actress{JapaneseName: "葵", Verified: false}},
	}

	actors := g.buildActorsFromCredits(credits)
	assert.Equal(t, []actor{
		{Name: "Hatano Yui", Role: "波多野結衣", Thumb: "https://example.com/yui.jpg"},
	}, actors)
}
