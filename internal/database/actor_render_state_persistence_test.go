package database

import (
	"encoding/xml"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestChallengeNFOMergeCannotPersistPresentationIdentity(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	a := models.Actress{DMMID: 991011, FirstName: "Source", LastName: "Person", Verified: true, Origin: "user"}
	require.NoError(t, repos.ActressRepo.Create(t.Context(), &a))
	incoming := &models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Synthetic", Credits: []models.MovieCredit{{CreditedName: "Person Source", Scraped: a}}}
	saved, err := repos.MovieRepo.Upsert(t.Context(), incoming)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	n := &models.Movie{ID: saved.ID, Actresses: []models.Actress{{FirstName: "Only", LastName: "NFO", ThumbURL: "https://nfo.invalid/face.jpg"}}}
	merged, err := nfo.MergeMovieMetadataWithOptions(saved, n, nfo.PreferNFO, true)
	require.NoError(t, err)
	presentation, ok := merged.Merged.RenderingCredits()
	require.True(t, ok)
	require.Len(t, presentation, 2)
	require.Zero(t, presentation[1].ID)
	require.Zero(t, presentation[1].ActressID)
	require.NotNil(t, presentation[1].Actress)
	require.Zero(t, presentation[1].Actress.ID)
	require.Zero(t, presentation[1].Actress.DMMID)
	require.False(t, presentation[1].Actress.Verified)
	require.Equal(t, saved.Credits, merged.Merged.Credits)
	// Exercise the generic merger's persistence boundary, not only direct GORM.
	reloaded, err := repos.MovieRepo.Upsert(t.Context(), merged.Merged)
	require.NoError(t, err)
	require.Len(t, reloaded.Credits, 1)
	require.Equal(t, a.ID, reloaded.Credits[0].ActressID)
	require.Len(t, reloaded.Actresses, 1)
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	canonical, err := repos.ActressRepo.FindByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, "Source", canonical.FirstName)
	require.Equal(t, "Person", canonical.LastName)
	credits, ok := reloaded.RenderingCredits()
	require.True(t, ok)
	require.Len(t, credits, 1, "reloading must discard private NFO presentation")
}

func TestChallengeNFOPreviousOverrideDoesNotReappearAsNewActor(t *testing.T) {
	for _, mode := range []string{"change", "clear", "clear then suppress"} {
		t.Run(mode, func(t *testing.T) {
			db := newCreditTestDB(t)
			repos := db.Repositories()
			a := models.Actress{DMMID: 991012, FirstName: "Source", LastName: "Person", Verified: true, Origin: "user"}
			require.NoError(t, repos.ActressRepo.Create(t.Context(), &a))
			incoming := &models.Movie{ID: "CAST-002", ContentID: "cast002", Title: "Synthetic", Credits: []models.MovieCredit{{CreditedName: "Person Source", Scraped: a}}}
			saved, err := repos.MovieRepo.Upsert(t.Context(), incoming)
			require.NoError(t, err)
			service := NewCollisionService(db)
			require.NoError(t, service.UpdateCreditOverride(t.Context(), saved.Credits[0].ID, "First Alias", true))
			old, err := repos.MovieRepo.FindByID(t.Context(), saved.ID)
			require.NoError(t, err)
			fs := afero.NewMemMapFs()
			g := nfo.NewGenerator(fs, &nfo.Config{FirstNameOrder: true})
			require.NoError(t, g.GenerateAtPath(t.Context(), old, "/existing.nfo", "", nil))
			parsed, err := nfo.ParseNFO(fs, "/existing.nfo")
			require.NoError(t, err)
			require.Len(t, parsed.Movie.Actresses, 1)
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
			merged, err := nfo.MergeMovieMetadataWithOptions(current, parsed.Movie, nfo.PreferNFO, true)
			require.NoError(t, err)
			require.NoError(t, g.GenerateAtPath(t.Context(), merged.Merged, "/result.nfo", "", nil))
			body, err := afero.ReadFile(fs, "/result.nfo")
			require.NoError(t, err)
			var rendered nfo.Movie
			require.NoError(t, xml.Unmarshal(body, &rendered))
			names := make([]string, 0, len(rendered.Actors))
			for _, actor := range rendered.Actors {
				names = append(names, actor.Name)
			}
			t.Logf("after %s: cast=%v", mode, names)
			require.NotContains(t, names, "First Alias", "a previously generated credit name is stale output, not a new actor")
		})
	}
}
