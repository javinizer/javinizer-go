package dmm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AI CID numbers are server slots, including T28 CIDs with numeric or channel
// prefixes. Only a verified page 品番 can supply their display identity.
func TestScrapeURLT28AICIDRequiresPageIdentity(t *testing.T) {
	for _, cid := range []struct{ value, series, displayNumber string }{
		{"t28123ai", "T", "28456"},
		{"t2800123ai", "T28", "456"},
		{"9t28123ai", "T28", "456"},
		{"h_003t28123ai", "T28", "456"},
	} {
		for _, page := range []struct{ name, row, wantID string }{
			{"missing row", "", ""},
			{"empty row", `<tr><td>品番：</td><td></td></tr>`, ""},
			{"unparseable row", `<tr><td>品番：</td><td>unknown</td></tr>`, ""},
			{"content ID row only", `<tr><td>商品番号：</td><td>` + cid.value + `</td></tr>`, ""},
			{"verified divergent display number", `<tr><td>品番：</td><td>` + cid.series + `-` + cid.displayNumber + `-AI</td></tr>`, cid.series + "-" + cid.displayNumber + "AI"},
		} {
			t.Run(cid.value+"/"+page.name, func(t *testing.T) {
				s, _ := newRemasterTestScraper(t)
				s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
					return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1><table>` + page.row + `</table></body></html>`
				}})
				result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid="+cid.value+"/")
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, cid.value, result.ContentID)
				assert.Equal(t, page.wantID, result.ID)
			})
		}
	}
}

func TestScrapeURLT28AICIDRejectsConflictingPageIdentity(t *testing.T) {
	for _, cid := range []struct{ value, series, displayNumber string }{
		{"t28123ai", "T", "28123"},
		{"t2800123ai", "T28", "123"},
		{"9t28123ai", "T28", "123"},
		{"h_003t28123ai", "T28", "123"},
	} {
		for _, display := range []string{
			"ABC-123-AI",
			cid.series + "-" + cid.displayNumber + "-HD",
			cid.series + "-" + cid.displayNumber + "Z-AI",
			cid.series + "-" + cid.displayNumber,
		} {
			t.Run(cid.value+"/"+display, func(t *testing.T) {
				s, _ := newRemasterTestScraper(t)
				s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
					return 200, `<html><body><table><tr><td>品番：</td><td>` + display + `</td></tr></table></body></html>`
				}})
				result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid="+cid.value+"/")
				require.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), "different release")
			})
		}
	}
}

// H and HD CID numbers still encode their display numbers, so their existing
// T/T28 fallback and page-number conflict guards must remain unchanged.
func TestScrapeURLT28HDCIDKeepsIdentityFallback(t *testing.T) {
	for _, cid := range []struct{ base, series, number, nextNumber string }{
		{"t28123", "T", "28123", "28124"},
		{"9t28123", "T28", "123", "124"},
		{"h_003t28123", "T28", "123", "124"},
	} {
		for _, marker := range []string{"h", "hd"} {
			for _, page := range []struct {
				name, row string
				wantError bool
			}{
				{"missing row", "", false},
				{"matching row", `<tr><td>品番：</td><td>` + cid.series + `-` + cid.number + `-HD</td></tr>`, false},
				{"different number", `<tr><td>品番：</td><td>` + cid.series + `-` + cid.nextNumber + `-HD</td></tr>`, true},
			} {
				t.Run(cid.base+marker+"/"+page.name, func(t *testing.T) {
					s, _ := newRemasterTestScraper(t)
					s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
						return 200, `<html><body><table>` + page.row + `</table></body></html>`
					}})
					result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid="+cid.base+marker+"/")
					if page.wantError {
						require.Error(t, err)
						assert.Nil(t, result)
						assert.Contains(t, err.Error(), "different release")
						return
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, cid.base+marker, result.ContentID)
					assert.Equal(t, cid.series+"-"+cid.number+"H", result.ID)
				})
			}
		}
	}
}
