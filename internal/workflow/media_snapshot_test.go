package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func actressNamesMatch(a, b models.Actress) bool {
	return actressJapaneseNameMatch(a, b) || actressRomanizedNameMatch(a, b)
}

func TestSnapshotScrapedMedia_DeepCopiesURLCollections(t *testing.T) {
	movie := &models.Movie{
		Poster:      models.PosterState{CoverURL: "cover", PosterURL: "poster", CroppedPosterURL: "cropped", ShouldCropPoster: true},
		TrailerURL:  "trailer",
		Screenshots: []string{"shot-1"},
		Actresses:   []models.Actress{{JapaneseName: "女優", ThumbURL: "thumb", Translations: []models.ActressTranslation{{DisplayName: "name"}}}},
	}
	snapshot := snapshotScrapedMedia(movie)
	movie.Screenshots[0] = "changed"
	movie.Actresses[0].ThumbURL = "changed"
	movie.Actresses[0].Translations[0].DisplayName = "changed"

	require.NotNil(t, snapshot)
	assert.Equal(t, "shot-1", snapshot.Screenshots[0])
	assert.Equal(t, "thumb", snapshot.Actresses[0].ThumbURL)
	assert.Equal(t, "name", snapshot.Actresses[0].Translations[0].DisplayName)
	assert.Equal(t, "cover", snapshot.CoverURL)
	assert.True(t, snapshot.ShouldCropPoster)
}

func TestScrapedMediaSnapshot_OverlayKeepsMergedMetadataAndUsesScrapedURLs(t *testing.T) {
	merged := &models.Movie{
		ID:          "TEST-001",
		Title:       "Merged title",
		Poster:      models.PosterState{CoverURL: "old-cover", PosterURL: "old-poster", CroppedPosterURL: "old-cropped"},
		TrailerURL:  "old-trailer",
		Screenshots: []string{"old-shot"},
		Actresses: []models.Actress{
			{JapaneseName: "桜花", FirstName: "Merged", LastName: "Name", ThumbURL: "old-thumb"},
			{JapaneseName: "NFOのみ", ThumbURL: "old-nfo-thumb"},
		},
	}
	snapshot := &scrapedMediaSnapshot{
		CoverURL:         "new-cover",
		PosterURL:        "new-poster",
		CroppedPosterURL: "new-cropped",
		ShouldCropPoster: true,
		TrailerURL:       "new-trailer",
		Screenshots:      []string{"new-shot"},
		Actresses: []models.Actress{
			{JapaneseName: "桜花", FirstName: "Scraped", LastName: "Name", ThumbURL: "new-thumb"},
			{JapaneseName: "新規", FirstName: "Fresh", LastName: "Only", ThumbURL: "fresh-thumb"},
		},
	}

	downloadMovie := snapshot.overlay(merged)
	require.NotNil(t, downloadMovie)
	assert.Equal(t, "TEST-001", downloadMovie.ID)
	assert.Equal(t, "Merged title", downloadMovie.Title)
	assert.Equal(t, "new-cover", downloadMovie.Poster.CoverURL)
	assert.Equal(t, "new-poster", downloadMovie.Poster.PosterURL)
	assert.Equal(t, "new-cropped", downloadMovie.Poster.CroppedPosterURL)
	assert.True(t, downloadMovie.Poster.ShouldCropPoster)
	assert.Equal(t, "new-trailer", downloadMovie.TrailerURL)
	assert.Equal(t, []string{"new-shot"}, downloadMovie.Screenshots)
	assert.Equal(t, "Merged", downloadMovie.Actresses[0].FirstName)
	assert.Equal(t, "Name", downloadMovie.Actresses[0].LastName)
	assert.Equal(t, "new-thumb", downloadMovie.Actresses[0].ThumbURL)
	assert.Empty(t, downloadMovie.Actresses[1].ThumbURL)
	require.Len(t, downloadMovie.Actresses, 3)
	assert.Equal(t, "Fresh", downloadMovie.Actresses[2].FirstName)
	assert.Equal(t, "Only", downloadMovie.Actresses[2].LastName)
	assert.Equal(t, "fresh-thumb", downloadMovie.Actresses[2].ThumbURL)
}

func TestOverlayActressThumbs_UsesDMMIDForSameNameCandidates(t *testing.T) {
	merged := []models.Actress{{JapaneseName: "同名", DMMID: 2}}
	scraped := []models.Actress{
		{JapaneseName: "同名", DMMID: 1, ThumbURL: "wrong"},
		{JapaneseName: "同名", DMMID: 2, ThumbURL: "right"},
	}
	result := overlayActressThumbs(merged, scraped)
	require.Len(t, result, 2)
	assert.Equal(t, "right", result[0].ThumbURL)
	assert.Equal(t, 1, result[1].DMMID)
	assert.Equal(t, "wrong", result[1].ThumbURL)
	result[1].ThumbURL = "mutated"
	assert.Equal(t, "wrong", scraped[0].ThumbURL)
}

func TestOverlayActressThumbs_PrefersJapaneseNameOverRomanizedDMMMatch(t *testing.T) {
	merged := []models.Actress{{JapaneseName: "桜花", FirstName: "Same", LastName: "Name", DMMID: 2}}
	scraped := []models.Actress{
		{JapaneseName: "桜花", DMMID: 1, ThumbURL: "japanese"},
		{JapaneseName: "別名", FirstName: "Same", LastName: "Name", DMMID: 2, ThumbURL: "romanized"},
	}

	result := overlayActressThumbs(merged, scraped)
	require.Len(t, result, 2)
	assert.Equal(t, "japanese", result[0].ThumbURL)
	assert.Equal(t, "romanized", result[1].ThumbURL)
}

type capturingDownloader struct {
	cmd     downloader.DownloadCmd
	outcome *downloader.DownloadOutcome
}

func (d *capturingDownloader) Download(_ context.Context, cmd downloader.DownloadCmd) (*downloader.DownloadOutcome, error) {
	d.cmd = cmd
	return d.outcome, nil
}

func TestStepDownload_OverwriteOverlaysScrapedMediaOntoMergedMovie(t *testing.T) {
	merged := &models.Movie{
		ID:          "TEST-002",
		Title:       "Merged",
		Poster:      models.PosterState{CoverURL: "old-cover", PosterURL: "old-poster"},
		TrailerURL:  "old-trailer",
		Screenshots: []string{"old-shot"},
		Actresses:   []models.Actress{{JapaneseName: "女優", FirstName: "Merged", ThumbURL: "old-thumb"}},
	}
	scraped := &models.Movie{
		ID:          "TEST-002",
		Poster:      models.PosterState{CoverURL: "new-cover", PosterURL: "new-poster"},
		TrailerURL:  "new-trailer",
		Screenshots: []string{"new-shot"},
		Actresses:   []models.Actress{{JapaneseName: "女優", FirstName: "Scraped", ThumbURL: "new-thumb"}},
	}
	dedup := &sync.Map{}
	capture := &capturingDownloader{outcome: &downloader.DownloadOutcome{
		DownloadedPaths: []string{"overwritten"},
		CreatedPaths:    []string{"created"},
	}}
	o := &applyOrchImpl{downloader: capture}
	state := &applyPipelineState{movie: merged, finalDir: "/merged-destination"}
	cmd := ApplyCmd{Movie: scraped, Download: true, OverwriteExistingMedia: true, Dedup: dedup}

	require.NoError(t, o.stepMerge(cmd, state, &stepCompletion{}))
	require.NoError(t, o.stepDownload(context.Background(), cmd, "op-test", state, &stepCompletion{}))
	require.NotNil(t, capture.cmd.Movie)
	assert.Equal(t, "/merged-destination", capture.cmd.DestDir)
	assert.Equal(t, "new-cover", capture.cmd.Movie.Poster.CoverURL)
	assert.Equal(t, "new-poster", capture.cmd.Movie.Poster.PosterURL)
	assert.Equal(t, "new-trailer", capture.cmd.Movie.TrailerURL)
	assert.Equal(t, []string{"new-shot"}, capture.cmd.Movie.Screenshots)
	assert.Equal(t, "Merged", capture.cmd.Movie.Actresses[0].FirstName)
	assert.Equal(t, "new-thumb", capture.cmd.Movie.Actresses[0].ThumbURL)
	assert.Equal(t, dedup, capture.cmd.Dedup)
	assert.True(t, capture.cmd.OverwriteExistingMedia)
	assert.Equal(t, []string{"created"}, state.downloadPaths)
}
func TestStepDownload_UsesOnlyCreatedPathsForRevert(t *testing.T) {
	capture := &capturingDownloader{outcome: &downloader.DownloadOutcome{
		DownloadedPaths: []string{"overwritten"},
	}}
	o := &applyOrchImpl{downloader: capture}
	state := &applyPipelineState{
		movie:    &models.Movie{ID: "TEST-003"},
		finalDir: "/destination",
	}
	cmd := ApplyCmd{
		Movie:                  &models.Movie{ID: "TEST-003"},
		Download:               true,
		OverwriteExistingMedia: true,
	}

	require.NoError(t, o.stepDownload(context.Background(), cmd, "op-test", state, &stepCompletion{}))
	assert.Empty(t, state.downloadPaths)
}

func TestActressNamesMatchSupportsReversedRomanizedNames(t *testing.T) {
	assert.True(t, actressNamesMatch(
		models.Actress{FirstName: "Yui", LastName: "Hatano"},
		models.Actress{FirstName: "Hatano", LastName: "Yui"},
	))
	assert.False(t, actressNamesMatch(models.Actress{FirstName: "Yui"}, models.Actress{LastName: "Hatano"}))
}

func TestSnapshotOverlayNilInputs(t *testing.T) {
	assert.Nil(t, snapshotScrapedMedia(nil))
	var snapshot *scrapedMediaSnapshot
	assert.Nil(t, snapshot.overlay(nil))
	movie := &models.Movie{ID: "TEST-004", Title: "Existing"}
	assert.Equal(t, movie, snapshot.overlay(movie))
}

func TestOverlayActressThumbsUsesRomanizedFallback(t *testing.T) {
	merged := []models.Actress{{FirstName: "Yui", LastName: "Hatano"}}
	scraped := []models.Actress{{FirstName: "Hatano", LastName: "Yui", ThumbURL: "romanized"}}
	result := overlayActressThumbs(merged, scraped)
	require.Len(t, result, 1)
	assert.Equal(t, "romanized", result[0].ThumbURL)
}
