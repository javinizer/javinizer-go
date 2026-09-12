package r18dev

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemaster_EZSuffixIdentity(t *testing.T) {
	if !markerVariationAccept([]byte(`{"content_id": "1ipx00535zh", "dvd_id": "IPX-535Z-HD"}`), "IPX-535Z-HD", "h", "ipx") {
		t.Fatal("matching Z-variant DVDID must be accepted")
	}
	if markerVariationAccept([]byte(`{"content_id": "1ipx00535h", "dvd_id": "IPX-535-HD"}`), "IPX-535Z-HD", "h", "ipx") {
		t.Fatal("original-release DVDID must be rejected for a Z-suffixed query")
	}
	if markerVariationAccept([]byte(`{"content_id": "1ipx00535zh", "dvd_id": "IPX-535Z-HD"}`), "IPX-535-HD", "h", "ipx") {
		t.Fatal("Z-variant result must be rejected for a plain remaster query")
	}
	assert.Equal(t, []string{"ipx-535z-hd"}, remasterDisplaySpellings("IPX-535Z-HD"))
}

func TestRemaster_NullDVDIDSuffixGuard(t *testing.T) {
	if markerVariationAccept([]byte(`{"content_id": "1ipx00535h", "dvd_id": null}`), "IPX-535Z-HD", "h", "ipx") {
		t.Fatal("plain-variant content id must be rejected for a Z-suffixed query when dvd_id is null")
	}
	if markerVariationAccept([]byte(`{"content_id": "1ipx00535zh", "dvd_id": null}`), "IPX-535-HD", "h", "ipx") {
		t.Fatal("Z-variant content id must be rejected for a plain remaster query when dvd_id is null")
	}
	if !markerVariationAccept([]byte(`{"content_id": "1ipx00535zh", "dvd_id": null}`), "IPX-535Z-HD", "h", "ipx") {
		t.Fatal("matching Z-variant content id must be accepted when dvd_id is null")
	}
}

func TestCidRemasterSuffixShapes(t *testing.T) {
	assert.Equal(t, "", cidRemasterSuffix("ABC-123"), "non-remaster shape has no suffix")
	assert.Equal(t, "z", cidRemasterSuffix("IPX-535ZH"))
	assert.Equal(t, "", cidRemasterSuffix("IPX-535H"), "remaster shape without E/Z has empty suffix")
}

func TestGuardRemasterResult_SuffixMismatchRejected(t *testing.T) {
	res := &models.ScraperResult{ID: "IPX-535Z-HD", ContentID: "1ipx00535h"}
	_, err := guardRemasterResult("IPX-535ZH", res)
	assert.Error(t, err)

	plain := &models.ScraperResult{ID: "IPX-535-HD", ContentID: "1ipx00535zh"}
	_, err = guardRemasterResult("IPX-535H", plain)
	assert.Error(t, err)

	matched := &models.ScraperResult{ID: "IPX-535Z-HD", ContentID: "1ipx00535zh"}
	out, err := guardRemasterResult("IPX-535ZH", matched)
	require.NoError(t, err)
	assert.Equal(t, "IPX-535ZH", out.ID)
}
