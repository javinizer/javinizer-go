package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Prefix membership alone does not prove whether compact t28 spells T-28N
// or T28-N. Keep the raw CID and require a page identity that agrees with the
// possible number, marker and catalog suffix. AI numbers remain unbound.
func TestScrapeURLUnprovenT28PrefixRequiresVerifiedPage(t *testing.T) {
	type pageCase struct {
		name, display, wantID string
		wantError             bool
	}
	for _, source := range []struct{ prefix, tail, suffix string }{
		{"55", "123", ""},
		{"55", "00123", ""},
		{"874", "123", ""},
		{"1038", "123", ""},
		{"n_1137", "123", ""},
		{"77", "123", ""},
		{"h_999", "123", ""},
		{"n_600", "123", ""},
		{"55", "1234", "e"},
		{"n_1137", "00123", "z"},
		{"77", "00123456789012345678901234567890", ""},
	} {
		for _, marker := range []string{"h", "hd", "ai"} {
			cid := source.prefix + "t28" + source.tail + source.suffix + marker
			assert.Empty(t, t28CidDisplayID(cid), "unproven prefix cannot derive a display ID")
			folded, wrongMarker := "H", "AI"
			tNumber, t28Number := "28"+source.tail, trimDisplayZeros(source.tail)
			if marker == "ai" {
				folded, wrongMarker = "AI", "H"
				tNumber, t28Number = "28999", "456"
			}
			suffix := strings.ToUpper(source.suffix)
			wrongSuffix := "Z"
			if suffix == "Z" {
				wrongSuffix = "E"
			}
			tID, t28ID := "T-"+tNumber+suffix+folded, "T28-"+t28Number+suffix+folded
			pages := []pageCase{
				{"missing row", "", "", false},
				{"unparseable row", "unknown", "", false},
				{"verified T boundary", tID, tID, false},
				{"verified T28 boundary", t28ID, t28ID, false},
				{"foreign series", "ABC-" + t28Number + suffix + folded, "", true},
				{"wrong marker", "T-" + tNumber + suffix + wrongMarker, "", true},
				{"wrong suffix", "T28-" + t28Number + wrongSuffix + folded, "", true},
				{"markerless T", "T-" + tNumber + suffix, "", true},
				{"markerless T28", "T28-" + t28Number + suffix, "", true},
			}
			if marker != "ai" {
				pages = append(pages,
					pageCase{"wrong T number", "T-" + tNumber + "1" + suffix + folded, "", true},
					pageCase{"wrong T28 number", "T28-" + t28Number + "1" + suffix + folded, "", true})
			}
			for _, page := range pages {
				t.Run(cid+"/"+page.name, func(t *testing.T) {
					s, _ := newRemasterTestScraper(t)
					s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
						body := `<html><body><h1 id='title' class='item'>Fixture product</h1><table>`
						if page.display != "" {
							body += `<tr><td>品番：</td><td>` + page.display + `</td></tr>`
						}
						return 200, body + `</table></body></html>`
					}})
					result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid="+cid+"/")
					if page.wantError {
						require.Error(t, err)
						assert.Nil(t, result)
						assert.Contains(t, err.Error(), "different release")
						return
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, cid, result.ContentID, "literal CID must remain unchanged")
					assert.Equal(t, page.wantID, result.ID)
				})
			}
		}
	}
}

func TestScrapeURLProvenT28PrefixRejectsTPage(t *testing.T) {
	for _, prefix := range []string{"9", "1", "h_003"} {
		for _, marker := range []string{"h", "hd", "ai"} {
			cid := prefix + "t28123" + marker
			t.Run(cid, func(t *testing.T) {
				s, _ := newRemasterTestScraper(t)
				s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
					return 200, `<html><body><h1 id='title' class='item'>Fixture product</h1><table><tr><td>品番：</td><td>T-28123-` + marker + `</td></tr></table></body></html>`
				}})
				result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid="+cid+"/")
				require.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), "different release")
			})
		}
	}
}
