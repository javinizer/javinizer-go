package r18dev

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapeURLCompactRemasterCIDProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, id, cid, dvd, wantID string
		reject                     bool
	}{
		{"compact five digits", "rct12345hd", "rct12345hd", "RCT-12345-HD", "RCT-12345H", false},
		{"compact six digits", "rct100000hd", "rct100000hd", "RCT-100000-HD", "RCT-100000H", false},
		{"uppercase URL", "RCT100000HD", "rct100000hd", "RCT-100000-HD", "RCT-100000H", false},
		{"mixed case channel URL", "H_003Rct100000Hd", "h_003rct100000hd", "RCT-100000-HD", "RCT-100000H", false},
		{"mixed case URL rejects changed identity", "RcT100000hD", "1rct100000h", "RCT-100000-HD", "", true},
		{"compact short digits", "rct1234hd", "rct1234hd", "RCT-1234-HD", "RCT-1234H", false},
		{"wide padding equivalent", "rct000123hd", "rct00123hd", "RCT-123-HD", "RCT-123H", false},
		{"overflow literal", "rct123456789012345678901234567890hd", "rct123456789012345678901234567890hd", "RCT-123456789012345678901234567890-HD", "RCT-123456789012345678901234567890H", false},
		{"catalog prefix", "1rct100000hd", "1rct100000hd", "RCT-100000-HD", "RCT-100000H", false},
		{"channel prefix", "h_003rct100000hd", "h_003rct100000hd", "RCT-100000-HD", "RCT-100000H", false},
		{"literal H", "rct100000h", "rct100000h", "RCT-100000-HD", "RCT-100000H", false},
		{"rental normalization", "rct100000hdr", "rct100000hd", "RCT-100000-HD", "RCT-100000H", false},
		{"AI diverging display number", "dv100000ai", "dv100000ai", "DV-818-AI", "DV-818AI", false},
		{"AI catalog prefix", "1dv100000ai", "1dv100000ai", "DV-818-AI", "DV-818AI", false},
		{"AI channel prefix", "h_003dv100000ai", "h_003dv100000ai", "DV-818-AI", "DV-818AI", false},
		{"AI no display number", "dv100000ai", "dv100000ai", "", "", false},
		{"different literal marker", "rct100000hd", "rct100000h", "RCT-100000-HD", "", true},
		{"different number masked by display", "rct100000hd", "rct100001hd", "RCT-100000-HD", "", true},
		{"different catalog prefix", "rct100000hd", "1rct100000hd", "RCT-100000-HD", "", true},
		{"different channel prefix", "h_003rct100000hd", "n_003rct100000hd", "RCT-100000-HD", "", true},
		{"different marker class", "rct100000hd", "rct100000ai", "RCT-100000-HD", "", true},
		{"different catalog suffix", "rct100000hd", "rct100000zhd", "RCT-100000-HD", "", true},
		{"missing content identity", "rct100000hd", "", "RCT-100000-HD", "", true},
		{"conflicting display stays unpublished", "rct100000hd", "rct100000hd", "RCT-100001-HD", "", false},
		{"separator pinned display URL", "RCT-100000-HD", "1rct100000h", "RCT-100000-HD", "RCT-100000H", false},
		{"separator pinned AI URL", "DV-818-AI", "dv100000ai", "DV-818-AI", "DV-818AI", false},
		{"separator pinned conflict", "RCT-100000-HD", "1rct100001h", "RCT-100001-HD", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &byURLTransport{responder: func(_ int, _ string) (int, string) {
				return 200, fmt.Sprintf(`{"content_id":%q,"dvd_id":%q,"title_en":"Identity fixture"}`, tc.cid, tc.dvd)
			}}
			s := numericContractScraper(nil, transport)
			result, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined="+tc.id+"/json")
			if tc.reject {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tc.cid, result.ContentID)
			assert.Equal(t, tc.wantID, result.ID)
		})
	}
}

func TestExplicitRemasterIdentityGuardKeepsSearchHeuristic(t *testing.T) {
	const id = "dv100000ai"
	require.False(t, isRawRemasterContentIDQuery(id), "standalone search text keeps its display interpretation")
	raw := &models.ScraperResult{ContentID: id, ID: "DV-818-AI"}
	result, err := guardRemasterResultWithIdentity(id, raw, true)
	require.NoError(t, err)
	assert.Equal(t, "DV-818AI", result.ID)
	_, err = guardRemasterResult(id, &models.ScraperResult{ContentID: id, ID: "DV-818-AI"})
	require.Error(t, err, "display search must not gain the raw AI number exemption")
	_, err = guardRemasterResultWithIdentity(id, &models.ScraperResult{ContentID: "dv100001ai", ID: "DV-818-AI"}, true)
	require.Error(t, err, "explicit raw provenance binds the catalog slot")
}
