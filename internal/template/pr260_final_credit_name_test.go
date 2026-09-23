package template

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"testing"
)

func TestFinalVerifiedUnnamedCreditUsesUnknownFallback(t *testing.T) {
	empty := &models.Actress{Verified: true}
	movie := &models.Movie{Credits: []models.MovieCredit{{Actress: empty}, {Actress: &models.Actress{Verified: true, FirstName: "Yui", LastName: "Hatano"}}}}
	ctx := NewContextFromMovieWithOptions(movie, ContextOptions{RenderCredits: true})
	if len(ctx.Actresses) != 2 || ctx.Actresses[0] != "Unknown" || ctx.Actresses[1] != "Hatano Yui" {
		t.Fatalf("verified unnamed actress must use canonical unknown fallback: %#v", ctx.Actresses)
	}
	if len(ctx.ActressDetails) != 2 || ctx.ActressDetails[1].FirstName != "Yui" {
		t.Fatalf("details must correspond to rendered credit: %#v", ctx.ActressDetails)
	}
}
