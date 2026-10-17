package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/require"
)

func TestChallengeOverwriteMediaPreservesNamesWithoutNFODownloads(t *testing.T) {
	a := models.Actress{ID: 8, FirstName: "Source", LastName: "Person", ThumbURL: "https://source.invalid/fresh.jpg", Verified: true}
	source := &models.Movie{ID: "MEDIA-001", Title: "Synthetic", Actresses: []models.Actress{a}, Credits: []models.MovieCredit{{ID: 9, ActressID: 8, Actress: &a, OverrideName: "Chosen Name", UserOverride: true}}}
	n := &models.Movie{ID: source.ID, Actresses: []models.Actress{{FirstName: "Source", LastName: "Person", ThumbURL: "https://nfo.invalid/stale.jpg"}, {FirstName: "Extra", LastName: "Actor", ThumbURL: "https://nfo.invalid/extra.jpg"}}}
	merged, err := nfo.MergeMovieMetadataWithOptions(source, n, nfo.PreferNFO, true)
	require.NoError(t, err)
	before := merged.Merged.Clone()
	capture := &capturingDownloader{outcome: &downloader.DownloadOutcome{}}
	orch := &applyOrchImpl{downloader: capture}
	state := &applyPipelineState{movie: merged.Merged, finalDir: "/output", scrapedMediaURLs: snapshotScrapedMedia(source)}
	require.NoError(t, orch.stepDownload(t.Context(), ApplyCmd{Download: true, OverwriteExistingMedia: true}, "", state, &stepCompletion{}))
	require.NotNil(t, capture.cmd.Movie)
	credits, present := capture.cmd.Movie.RenderingCredits()
	require.True(t, present)
	require.Len(t, credits, 2)
	require.Equal(t, "Chosen Name", credits[0].RenderName(false, credits[0].Actress.FullName()))
	require.Equal(t, "https://source.invalid/fresh.jpg", credits[0].Actress.ThumbURL)
	require.Equal(t, "Extra", credits[1].Actress.FirstName)
	require.Empty(t, credits[1].Actress.ThumbURL, "overwrite must never reuse NFO-only media URLs")
	require.Equal(t, source.Credits, capture.cmd.Movie.Credits, "download-only projection cannot mutate the authority ledger")
	require.Equal(t, before, state.movie, "download-only projection cannot change metadata output")
}

func TestNFOMediaProjectionHandlesMissingIdentityWithoutFallback(t *testing.T) {
	movie := &models.Movie{Credits: []models.MovieCredit{{CreditedName: "Unresolved"}}}
	snapshot := snapshotScrapedMedia(movie)
	rendered := snapshot.overlay(movie)
	credits, present := rendered.RenderingCredits()
	require.True(t, present)
	require.Nil(t, credits[0].Actress)
	require.Equal(t, movie.Credits, rendered.Credits)
}
