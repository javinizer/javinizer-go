package dmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestExtractIdentifiers_RawMarkerFold(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	for _, tc := range []struct {
		cid, wantID string
	}{
		{"1rct00156hd", "RCT-156H"},
		{"1ipx00535zhd", "IPX-535ZH"},
		{"1dv00818ai", "DV-818AI"},
		{"1ipx00535", "IPX-535"},
		{"1t28000123hd", "T28-000123H"},
	} {
		res := &models.ScraperResult{}
		s.extractIdentifiers(res, nil, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid="+tc.cid+"/", true)
		assert.Equal(t, tc.cid, res.ContentID, tc.cid)
		assert.Equal(t, tc.wantID, res.ID, tc.cid)
	}
}
