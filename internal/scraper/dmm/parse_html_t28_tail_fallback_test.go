package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Keep the classifier's T/T28 split when no page display ID is available.
// Catalog/channel prefixes prove the T28 boundary even for four-digit tails;
// prefixless three/four-digit tails retain their existing T-series meaning.
func TestScrapeURLT28CIDTailKeepsBoundary(t *testing.T) {
	for _, cid := range []struct{ base, wantID string }{
		{"t2800123", "T28-123"},
		{"1t28000123", "T28-123"},
		{"9t2800123", "T28-123"},
		{"h_003t2800123", "T28-123"},
		{"9t281234", "T28-1234"},
		{"1t281234", "T28-1234"},
		{"h_003t281234", "T28-1234"},
		{"t2812345", "T28-12345"},
		{"9t2812345", "T28-12345"},
		{"9t28123456", "T28-123456"},
		{"9t28000123456789012345678901234567890", "T28-123456789012345678901234567890"},
		{"9t28000", "T28-0"},
		{"t28123", "T-28123"},
		{"t281234", "T-281234"},
	} {
		for _, suffix := range []string{"", "e", "z"} {
			for _, marker := range []string{"h", "hd"} {
				value := cid.base + suffix + marker
				t.Run(value, func(t *testing.T) {
					s, _ := newRemasterTestScraper(t)
					s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
						return 200, `<html><body><h1 id="title" class="item">HD Remaster</h1></body></html>`
					}})
					result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid="+value+"/")
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, value, result.ContentID)
					assert.Equal(t, cid.wantID+strings.ToUpper(suffix)+"H", result.ID)
				})
			}
		}
	}
}

// AI slots still never derive a display number, including longer T28 tails.
func TestScrapeURLT28LongAICIDStillRequiresPageIdentity(t *testing.T) {
	for _, cid := range []string{"t2812345ai", "9t281234ai", "h_003t281234ai"} {
		for _, page := range []struct{ name, row, wantID string }{
			{"missing row", "", ""},
			{"verified divergent number", `<tr><td>品番：</td><td>T28-456-AI</td></tr>`, "T28-456AI"},
		} {
			t.Run(cid+"/"+page.name, func(t *testing.T) {
				s, _ := newRemasterTestScraper(t)
				s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
					return 200, `<html><body><h1 id='title' class='item'>Fixture product</h1><table>` + page.row + `</table></body></html>`
				}})
				result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid="+cid+"/")
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, cid, result.ContentID)
				assert.Equal(t, page.wantID, result.ID)
			})
		}
	}
}

func TestT28CIDDisplayIDPreservesOtherInputs(t *testing.T) {
	assert.Equal(t, "T-28123H", canonicalRemasterDisplayID("T-28123-HD"))
	for _, tc := range []struct{ cid, wantID string }{
		{"t28123h", "T-28123H"},
		{"t281234hd", "T-281234H"},
		{"dv00899ai", "DV-00899AI"},
		{"unparseable", "UNPARSEABLE"},
	} {
		t.Run(tc.cid, func(t *testing.T) {
			assert.Equal(t, tc.wantID, t28CidDisplayID(tc.cid))
		})
	}
}
