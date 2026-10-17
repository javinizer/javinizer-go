package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestMergedCreditPresentationPolicyMatrix(t *testing.T) {
	for _, strategy := range []MergeStrategy{PreferScraper, PreferNFO, PreserveExisting, FillMissingOnly, MergeArrays} {
		for _, mergeArrays := range []bool{false, true} {
			for _, selection := range []string{"canonical", "override", "credited", "forced canonical", "forced override"} {
				t.Run(string(strategy)+map[bool]string{false: "/replace/", true: "/merge/"}[mergeArrays]+selection, func(t *testing.T) {
					identity := models.Actress{ID: 17, FirstName: "Canonical", LastName: "Person", JapaneseName: "正名", ThumbURL: "https://source.invalid/thumb.jpg", Verified: true}
					hidden := models.Actress{ID: 18, FirstName: "Hidden", LastName: "Person", Verified: true}
					credit := models.MovieCredit{ID: 71, ActressID: 17, Actress: &identity, CreditedName: "Reported Alias", OrderPinned: true}
					if selection == "override" || selection == "forced override" {
						credit.UserOverride, credit.OverrideName = true, "Selected Alias"
					}
					credit.DisplayForceCanonical = selection == "forced canonical" || selection == "forced override"
					useCredited := selection == "credited" || credit.DisplayForceCanonical
					source := &models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Source", Actresses: []models.Actress{identity}, Credits: []models.MovieCredit{credit, {ID: 72, ActressID: 18, Actress: &hidden, Suppressed: true}}}
					nfoMovie := &models.Movie{ID: source.ID, Actresses: []models.Actress{
						{FirstName: "NFO", LastName: "Name", JapaneseName: "正名", ThumbURL: "https://old.invalid/thumb.jpg"},
						{FirstName: "Hidden", LastName: "Person"},
						{ID: 999, DMMID: 999, Verified: true, Origin: "user", FirstName: "New", LastName: "Face", ThumbURL: "https://new.invalid/thumb.jpg"},
					}}
					result, err := MergeMovieMetadataWithOptions(source, nfoMovie, strategy, mergeArrays)
					require.NoError(t, err)
					rendered, names := challengeRenderCast(t, result.Merged, useCredited)
					want := "Canonical Person"
					if !mergeArrays && (strategy == PreferNFO || strategy == PreserveExisting || strategy == FillMissingOnly) {
						want = "NFO Name"
					}
					if selection == "credited" {
						want = "Reported Alias"
					}
					if credit.DisplayForceCanonical {
						want = "Canonical Person"
					}
					if credit.UserOverride {
						want = "Selected Alias"
					}
					wantCount := 2
					if strategy == PreferScraper && !mergeArrays {
						wantCount = 1
					}
					require.Len(t, rendered.Actors, wantCount)
					require.Equal(t, want, rendered.Actors[0].Name)
					require.Equal(t, "https://source.invalid/thumb.jpg", rendered.Actors[0].Thumb)
					require.Equal(t, 0, rendered.Actors[0].Order)
					require.Contains(t, names, want)
					require.NotContains(t, names, "Hidden")
					if wantCount == 2 {
						require.Equal(t, "New Face", rendered.Actors[1].Name)
						require.Equal(t, "https://new.invalid/thumb.jpg", rendered.Actors[1].Thumb)
						projection, _ := result.Merged.RenderingCredits()
						require.Zero(t, projection[1].ID)
						require.Zero(t, projection[1].ActressID)
						require.Zero(t, projection[1].Actress.ID)
						require.Zero(t, projection[1].Actress.DMMID)
						require.False(t, projection[1].Actress.Verified)
						require.Empty(t, projection[1].Actress.Origin)
					}
					require.Equal(t, source.Credits, result.Merged.Credits)
					require.Equal(t, "Canonical", source.Credits[0].Actress.FirstName)
				})
			}
		}
	}
}

func TestMergedCreditPresentationEmptyAndThumbFallback(t *testing.T) {
	identity := models.Actress{ID: 1, FirstName: "Canonical", LastName: "Person", JapaneseName: "正名", Verified: true}
	for _, force := range []bool{false, true} {
		movie := &models.Movie{ID: "CAST-001", Title: "Title", Credits: []models.MovieCredit{{ActressID: 1, Actress: &identity, DisplayForceCanonical: force}}}
		nfoMovie := &models.Movie{ID: movie.ID, Actresses: []models.Actress{{FirstName: "NFO", LastName: "Name", JapaneseName: "正名", ThumbURL: "https://nfo.invalid/thumb.jpg"}, {}}}
		result, err := MergeMovieMetadataWithOptions(movie, nfoMovie, PreferNFO, false)
		require.NoError(t, err)
		rendered, _ := challengeRenderCast(t, result.Merged, false)
		require.Len(t, rendered.Actors, 1)
		require.Equal(t, "https://nfo.invalid/thumb.jpg", rendered.Actors[0].Thumb)
		movie.Credits[0].Suppressed = true
		result, err = MergeMovieMetadataWithOptions(movie, nfoMovie, PreferNFO, false)
		require.NoError(t, err)
		rendered, names := challengeRenderCast(t, result.Merged, false)
		require.Empty(t, rendered.Actors)
		require.Empty(t, names)
	}
}
