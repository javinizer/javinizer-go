package database

import (
	"encoding/xml"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The optional XML extension can be stripped by an older editor. Persisted user
// ownership must still make an unknown former alias block rather than turn into
// a second actor after override changes, clears, or suppression.
func TestChallengeLegacyNFORoundTripBlocksHistoricalAlias(t *testing.T) {
	for _, mode := range []string{"change", "clear", "clear then suppress"} {
		t.Run(mode, func(t *testing.T) {
			db := newCreditTestDB(t)
			repos := db.Repositories()
			a := models.Actress{DMMID: 991013, FirstName: "Source", LastName: "Person", Verified: true, Origin: "user"}
			require.NoError(t, repos.ActressRepo.Create(t.Context(), &a))
			saved, err := repos.MovieRepo.Upsert(t.Context(), &models.Movie{ID: "CAST-003", ContentID: "cast003", Title: "Synthetic", Credits: []models.MovieCredit{{CreditedName: "Person Source", Scraped: a}}})
			require.NoError(t, err)
			service := NewCollisionService(db)
			require.NoError(t, service.UpdateCreditOverride(t.Context(), saved.Credits[0].ID, "First Alias", true))
			old, err := repos.MovieRepo.FindByID(t.Context(), saved.ID)
			require.NoError(t, err)
			fs := afero.NewMemMapFs()
			g := nfo.NewGenerator(fs, &nfo.Config{FirstNameOrder: true})
			require.NoError(t, g.GenerateAtPath(t.Context(), old, "/existing.nfo", "", nil))
			original, err := afero.ReadFile(fs, "/existing.nfo")
			require.NoError(t, err)
			var legacy nfo.Movie
			require.NoError(t, xml.Unmarshal(original, &legacy))
			require.Len(t, legacy.Actors, 1)
			require.NotEmpty(t, legacy.Actors[0].PresentationLinks)
			legacy.Actors[0].PresentationLinks = nil
			stripped, err := xml.Marshal(legacy)
			require.NoError(t, err)
			require.NoError(t, afero.WriteFile(fs, "/existing.nfo", stripped, 0644))
			parsed, err := nfo.ParseNFO(fs, "/existing.nfo")
			require.NoError(t, err)
			if mode == "change" {
				require.NoError(t, service.UpdateCreditOverride(t.Context(), saved.Credits[0].ID, "Second Alias", true))
			} else {
				require.NoError(t, service.UpdateCreditOverride(t.Context(), saved.Credits[0].ID, "", false))
			}
			if mode == "clear then suppress" {
				require.NoError(t, service.SetCreditSuppressed(t.Context(), saved.Credits[0].ID, true))
			}
			current, err := repos.MovieRepo.FindByID(t.Context(), saved.ID)
			require.NoError(t, err)
			_, err = nfo.MergeMovieMetadataWithOptions(current, parsed.Movie, nfo.PreferNFO, true)
			var ambiguity *nfo.CastAmbiguityError
			require.ErrorAs(t, err, &ambiguity)
			require.Contains(t, ambiguity.Error(), "First Alias")
			stored, err := repos.MovieRepo.FindByID(t.Context(), saved.ID)
			require.NoError(t, err)
			require.Equal(t, current.Credits, stored.Credits)
			after, err := afero.ReadFile(fs, "/existing.nfo")
			require.NoError(t, err)
			require.Equal(t, stripped, after)
		})
	}
}
