package scrape

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCreditEvidenceCompactionRetainsSourcePriority(t *testing.T) {
	high := models.Actress{DMMID: 77, FirstName: "Canonical"}
	low := high
	high.SetScrapeEvidence("high", models.ActressInfo{FirstName: "Reported", JapaneseName: "報告", ThumbURL: "high.jpg"}, 0)
	low.SetScrapeEvidence("low", models.ActressInfo{FirstName: "Other", ThumbURL: "low.jpg"}, 1)
	for _, actors := range [][]models.Actress{{low, high}, {high, low}, {{DMMID: 77, FirstName: "Canonical"}, high}} {
		sources := buildActressSourcesFromScrapeResults([]*models.ScraperResult{{Source: "high"}, {Source: "low"}}, nil, nil, actors)
		require.Equal(t, "high", sources["dmmid:77"])
		movie := &models.Movie{Actresses: actors}
		BuildCreditsFromScrape(movie, nil, nil)
		require.Len(t, movie.Credits, 1)
		credit := movie.Credits[0]
		require.Equal(t, "high", credit.Source)
		require.Equal(t, "Reported", credit.CreditedName)
		require.Equal(t, "報告", credit.CreditedJapaneseName)
		require.Equal(t, "high.jpg", credit.ReportedThumbURL)
		require.Equal(t, "Canonical", credit.Scraped.FirstName)
	}
}
