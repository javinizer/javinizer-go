package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapeResultToMovieResult_PreserveMatcherID(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "MIDA-660.mp4", Name: "MIDA-660.mp4", MovieID: "MIDA-660"}
	result := &scrape.ScrapeResult{Movie: &models.Movie{ID: "SAME-CONTENT-ID"}}
	mr, _ := scrapeResultToMovieResult(fmi, result, nil, true)
	require.NotNil(t, mr)
	assert.Equal(t, "MIDA-660", mr.FileMatchInfo.MovieID,
		"matcher-derived MovieID should be preserved when preserveMovieID=true")
}

func TestScrapeResultToMovieResult_OverwriteWhenNotPreserved(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "MIDA-660.mp4", Name: "MIDA-660.mp4", MovieID: "MIDA-660"}
	result := &scrape.ScrapeResult{Movie: &models.Movie{ID: "SCRAPE-001"}}
	mr, _ := scrapeResultToMovieResult(fmi, result, nil, false)
	require.NotNil(t, mr)
	assert.Equal(t, "SCRAPE-001", mr.FileMatchInfo.MovieID,
		"scraped MovieID should overwrite when preserveMovieID=false")
}

func TestScrapeResultToMovieResult_PreserveButFMIEmpty(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "unknown.mp4", Name: "unknown.mp4", MovieID: ""}
	result := &scrape.ScrapeResult{Movie: &models.Movie{ID: "SCRAPE-001"}}
	mr, _ := scrapeResultToMovieResult(fmi, result, nil, true)
	require.NotNil(t, mr)
	assert.Equal(t, "SCRAPE-001", mr.FileMatchInfo.MovieID,
		"scraped MovieID should be used when fmi.MovieID is empty even with preserveMovieID=true")
}

func TestScrapeResultToMovieResult_TwoDifferentIDsSameScrapedID(t *testing.T) {
	result := &scrape.ScrapeResult{Movie: &models.Movie{ID: "SAME-CONTENT-ID"}}

	fmi1 := models.FileMatchInfo{Path: "MIDA-660.mp4", Name: "MIDA-660.mp4", MovieID: "MIDA-660"}
	mr1, _ := scrapeResultToMovieResult(fmi1, result, nil, true)
	require.NotNil(t, mr1)

	fmi2 := models.FileMatchInfo{Path: "YUJ-055.mp4", Name: "YUJ-055.mp4", MovieID: "YUJ-055"}
	mr2, _ := scrapeResultToMovieResult(fmi2, result, nil, true)
	require.NotNil(t, mr2)

	assert.NotEqual(t, mr1.FileMatchInfo.MovieID, mr2.FileMatchInfo.MovieID,
		"two files with different matcher IDs should keep different MovieIDs even if scraped ID is the same")
	assert.Equal(t, "MIDA-660", mr1.FileMatchInfo.MovieID)
	assert.Equal(t, "YUJ-055", mr2.FileMatchInfo.MovieID)
}
