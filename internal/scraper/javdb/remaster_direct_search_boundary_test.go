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

func TestAllMarkerBareInputsUseGuardedSearch(t *testing.T) {
	for _, tc := range []struct{ query, selected string }{
		{"RCT156H", "RCT-156H"},
		{"rct156hd", "RCT-156-HD"},
		{"RCT156HHD", "RCT-156-HD"},
		{"DV818AI", "DV-818AI"},
		{"IPX535EH", "IPX-535-EH"},
		{"IPX535ZHD", "IPX-535-Z-HD"},
		{"DV818EAI", "DV-818-EAI"},
		{"DV818ZAI", "DV-818-ZAI"},
		{"t28123h", "T-28123H"},
		{"t2800123h", "T28-123H"},
		{"1rct00156h", "RCT-156H"},
		{"12345ABC1H", "12345ABC-1H"},
		{"dv00899ai", "DV-818AI"},
		{"300MIUM700H", "300MIUM-700H"},
		// Marker detection applies even when the folded key cannot pin a
		// series/number split. The search and direct-lookup gates must agree.
		{"FC2PPV123H", "FC2-PPV-123H"},
		{"FC2PPV123AI", "FC2-PPV-123AI"},
		{"A1B2H", "A1-B2H"},
	} {
		for _, mode := range []string{"empty", "foreign", "selected"} {
			t.Run(tc.query+"/"+mode, func(t *testing.T) {
				query := "  " + tc.query + "  "
				directURL := "https://javdb.test/v/" + tc.query
				searchURL := "https://javdb.test/search?q=" + url.QueryEscape(tc.query) + "&f=all"
				cards := []string{}
				if mode != "empty" {
					cards = []string{"RCT-156", "RCT-157H", "ZZZ-999AI"}
				}
				if mode == "selected" {
					cards = append(cards, tc.selected)
				}
				// This rich guessed page has no published ID. The old direct
				// branch inherited the query and accepted it without searching.
				responses := map[string]string{
					directURL: remasterDetailPage(""),
					searchURL: remasterSearchPage(cards...),
				}
				slug := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(tc.selected))
				selectedURL := "https://javdb.test/v/" + slug
				if mode == "selected" {
					responses[selectedURL] = remasterDetailPage(tc.selected)
				}
				transport := &videoCodeContractTransport{staticRoundTripper: staticRoundTripper{responses: responses}}
				s := newMarkerTestScraper(nil)
				s.client.SetTransport(transport)
				result, err := s.Search(context.Background(), query)
				if mode == "selected" {
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, tc.selected, result.ID)
					assert.Equal(t, []string{searchURL, selectedURL}, transport.requests)
				} else {
					require.Error(t, err)
					assert.Nil(t, result)
					scraperErr, ok := models.AsScraperError(err)
					require.True(t, ok)
					assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
					assert.Equal(t, []string{searchURL}, transport.requests)
				}
			})
		}
	}
}

func TestAllMarkerBareInputsDoNotBypassCompetingInterpretations(t *testing.T) {
	for _, tc := range []struct {
		query string
		cards []string
	}{
		{"300MIUM700H", []string{"MIUM-700H", "300MIUM-700H"}},
		{"300MIUM700AI", []string{"MIUM-818AI", "300MIUM-700AI"}},
		{"dv00899ai", []string{"DV-818AI", "DV-819AI"}},
	} {
		for _, reverse := range []bool{false, true} {
			cards := append([]string(nil), tc.cards...)
			if reverse {
				cards[0], cards[1] = cards[1], cards[0]
			}
			t.Run(tc.query+"/"+strings.Join(cards, "/"), func(t *testing.T) {
				searchURL := "https://javdb.test/search?q=" + tc.query + "&f=all"
				transport := &videoCodeContractTransport{staticRoundTripper: staticRoundTripper{responses: map[string]string{
					"https://javdb.test/v/" + tc.query: remasterDetailPage(""),
					searchURL:                          remasterSearchPage(cards...),
				}}}
				s := newMarkerTestScraper(nil)
				s.client.SetTransport(transport)
				result, err := s.Search(context.Background(), tc.query)
				require.Error(t, err)
				assert.Nil(t, result)
				assert.Equal(t, []string{searchURL}, transport.requests)
			})
		}
	}
}
