package workflow

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCompareLinkedNFOIsReadOnlyPreviewNotPublicationAuthority(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/movie.nfo"
	original := []byte(`<movie><id>CAST-001</id><title>Old</title><actor><name>Old Alias</name><javinizer-credit version="1" selection="override" movie="cast001" credit="42"/></actor></movie>`)
	require.NoError(t, afero.WriteFile(fs, path, original, 0644))
	raw := &models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Fresh", Credits: []models.MovieCredit{{CreditedName: "Reported"}}}
	orch := newCompareOrchestrator(fs, nil, &mockScraperInterface{result: &scrape.ScrapeResult{Movie: raw}}, nil)
	result, err := orch.Execute(t.Context(), CompareCmd{MovieID: raw.ID, NFOPath: path, ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true})
	require.NoError(t, err)
	require.Len(t, result.Movie.Actresses, 1)
	require.Equal(t, "Old", result.Movie.Actresses[0].FirstName)
	require.Equal(t, raw.Credits, result.Movie.Credits)
	require.Zero(t, result.Movie.Actresses[0].ID)
	require.Zero(t, result.Movie.Credits[0].ID)
	links, present := result.NFOData.NFOActorLinks()
	require.True(t, present)
	require.Len(t, links[0], 1, "preview must not mutate the original parsed input")
	body, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	require.Equal(t, original, body)
	// Actual publication lookup reparses the file and rejects the unknown link.
	impl := nfo.NewNFOImplementor(fs, &nfo.Config{FilenameTemplate: "movie.nfo"}, nil)
	apply := impl.MergeWithExistingNFO(raw, nfo.MergeWithExistingOptions{Match: models.FileMatchInfo{Path: "/movie.mp4"}, ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true})
	var blocked *nfo.CastAmbiguityError
	require.ErrorAs(t, apply.Err, &blocked)
}

func TestComparePreservesTypedLegacyCastBlock(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/movie.nfo", []byte(`<movie><id>CAST-001</id><title>Old</title><actor><name>Prior Alias</name></actor></movie>`), 0644))
	raw := &models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Fresh", Credits: []models.MovieCredit{{CreditedName: "Reported", Origin: "user"}}}
	orch := newCompareOrchestrator(fs, nil, &mockScraperInterface{result: &scrape.ScrapeResult{Movie: raw}}, nil)
	_, err := orch.Execute(t.Context(), CompareCmd{MovieID: raw.ID, NFOPath: "/movie.nfo", ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true})
	require.ErrorIs(t, err, ErrMergeFailed)
	var blocked *nfo.CastAmbiguityError
	require.ErrorAs(t, err, &blocked)
}
