package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	prov := &resultstore.ProvenanceData{}
	err := applyFieldOverride(movie, prov, "maker", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not contribute")
}

func TestApplyFieldOverride_SynthesizedSourceFallback(t *testing.T) {
	movie := &models.Movie{ID: "ABC-001", Title: "Cached Title", Maker: "Cached Maker"}
	prov := &resultstore.ProvenanceData{}
	err := applyFieldOverride(movie, prov, "maker", "scraper")
	require.NoError(t, err)
	assert.Equal(t, "Cached Maker", movie.Maker)
	assert.Equal(t, "scraper", prov.FieldSources["maker"])
}

func TestApplyFieldOverride_PosterSourceFieldsClearCropBounds(t *testing.T) {
	for _, field := range []string{"poster_url", "cover_url", "should_crop_poster"} {
		t.Run(field, func(t *testing.T) {
			movie, prov := overrideFixture()
			movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}
			err := applyFieldOverride(movie, prov, field, "dmm")
			require.NoError(t, err)
			assert.Nil(t, movie.Poster.CropBounds,
				"overriding %s changes the poster source/decision; stale bounds would silently miscrop the new image at apply", field)
		})
	}
}

func TestApplyFieldOverride_SamePosterURLKeepsCropBounds(t *testing.T) {
	movie, prov := overrideFixture()
	movie.Poster.PosterURL = "dmm-poster-url" // equals the selected source's URL
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	require.NoError(t, applyFieldOverride(movie, prov, "poster_url", "dmm"))
	assert.NotNil(t, movie.Poster.CropBounds,
		"selecting a source with the identical poster URL must not discard a still-valid crop")
}

func TestApplyFieldOverride_CoverOnlyChangeKeepsBoundsWhenPosterURLSet(t *testing.T) {
	movie, prov := overrideFixture()
	movie.Poster.PosterURL = "https://example.com/poster.jpg" // cover is not the poster source
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	require.NoError(t, applyFieldOverride(movie, prov, "cover_url", "dmm"))
	assert.NotNil(t, movie.Poster.CropBounds,
		"downloadPoster reads PosterURL when set — a cover-only override must not discard the crop")
}

func TestApplyFieldOverride_UnrelatedFieldKeepsCropBounds(t *testing.T) {
	movie, prov := overrideFixture()
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}
	require.NoError(t, applyFieldOverride(movie, prov, "title", "javlibrary"))
	require.NotNil(t, movie.Poster.CropBounds, "non-poster overrides must not discard the user's crop")
}

func overrideFixture() (*models.Movie, *resultstore.ProvenanceData) {
	dmmDate := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	prov := &resultstore.ProvenanceData{
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
	mr, prov := scrapeResultToMovieResult(fmi, result, nil, false)
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
	mr, prov := scrapeResultToMovieResult(fmi, result, nil, false)
	require.NotNil(t, mr)
	require.NotNil(t, prov)
	assert.Nil(t, prov.ScraperResults)
}

func TestProvenanceData_Clone_DeepCopiesScraperResults(t *testing.T) {
	date := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	orig := &resultstore.ProvenanceData{
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

func TestApplyFieldOverride_ConcurrentDifferentFieldsSameResult(t *testing.T) {
	for iter := 0; iter < 50; iter++ {
		movie, prov := overrideFixture()
		filePath := "test.mp4"
		resultID := "res-001"

		tracker := resultstore.New(1, []string{filePath})
		mr := &resultstore.MovieResult{
			ResultID:      resultID,
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movie.ID},
			Movie:         movie,
			Status:        models.JobStatusCompleted,
		}
		tracker.UpdateFileResult(filePath, mr)
		tracker.SetProvenance(filePath, prov)

		je := &jobEditorImpl{
			store: tracker,
		}

		var wg sync.WaitGroup
		ready := make(chan struct{})
		errs := make([]error, 2)

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-ready
			_, _, errs[0] = je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
		}()
		go func() {
			defer wg.Done()
			<-ready
			_, _, errs[1] = je.ApplyFieldOverride(context.Background(), resultID, "title", "r18dev")
		}()
		close(ready)
		wg.Wait()

		require.NoError(t, errs[0], "maker override failed on iter %d", iter)
		require.NoError(t, errs[1], "title override failed on iter %d", iter)

		final, _, ok := tracker.GetFileResultByResultID(resultID)
		require.True(t, ok)
		require.NotNil(t, final.Movie)
		assert.Equal(t, "DMM Studio", final.Movie.Maker, "maker override lost on iter %d", iter)
		assert.Equal(t, "R18 Title", final.Movie.Title, "title override lost on iter %d", iter)

		finalProv := tracker.GetProvenance(filePath)
		require.NotNil(t, finalProv)
		assert.Equal(t, "dmm", finalProv.FieldSources["maker"], "maker provenance lost on iter %d", iter)
		assert.Equal(t, "r18dev", finalProv.FieldSources["title"], "title provenance lost on iter %d", iter)
	}
}

func TestApplyFieldOverride_NilMovie(t *testing.T) {
	_, prov := overrideFixture()
	prov = &resultstore.ProvenanceData{ScraperResults: prov.ScraperResults}
	err := applyFieldOverride(nil, prov, "maker", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil movie")
}

func TestApplyFieldOverride_NilReleaseDateClearsYear(t *testing.T) {
	movie, prov := overrideFixture()
	movie.ReleaseYear = 1999
	dmm := findScraperResult(prov.ScraperResults, "dmm")
	require.NotNil(t, dmm)
	dmm.ReleaseDate = nil
	err := applyFieldOverride(movie, prov, "release_date", "dmm")
	require.NoError(t, err)
	assert.Nil(t, movie.ReleaseDate)
	assert.Equal(t, 0, movie.ReleaseYear)
}

func TestApplyFieldOverride_NilRatingScoresDefaultZero(t *testing.T) {
	movie, prov := overrideFixture()
	dmm := findScraperResult(prov.ScraperResults, "dmm")
	require.NotNil(t, dmm)
	dmm.Rating = nil
	require.NoError(t, applyFieldOverride(movie, prov, "rating_score", "dmm"))
	assert.Equal(t, float64(0), movie.RatingScore)
	require.NoError(t, applyFieldOverride(movie, prov, "rating_votes", "dmm"))
	assert.Equal(t, 0, movie.RatingVotes)
}

func TestApplyFieldOverride_EmptyActressListClearsSources(t *testing.T) {
	movie, prov := overrideFixture()
	dmm := findScraperResult(prov.ScraperResults, "dmm")
	dmm.Actresses = nil
	require.NoError(t, applyFieldOverride(movie, prov, "actresses", "dmm"))
	assert.Nil(t, movie.Actresses)
	assert.Nil(t, prov.ActressSources)
}

func TestApplyFieldOverride_EmptyGenreList(t *testing.T) {
	movie, prov := overrideFixture()
	dmm := findScraperResult(prov.ScraperResults, "dmm")
	dmm.Genres = nil
	require.NoError(t, applyFieldOverride(movie, prov, "genres", "dmm"))
	assert.Nil(t, movie.Genres)
}

func TestApplyFieldOverride_UnhandledKeyErrors(t *testing.T) {
	_, ok := fieldOverrideKeys["bogus"]
	require.False(t, ok)
}

func TestRebuildActressSources_EmptyKeySkipped(t *testing.T) {
	prov := &resultstore.ProvenanceData{}
	rebuildActressSources(prov, []models.Actress{{FirstName: "", LastName: "", JapaneseName: "", DMMID: 0}}, "dmm")
	assert.Nil(t, prov.ActressSources, "actress with no identifying fields yields no source keys")
}

func TestRebuildActressSources_EmptyListClears(t *testing.T) {
	prov := &resultstore.ProvenanceData{ActressSources: map[string]string{"name:x": "dmm"}}
	rebuildActressSources(prov, nil, "dmm")
	assert.Nil(t, prov.ActressSources)
}

func TestApplyFieldOverride_ResultNotFound(t *testing.T) {
	tracker := resultstore.New(1, []string{"x.mp4"})
	je := &jobEditorImpl{store: tracker}
	_, _, err := je.ApplyFieldOverride(context.Background(), "nope", "maker", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestApplyFieldOverride_BogusSourceErrors(t *testing.T) {
	movie, prov := overrideFixture()
	filePath := "test.mp4"
	resultID := "res-001"
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID: resultID, FileMatchInfo: models.FileMatchInfo{Path: filePath}, Movie: movie, Status: models.JobStatusCompleted,
	})
	tracker.SetProvenance(filePath, prov)
	je := &jobEditorImpl{store: tracker}
	_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "nonexistent-source")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not contribute")
}

func TestApplyFieldOverride_NilProvenanceUsesSynthesizedFallback(t *testing.T) {
	movie, _ := overrideFixture()
	filePath := "test.mp4"
	resultID := "res-001"
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID: resultID, FileMatchInfo: models.FileMatchInfo{Path: filePath}, Movie: movie, Status: models.JobStatusCompleted,
	})
	je := &jobEditorImpl{store: tracker}
	res, prov, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "scraper")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, prov)
	assert.Equal(t, "scraper", prov.FieldSources["maker"])
}

func TestApplyFieldOverride_UnregisteredKeyHitsDefault(t *testing.T) {
	movie, prov := overrideFixture()
	fieldOverrideKeys["__test_no_case__"] = struct{}{}
	defer delete(fieldOverrideKeys, "__test_no_case__")
	err := applyFieldOverride(movie, prov, "__test_no_case__", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unhandled field")
}

func TestUpdateMovie_DBPersistError(t *testing.T) {
	movie, _ := overrideFixture()
	filePath := "test.mp4"
	tracker := resultstore.New(1, []string{filePath})
	repo := mocks.NewMockMovieRepositoryInterface(t)
	repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
	je := &jobEditorImpl{store: tracker, movieRepo: repo}
	err := je.UpdateMovie(context.Background(), filePath, movie)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist movie update")
}

func TestApplyFieldOverride_PersistErrorWrapped(t *testing.T) {
	movie, _ := overrideFixture()
	filePath := "test.mp4"
	resultID := "res-001"
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID: resultID, FileMatchInfo: models.FileMatchInfo{Path: filePath}, Movie: movie, Status: models.JobStatusCompleted,
	})
	repo := mocks.NewMockMovieRepositoryInterface(t)
	repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
	je := &jobEditorImpl{store: tracker, movieRepo: repo}
	_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "scraper")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist field override")
}

// TestApplyFieldOverride_PosterURLSyncsCropIntent pins Finding B at the
// field-override path: when a poster_url override replaces the effective
// poster source, ShouldCropPoster must describe the NEW source. The
// SELECTED source's own decision travels with its image: a source that
// serves a landscape cover AS its poster (javdb/mgstage shape:
// PosterURL == CoverURL, ShouldCropPoster=true) keeps its crop intent, so
// the auto-cropped review preview and Organize's final poster agree. A
// poster-grade source (intent=false) conversely clears a cover-backed
// movie's stale intent — otherwise downloadPoster default-crops the new
// poster wholesale at Organize, and a subsequent manual crop records
// CropBounds.SourceWasCover=true so the apply-time geometry fallback
// degrades to the default cover crop instead of keeping the poster whole
// (internal/downloader/media.go). Only when the source's intent describes
// a different image does the sync fall back to URL-field classification
// (clearing the override back to the cover restores cover-backed
// semantics). The cover_url cases pin the sibling fix: when the cover
// becomes the effective poster source again, a manual crop's
// ShouldCropPoster=false does not survive onto the new image.
func TestApplyFieldOverride_PosterURLSyncsCropIntent(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(movie *models.Movie, dmm *models.ScraperResult)
		field      string
		wantCrop   bool
		wantPoster string // expected movie.Poster.PosterURL after the override
	}{
		{
			name: "cover-backed movie picking a poster-grade URL drops the cover intent",
			setup: func(movie *models.Movie, dmm *models.ScraperResult) {
				movie.Poster.PosterURL = ""
				movie.Poster.CoverURL = "https://old.example/cover.jpg"
				movie.Poster.ShouldCropPoster = true // cover-backed at scrape time
				dmm.ShouldCropPoster = false         // the selected source serves a poster-grade image
			},
			field:      "poster_url",
			wantCrop:   false,
			wantPoster: "dmm-poster-url",
		},
		{
			name: "stale cover intent on a poster swap is cleared",
			setup: func(movie *models.Movie, dmm *models.ScraperResult) {
				movie.Poster.PosterURL = "https://old.example/poster.jpg"
				movie.Poster.CoverURL = "https://old.example/cover.jpg"
				movie.Poster.ShouldCropPoster = true // stale: already describes a poster source
				dmm.ShouldCropPoster = false
			},
			field:      "poster_url",
			wantCrop:   false,
			wantPoster: "dmm-poster-url",
		},
		{
			// javdb/mgstage output shape: PosterURL is populated FROM the
			// landscape CoverURL and flagged ShouldCropPoster=true. The intent
			// must survive the override or Organize writes the landscape image
			// whole while the review preview showed it cropped.
			name: "cover-derived poster URL keeps the selected source's crop intent",
			setup: func(movie *models.Movie, dmm *models.ScraperResult) {
				movie.Poster.PosterURL = "https://old.example/poster.jpg"
				movie.Poster.ShouldCropPoster = false
				dmm.CoverURL = dmm.PosterURL // the selected source's poster IS its landscape cover
				// dmm.ShouldCropPoster stays true (fixture default)
			},
			field:      "poster_url",
			wantCrop:   true,
			wantPoster: "dmm-poster-url",
		},
		{
			name: "clearing the poster URL falls back to cover-backed semantics",
			setup: func(movie *models.Movie, dmm *models.ScraperResult) {
				movie.Poster.PosterURL = "https://old.example/poster.jpg"
				movie.Poster.CoverURL = "https://old.example/cover.jpg"
				movie.Poster.ShouldCropPoster = false
				dmm.PosterURL = "" // the chosen source contributes no poster: back to the cover
			},
			field:      "poster_url",
			wantCrop:   true,
			wantPoster: "",
		},
		{
			name: "clearing with neither URL leaves the intent untouched",
			setup: func(movie *models.Movie, dmm *models.ScraperResult) {
				movie.Poster.PosterURL = "https://old.example/poster.jpg"
				movie.Poster.CoverURL = ""
				movie.Poster.ShouldCropPoster = false
				dmm.PosterURL = ""
			},
			field:      "poster_url",
			wantCrop:   false,
			wantPoster: "",
		},
		{
			name: "identical poster URL is a no-op that keeps both bounds and intent",
			setup: func(movie *models.Movie, _ *models.ScraperResult) {
				movie.Poster.PosterURL = "dmm-poster-url" // equals the selected source's URL
				movie.Poster.CoverURL = "https://old.example/cover.jpg"
				movie.Poster.ShouldCropPoster = true // a deliberate decision must not be clobbered
			},
			field:      "poster_url",
			wantCrop:   true,
			wantPoster: "dmm-poster-url",
		},
		{
			name: "cover_url override behind an explicit poster leaves the intent alone",
			setup: func(movie *models.Movie, _ *models.ScraperResult) {
				movie.Poster.PosterURL = "https://old.example/poster.jpg"
				movie.Poster.CoverURL = "https://old.example/cover.jpg"
				movie.Poster.ShouldCropPoster = false
			},
			field:      "cover_url",
			wantCrop:   false,
			wantPoster: "https://old.example/poster.jpg",
		},
		{
			name: "cover_url override on a cover-backed movie keeps cover intent",
			setup: func(movie *models.Movie, _ *models.ScraperResult) {
				movie.Poster.PosterURL = ""
				movie.Poster.CoverURL = "https://old.example/cover.jpg"
				movie.Poster.ShouldCropPoster = true
			},
			field:      "cover_url",
			wantCrop:   true,
			wantPoster: "",
		},
		{
			// The source's recorded intent describes its own poster URL, NOT
			// the cover adopted here, so the URL-class fallback applies: the
			// new cover feeds the poster pipeline and must be cropped.
			name: "cover_url override after a manual crop re-establishes cover cropping",
			setup: func(movie *models.Movie, dmm *models.ScraperResult) {
				movie.Poster.PosterURL = ""
				movie.Poster.CoverURL = "https://old.example/cover.jpg"
				movie.Poster.ShouldCropPoster = false // the manual crop decision on the OLD cover
				dmm.ShouldCropPoster = false          // describes dmm-poster-url, not the cover
			},
			field:      "cover_url",
			wantCrop:   true,
			wantPoster: "",
		},
		{
			// mgstage shape: the selected source's poster IS its cover, so its
			// crop intent describes the very image adopted here.
			name: "cover_url override adopts the selected source's intent for the same image",
			setup: func(movie *models.Movie, dmm *models.ScraperResult) {
				movie.Poster.PosterURL = ""
				movie.Poster.CoverURL = "https://old.example/cover.jpg"
				movie.Poster.ShouldCropPoster = false // the manual crop decision on the OLD cover
				dmm.PosterURL = dmm.CoverURL          // poster populated from the cover
				// dmm.ShouldCropPoster stays true (fixture default)
			},
			field:      "cover_url",
			wantCrop:   true,
			wantPoster: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			movie, prov := overrideFixture()
			dmm := findScraperResult(prov.ScraperResults, "dmm")
			require.NotNil(t, dmm)
			movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}
			tc.setup(movie, dmm)

			require.NoError(t, applyFieldOverride(movie, prov, tc.field, "dmm"))

			assert.Equal(t, tc.wantPoster, movie.Poster.PosterURL)
			assert.Equal(t, tc.wantCrop, movie.Poster.ShouldCropPoster,
				"the crop intent must describe the effective poster source, not the pre-edit image")
		})
	}
}
