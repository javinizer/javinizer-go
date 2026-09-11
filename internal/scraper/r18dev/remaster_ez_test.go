package r18dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
