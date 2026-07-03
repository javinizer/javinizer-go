package worker

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFieldOverride_StringFields(t *testing.T) {
	movie, prov := overrideFixture()
	err := applyFieldOverride(movie, prov, "maker", "dmm")
	require.NoError(t, err)
	assert.Equal(t, "DMM Studio", movie.Maker)
	assert.Equal(t, "dmm", prov.FieldSources["maker"])
}

func TestApplyFieldOverride_TitleLinksDisplayTitle(t *testing.T) {
	movie, prov := overrideFixture()
	err := applyFieldOverride(movie, prov, "title", "javlibrary")
	require.NoError(t, err)
	assert.Equal(t, "JavLibrary Title", movie.Title)
	assert.Equal(t, "JavLibrary Title", movie.DisplayTitle)
	assert.Equal(t, "javlibrary", prov.FieldSources["title"])
	assert.Equal(t, "javlibrary", prov.FieldSources["display_title"])
}

func TestApplyFieldOverride_ActressesRebuildsActressSources(t *testing.T) {
	movie, prov := overrideFixture()
	// Pre-existing attribution from r18dev should be replaced.
	require.NotEmpty(t, prov.ActressSources)

	err := applyFieldOverride(movie, prov, "actresses", "dmm")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
	assert.Equal(t, "dmm", prov.FieldSources["actresses"])
	// The single DMM actress (DMMID 555) is now attributed to dmm.
	assert.Equal(t, "dmm", prov.ActressSources["dmmid:555"])
	// r18dev attribution is gone (list was replaced).
	for _, src := range prov.ActressSources {
		assert.NotEqual(t, "r18dev", src)
	}
}

func TestApplyFieldOverride_GenresAndScreenshots(t *testing.T) {
	movie, prov := overrideFixture()
	err := applyFieldOverride(movie, prov, "genres", "dmm")
	require.NoError(t, err)
	assert.Equal(t, []models.Genre{{Name: "Drama"}, {Name: "Romance"}}, movie.Genres)
	assert.Equal(t, "dmm", prov.FieldSources["genres"])

	err = applyFieldOverride(movie, prov, "screenshot_urls", "dmm")
	require.NoError(t, err)
	assert.Equal(t, []string{"dmm-shot-1", "dmm-shot-2"}, movie.Screenshots)
}

func TestApplyFieldOverride_ReleaseDateSetsYear(t *testing.T) {
	movie, prov := overrideFixture()
	err := applyFieldOverride(movie, prov, "release_date", "dmm")
	require.NoError(t, err)
	require.NotNil(t, movie.ReleaseDate)
	assert.Equal(t, 2021, movie.ReleaseYear)
	assert.Equal(t, "dmm", prov.FieldSources["release_date"])
}

func TestApplyFieldOverride_UnknownSource(t *testing.T) {
	movie, prov := overrideFixture()
	err := applyFieldOverride(movie, prov, "maker", "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not contribute")
}

func TestApplyFieldOverride_UnsupportedField(t *testing.T) {
	movie, prov := overrideFixture()
	err := applyFieldOverride(movie, prov, "bogus_field", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported field")
}

func TestApplyFieldOverride_NilProvenance(t *testing.T) {
	err := applyFieldOverride(&models.Movie{}, nil, "maker", "dmm")
	require.Error(t, err)
}

func TestApplyFieldOverride_NoScraperResults(t *testing.T) {
	movie := &models.Movie{}
	prov := &ProvenanceData{}
	err := applyFieldOverride(movie, prov, "maker", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not contribute")
}

func TestApplyFieldOverride_SynthesizedSourceFallback(t *testing.T) {
	movie := &models.Movie{ID: "ABC-001", Title: "Cached Title", Maker: "Cached Maker"}
	prov := &ProvenanceData{}
	err := applyFieldOverride(movie, prov, "maker", "scraper")
	require.NoError(t, err)
	assert.Equal(t, "Cached Maker", movie.Maker)
	assert.Equal(t, "scraper", prov.FieldSources["maker"])
}

func overrideFixture() (*models.Movie, *ProvenanceData) {
	dmmDate := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	prov := &ProvenanceData{
		FieldSources:   map[string]string{"maker": "r18dev", "actresses": "r18dev"},
		ActressSources: map[string]string{"name:yuihatano": "r18dev"},
		ScraperResults: []*models.ScraperResult{
			{
				Source:        "r18dev",
				Title:         "R18 Title",
				Maker:         "R18 Maker",
				Genres:        []string{"HD"},
				ScreenshotURL: []string{"r18-shot"},
				Actresses:     []models.ActressInfo{{FirstName: "R18", LastName: "Actress"}},
			},
			{
				Source:           "dmm",
				ID:               "dmm-id",
				ContentID:        "dmm-content-id",
				Title:            "DMM Title",
				OriginalTitle:    "DMM Original Title",
				Description:      "DMM Description",
				Maker:            "DMM Studio",
				Label:            "DMM Label",
				Series:           "DMM Series",
				Director:         "DMM Director",
				ReleaseDate:      &dmmDate,
				Runtime:          120,
				Genres:           []string{"Drama", "Romance"},
				ScreenshotURL:    []string{"dmm-shot-1", "dmm-shot-2"},
				Actresses:        []models.ActressInfo{{DMMID: 555, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}},
				Rating:           &models.Rating{Score: 8.5, Votes: 42},
				PosterURL:        "dmm-poster-url",
				CoverURL:         "dmm-cover-url",
				TrailerURL:       "dmm-trailer-url",
				ShouldCropPoster: true,
			},
			{
				Source: "javlibrary",
				Title:  "JavLibrary Title",
			},
		},
	}
	movie := &models.Movie{
		ID:           "orig-id",
		Title:        "Orig Title",
		DisplayTitle: "Orig Title",
		Maker:        "Orig Maker",
		Actresses:    []models.Actress{{FirstName: "Orig"}},
	}
	return movie, prov
}

func TestScrapeResultToMovieResult_RetainsScraperResults(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "f.mp4", MovieID: "IPX-535"}
	result := &scrape.ScrapeResult{
		Movie:        &models.Movie{ID: "IPX-535", Maker: "Aggregated"},
		FieldSources: map[string]string{"maker": "r18dev"},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", Maker: "R18"},
			{Source: "dmm", Maker: "DMM"},
		},
	}
	mr, prov := scrapeResultToMovieResult(fmi, result, nil)
	require.NotNil(t, mr)
	require.NotNil(t, prov)
	assert.Len(t, prov.ScraperResults, 2)
	assert.Equal(t, "dmm", prov.ScraperResults[1].Source)
}

func TestScrapeResultToMovieResult_NoScraperResults(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "f.mp4"}
	result := &scrape.ScrapeResult{
		Movie:        &models.Movie{ID: "IPX-535"},
		FieldSources: map[string]string{"maker": "r18dev"},
	}
	mr, prov := scrapeResultToMovieResult(fmi, result, nil)
	require.NotNil(t, mr)
	require.NotNil(t, prov)
	assert.Nil(t, prov.ScraperResults)
}

func TestProvenanceData_Clone_DeepCopiesScraperResults(t *testing.T) {
	date := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	orig := &ProvenanceData{
		FieldSources: map[string]string{"maker": "dmm"},
		ScraperResults: []*models.ScraperResult{
			{Source: "dmm", Maker: "DMM", ReleaseDate: &date, Genres: []string{"Drama"}},
		},
	}
	clone := orig.Clone()
	require.NotNil(t, clone)
	require.Len(t, clone.ScraperResults, 1)

	clone.ScraperResults[0].Maker = "CHANGED"
	clone.ScraperResults[0].Genres[0] = "CHANGED"
	*clone.ScraperResults[0].ReleaseDate = time.Time{}

	assert.Equal(t, "DMM", orig.ScraperResults[0].Maker)
	assert.Equal(t, "Drama", orig.ScraperResults[0].Genres[0])
	assert.Equal(t, date, *orig.ScraperResults[0].ReleaseDate)
}

func TestApplyFieldOverride_AllFields(t *testing.T) {
	for _, key := range SupportedFieldOverrideKeys() {
		t.Run(key, func(t *testing.T) {
			movie, prov := overrideFixture()
			err := applyFieldOverride(movie, prov, key, "dmm")
			require.NoError(t, err)
			assert.Equal(t, "dmm", prov.FieldSources[key],
				"FieldSources[%s] should be dmm after override", key)

			dmm := findScraperResult(prov.ScraperResults, "dmm")
			require.NotNil(t, dmm)

			switch key {
			case "id":
				assert.Equal(t, dmm.ID, movie.ID)
			case "content_id":
				assert.Equal(t, dmm.ContentID, movie.ContentID)
			case "title", "display_title":
				assert.Equal(t, dmm.Title, movie.Title)
				assert.Equal(t, dmm.Title, movie.DisplayTitle)
			case "original_title":
				assert.Equal(t, dmm.OriginalTitle, movie.OriginalTitle)
			case "description":
				assert.Equal(t, dmm.Description, movie.Description)
			case "director":
				assert.Equal(t, dmm.Director, movie.Director)
			case "maker":
				assert.Equal(t, dmm.Maker, movie.Maker)
			case "label":
				assert.Equal(t, dmm.Label, movie.Label)
			case "series":
				assert.Equal(t, dmm.Series, movie.Series)
			case "runtime":
				assert.Equal(t, dmm.Runtime, movie.Runtime)
			case "release_date":
				require.NotNil(t, movie.ReleaseDate)
				assert.Equal(t, *dmm.ReleaseDate, *movie.ReleaseDate)
				assert.Equal(t, dmm.ReleaseDate.Year(), movie.ReleaseYear)
			case "rating_score":
				assert.Equal(t, dmm.Rating.Score, movie.RatingScore)
			case "rating_votes":
				assert.Equal(t, dmm.Rating.Votes, movie.RatingVotes)
			case "actresses":
				require.Len(t, movie.Actresses, 1)
				assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
				assert.Equal(t, "dmm", prov.ActressSources["dmmid:555"])
			case "genres":
				require.Len(t, movie.Genres, 2)
				assert.Equal(t, "Drama", movie.Genres[0].Name)
			case "screenshot_urls":
				assert.Equal(t, []string{"dmm-shot-1", "dmm-shot-2"}, movie.Screenshots)
			case "poster_url":
				assert.Equal(t, dmm.PosterURL, movie.Poster.PosterURL)
			case "cover_url":
				assert.Equal(t, dmm.CoverURL, movie.Poster.CoverURL)
			case "trailer_url":
				assert.Equal(t, dmm.TrailerURL, movie.TrailerURL)
			case "should_crop_poster":
				assert.Equal(t, dmm.ShouldCropPoster, movie.Poster.ShouldCropPoster)
			}
		})
	}
}
