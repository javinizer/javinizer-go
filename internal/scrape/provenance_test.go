package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraperResultFromCachedMovie_NilReturnsNil(t *testing.T) {
	assert.Nil(t, ScraperResultFromCachedMovie(nil))
}

func TestScraperResultFromCachedMovie_MinimalMovie(t *testing.T) {
	r := ScraperResultFromCachedMovie(&models.Movie{ID: "ABC-1", Title: "T"})
	require.NotNil(t, r)
	assert.Equal(t, "scraper", r.Source, "empty SourceName falls back to 'scraper'")
	assert.Nil(t, r.Actresses, "no actresses -> nil")
	assert.Nil(t, r.Rating, "no rating -> nil")
	assert.Nil(t, r.ScreenshotURL)
	assert.Nil(t, r.Genres)
	assert.Equal(t, "ABC-1", r.ID)
	assert.Equal(t, "T", r.Title)
}

func TestScraperResultFromCachedMovie_FullMovie(t *testing.T) {
	m := &models.Movie{
		ID:          "ABC-1",
		Title:       "T",
		SourceName:  "dmm",
		SourceURL:   "http://dmm",
		ContentID:   "abc",
		Description: "desc",
		Runtime:     120,
		Director:    "Dir",
		Maker:       "Mkr",
		Label:       "Lbl",
		Series:      "Ser",
		Actresses:   []models.Actress{{DMMID: 5, FirstName: "Yui", JapaneseName: "波多野結衣", ThumbURL: "http://t"}},
		Genres:      []models.Genre{{Name: "Drama"}, {Name: "Romance"}},
		Screenshots: []string{"s1.jpg", "s2.jpg"},
		RatingScore: 8.5,
		RatingVotes: 42,
		Poster:      models.PosterState{PosterURL: "p.jpg", CoverURL: "c.jpg", ShouldCropPoster: true},
		TrailerURL:  "tr.mp4",
	}
	r := ScraperResultFromCachedMovie(m)
	require.NotNil(t, r)
	assert.Equal(t, "dmm", r.Source)
	assert.Equal(t, "abc", r.ContentID)
	assert.Equal(t, "desc", r.Description)
	assert.Equal(t, "p.jpg", r.PosterURL)
	assert.Equal(t, "c.jpg", r.CoverURL)
	assert.True(t, r.ShouldCropPoster)
	assert.Equal(t, []string{"s1.jpg", "s2.jpg"}, r.ScreenshotURL)
	assert.Equal(t, []string{"Drama", "Romance"}, r.Genres)
	require.Len(t, r.Actresses, 1)
	assert.Equal(t, "Yui", r.Actresses[0].FirstName)
	assert.Equal(t, "波多野結衣", r.Actresses[0].JapaneseName)
	require.NotNil(t, r.Rating)
	assert.Equal(t, 8.5, r.Rating.Score)
	assert.Equal(t, 42, r.Rating.Votes)
}

func TestScraperResultFromCachedMovie_RatingOnlyScore(t *testing.T) {
	r := ScraperResultFromCachedMovie(&models.Movie{RatingScore: 7})
	require.NotNil(t, r)
	require.NotNil(t, r.Rating)
	assert.Equal(t, float64(7), r.Rating.Score)
	assert.Equal(t, 0, r.Rating.Votes)
}

func TestGenreNamesFromModel(t *testing.T) {
	assert.Nil(t, genreNamesFromModel(nil))
	assert.Nil(t, genreNamesFromModel([]models.Genre{}))
	assert.Equal(t, []string{"A", "B"}, genreNamesFromModel([]models.Genre{{Name: "A"}, {Name: "B"}}))
}

func TestBuildActressSourcesFromCachedMovie_EmptyKeySkipped(t *testing.T) {
	m := &models.Movie{SourceName: "dmm", Actresses: []models.Actress{{}}}
	assert.Nil(t, buildActressSourcesFromCachedMovie(m), "actress with no identifying fields yields no keys")
}

func TestBuildActressSourcesFromCachedMovie_EmptySourceNameFallsBack(t *testing.T) {
	m := &models.Movie{Actresses: []models.Actress{{DMMID: 7}}}
	sources := buildActressSourcesFromCachedMovie(m)
	require.NotNil(t, sources)
	assert.Equal(t, "scraper", sources["dmmid:7"])
}

func TestActressSourceKey_AllBranches(t *testing.T) {
	assert.Equal(t, "dmmid:5", ActressSourceKey(models.Actress{DMMID: 5}))
	assert.Equal(t, "name:波多野結衣", ActressSourceKey(models.Actress{JapaneseName: "波多野結衣"}))
	assert.Equal(t, "name:yui hatano", ActressSourceKey(models.Actress{FirstName: "Yui", LastName: "Hatano"}))
	assert.Equal(t, "name:hatano", ActressSourceKey(models.Actress{LastName: "Hatano"}), "first+last empty -> falls through to last+first")
	assert.Equal(t, "", ActressSourceKey(models.Actress{}), "no identifying fields -> empty key")
}
