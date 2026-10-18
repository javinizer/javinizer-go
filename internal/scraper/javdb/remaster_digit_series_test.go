package javdb

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchCompactDigitSeriesSoleDisplay(t *testing.T) {
	for _, tc := range []struct{ query, display string }{
		{"300MIUM700H", "300MIUM-700H"},
		{"300mium700hd", "300MIUM-700-H"},
		{"300MIUM700H", "300MIUM-700-HD"},
		{"300MIUM700AI", "300MIUM-700AI"},
		{"300MIUM700EH", "300MIUM-700-E-HD"},
		{"300MIUM700ZHD", "300MIUM-700ZH"},
		{"300MIUM700EAI", "300MIUM-700-E-AI"},
		{"300MIUM700ZAI", "300MIUM-700-Z-AI"},
	} {
		t.Run(tc.query+"/"+tc.display, func(t *testing.T) {
			s := digitSeriesTestScraper(tc.query, []string{tc.display}, tc.display)
			result, err := s.Search(context.Background(), tc.query)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tc.display, result.ID)
		})
	}
}

func TestSearchCompactDigitSeriesRejectsDifferentIdentity(t *testing.T) {
	for _, tc := range []struct{ query, display string }{
		{"300MIUM700H", "300MIUM-701H"},
		{"300MIUM700H", "301MIUM-700H"},
		{"300MIUM700H", "300MIUM-700"},
		{"300MIUM700H", "300MIUM-700AI"},
		{"300MIUM700AI", "300MIUM-701AI"},
		{"300MIUM700AI", "300MIUM-700H"},
		{"300MIUM700ZH", "300MIUM-700H"},
		{"300MIUM700ZH", "300MIUM-700EH"},
		{"300MIUM700EAI", "300MIUM-700ZAI"},
		{"300mium00700h", "300MIUM-700H"},
		{"300mium00700ai", "300MIUM-700AI"},
		{"h_300mium700h", "300MIUM-700H"},
		{"n_300mium700ai", "300MIUM-700AI"},
		{"300MIUM-700H", "MIUM-700H"},
		{"T-28123H", "T28-123H"},
		{"T28-123H", "T-28123H"},
		{"t28123h", "T28-123H"},
	} {
		t.Run(tc.query+"/"+tc.display, func(t *testing.T) {
			s := digitSeriesTestScraper(tc.query, []string{tc.display}, tc.display)
			result, err := s.Search(context.Background(), tc.query)
			require.Error(t, err)
			assert.Nil(t, result)
			scraperErr, ok := models.AsScraperError(err)
			require.True(t, ok)
			assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
		})
	}
}

func TestSearchCompactDigitSeriesPreservesRawControls(t *testing.T) {
	for _, tc := range []struct{ query, display string }{
		{"300mium00700h", "MIUM-700-HD"},
		{"300mium00700ai", "MIUM-818AI"},
		{"h_300mium700h", "MIUM-700H"},
		{"n_300mium700ai", "MIUM-818AI"},
		{"1rct156h", "RCT-156-HD"},
		{"1rct156ai", "RCT-818AI"},
		{"h_1rct156ai", "RCT-818AI"},
		{"9t28123h", "T28-123H"},
		{"300MIUM-700H", "300MIUM-700HD"},
	} {
		t.Run(tc.query+"/"+tc.display, func(t *testing.T) {
			s := digitSeriesTestScraper(tc.query, []string{tc.display}, tc.display)
			result, err := s.Search(context.Background(), tc.query)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tc.display, result.ID)
		})
	}
}

func TestSearchCompactDigitSeriesRejectsAmbiguousDisplays(t *testing.T) {
	for _, marker := range []string{"H", "HD", "AI", "EH", "ZAI"} {
		for _, reverse := range []bool{false, true} {
			cards := []string{"MIUM-700" + marker, "300MIUM-700" + marker}
			if reverse {
				cards[0], cards[1] = cards[1], cards[0]
			}
			t.Run(strings.Join(cards, "/"), func(t *testing.T) {
				query := "300MIUM700" + marker
				s := digitSeriesTestScraper(query, cards, cards[0])
				// A guessed video-code URL cannot bypass search ambiguity.
				responses := map[string]string{
					"https://javdb.test/search?q=" + query + "&f=all": remasterSearchPage(cards...),
					"https://javdb.test/v/" + query:                   remasterDetailPage(cards[0]),
				}
				for _, card := range cards {
					slug := strings.ToLower(strings.ReplaceAll(card, "-", ""))
					responses["https://javdb.test/v/"+slug] = remasterDetailPage(card)
				}
				s.client.SetTransport(&staticRoundTripper{responses: responses})
				_, _, err := s.findDetailSelectionCtx(context.Background(), query)
				require.Error(t, err, "both display interpretations must miss before fetching any detail")
				result, err := s.Search(context.Background(), query)
				require.Error(t, err)
				assert.Nil(t, result)
			})
		}
	}
}

func TestFindDetailCompactDigitSeriesControlsInBothOrders(t *testing.T) {
	for _, tc := range []struct {
		query, first, second, selected string
	}{
		{"300mium00700h", "MIUM-700H", "300MIUM-700H", "MIUM-700H"},
		{"300mium00700ai", "MIUM-818AI", "300MIUM-700AI", "MIUM-818AI"},
		{"h_300mium700h", "MIUM-700H", "300MIUM-700H", "MIUM-700H"},
		{"n_300mium700ai", "MIUM-818AI", "300MIUM-700AI", "MIUM-818AI"},
		{"300MIUM-700H", "MIUM-700H", "300MIUM-700H", "300MIUM-700H"},
		{"300MIUM700AI", "MIUM-818AI", "300MIUM-700AI", ""},
		{"300MIUM700H", "300MIUM-700H", "300MIUM-700H", "300MIUM-700H"},
		{"300MIUM700AI", "300MIUM-700AI", "300MIUM-701AI", "300MIUM-700AI"},
		{"300MIUM700H", "MIUM-700H", "MIUM-700H", "MIUM-700H"},
		{"300MIUM700H", "MIUM-701H", "300MIUM-700H", "300MIUM-700H"},
		{"300mium700h", "1mium700h", "300MIUM-700H", ""},
		{"300mium700ai", "1mium700ai", "300MIUM-700AI", ""},
		{"300mium700h", "h_300mium700h", "300MIUM-700H", ""},
		{"300mium700ai", "n_300mium700ai", "300MIUM-700AI", ""},
		{"300mium700h", "1mium00700h", "300MIUM-700H", ""},
		{"300mium700h", "1mium700h", "300mium700h", "300mium700h"},
		{"300mium700ai", "1mium700ai", "300mium700ai", "300mium700ai"},
		{"300mium700h", "h_300mium700h", "300mium700hd", "300mium700hd"},
		{"300mium700h", "1mium700h", "1mium700h", "1mium700h"},
		{"300MIUM700AI", "MIUM-818AI", "MIUM-819AI", ""},
		{"300mium00700ai", "MIUM-700AI", "MIUM-818AI", ""},
		{"T-28123H", "T28-123H", "T-28123H", "T-28123H"},
		{"T28-123H", "T-28123H", "T28-123H", "T28-123H"},
	} {
		for _, reverse := range []bool{false, true} {
			cards := []string{tc.first, tc.second}
			if reverse {
				cards[0], cards[1] = cards[1], cards[0]
			}
			t.Run(tc.query+"/"+strings.Join(cards, "/"), func(t *testing.T) {
				s := digitSeriesTestScraper(tc.query, cards, "")
				_, selected, err := s.findDetailSelectionCtx(context.Background(), tc.query)
				if tc.selected == "" {
					require.Error(t, err)
					assert.Empty(t, selected)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tc.selected, selected)
				}
			})
		}
	}
}

func TestFindDetailCompactDigitSeriesLiteralCardWins(t *testing.T) {
	for _, query := range []string{"300mium700h", "300mium700ai", "300mium00700h", "300mium00700ai"} {
		for _, reverse := range []bool{false, true} {
			marker := remasterMarkerSuffix(query)
			cards := []string{"MIUM-700" + marker, "300MIUM-700" + marker, query}
			if reverse {
				cards[0], cards[2] = cards[2], cards[0]
			}
			t.Run(strings.Join(cards, "/"), func(t *testing.T) {
				s := digitSeriesTestScraper(query, cards, "")
				_, selected, err := s.findDetailSelectionCtx(context.Background(), query)
				require.NoError(t, err)
				assert.Equal(t, query, selected, "literal compact identity is stronger than display interpretations")
			})
		}
	}
}

func TestSearchCompactDigitSeriesSelectedDetailGuard(t *testing.T) {
	for _, detail := range []string{"300MIUM-700H", "300MIUM-700HD", "300MIUM700H", "", "MIUM-700H", "300MIUM-701H", "300MIUM-700AI", "300MIUM-700"} {
		t.Run(detail, func(t *testing.T) {
			s := digitSeriesTestScraper("300MIUM700H", []string{"300MIUM-700H"}, detail)
			result, err := s.Search(context.Background(), "300MIUM700H")
			if detail == "MIUM-700H" || detail == "300MIUM-701H" || detail == "300MIUM-700AI" || detail == "300MIUM-700" {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				if detail == "" {
					detail = "300MIUM-700H"
				}
				assert.Equal(t, detail, result.ID)
			}
		})
	}
}

func digitSeriesTestScraper(query string, cards []string, detail string) *scraper {
	responses := map[string]string{
		"https://javdb.test/search?q=" + url.QueryEscape(query) + "&f=all": remasterSearchPage(cards...),
	}
	for _, card := range cards {
		slug := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(card))
		responses["https://javdb.test/v/"+slug] = remasterDetailPage(detail)
	}
	return newMarkerTestScraper(responses)
}
