package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Linked-movie identity recovery must distinguish ordinary catalog misses
// from real outages: misses are data, outages need retries, partial success
// degrades to whatever could be found.
func TestLinkedActressCandidatesDistinguishesMissesFromOutages(t *testing.T) {
	_, _, movies, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "linked"})
	db := movies.GetDB()
	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "MOV-9", DisplayTitle: "Linked Movie"}).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "MOV-9", actress.ID).Error)

	notFound := &callbackActressScraper{search: func(context.Context, string) (*models.ScraperResult, error) {
		return nil, models.NewScraperNotFoundError("minnanoav", "no such actress")
	}}
	outage := &callbackActressScraper{search: func(context.Context, string) (*models.ScraperResult, error) {
		return nil, errors.New("upstream timeout")
	}}
	good := &callbackActressScraper{search: func(context.Context, string) (*models.ScraperResult, error) {
		return &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 42, JapaneseName: "linked"}}}, nil
	}}

	candidates, err := linkedActressCandidates(context.Background(), movies, actress.ID, []models.Scraper{notFound})
	require.NoError(t, err, "a catalog miss is data, not an outage")
	require.Empty(t, candidates)

	_, err = linkedActressCandidates(context.Background(), movies, actress.ID, []models.Scraper{outage})
	require.Error(t, err, "with nothing usable, an outage must reach the caller")

	candidates, err = linkedActressCandidates(context.Background(), movies, actress.ID, []models.Scraper{good, outage})
	require.NoError(t, err, "partial success proceeds with usable candidates")
	require.NotEmpty(t, candidates)
}
