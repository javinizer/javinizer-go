package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemaster_RentalSuffixNormalized(t *testing.T) {
	assert.Equal(t, "dv00899ai", stripRentalSuffixMarkerAware("dv00899air"))
	assert.Equal(t, "1rct00156h", stripRentalSuffixMarkerAware("1rct00156hr"))
	assert.Equal(t, "118abp00420", stripRentalSuffixMarkerAware("118abp00420r"))
	assert.Equal(t, "ipx00535", stripRentalSuffixMarkerAware("ipx00535r"))
	assert.Equal(t, "ipx00535", stripRentalSuffixMarkerAware("ipx00535"))
	assert.Equal(t, "dv00899ar", stripRentalSuffixMarkerAware("dv00899ar"), "unrecognized tail is preserved")
	assert.Equal(t, "dv-818ai", stripRentalSuffixMarkerAware("DV-818AI"), "display ids without rental suffix are untouched")

	m, series := classifyRemaster("dv00899air")
	assert.Equal(t, "ai", m)
	assert.Equal(t, "dv", series)
	m, series = classifyRemaster("1rct00156hr")
	assert.Equal(t, "h", m)
	assert.Equal(t, "rct", series)
}

func TestRemaster_RentalRawCIDResolvesMarkerRelease(t *testing.T) {
	var baseFetched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		if strings.Contains(path, "combined=1dv00899") && !strings.Contains(path, "combined=1dv00899ai") {
			baseFetched = true
		}
		if strings.Contains(path, "combined=dv00899ai") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "dv00899ai", "dvd_id": null, "title_en": "AI Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "dv00899air")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "dv00899ai", result.ContentID)
	assert.False(t, baseFetched, "rental marker query must never resolve the base release")
}

func TestRemaster_ClassifyAndFold(t *testing.T) {
	m, _ := classifyRemaster("RCT-156H")
	assert.Equal(t, "h", m)
	m, _ = classifyRemaster("RCT-156-HD")
	assert.Equal(t, "h", m)
	m, s := classifyRemaster("DV-818AI")
	assert.Equal(t, "ai", m)
	assert.Equal(t, "dv", s)
	m, _ = classifyRemaster("RCT-156")
	assert.Equal(t, "", m)
	m, s = classifyRemaster("IPX-535Z-HD")
	assert.Equal(t, "h", m)
	assert.Equal(t, "ipx", s)
	m, _ = classifyRemaster("IPX-535ZH")
	assert.Equal(t, "h", m)
	m, _ = classifyRemaster("1ipx00535zh")
	assert.Equal(t, "h", m)

	assert.Equal(t, "rct156h", foldDisplay("RCT-156-HD"))
	assert.Equal(t, "dv818ai", foldDisplay("DV-818AI"))
	assert.Equal(t, "ipx535zh", foldDisplay("IPX-535Z-HD"))
	assert.Equal(t, "ipx535zh", foldDisplay("IPX-535ZH"))
	assert.NotEqual(t, foldDisplay("IPX-535-HD"), foldDisplay("IPX-535Z-HD"))
	assert.Equal(t, "IPX-535ZH", canonicalRemasterDisplayID("IPX-535Z-HD"))
	assert.True(t, cidCarriesMarker("1ipx00535zh", "h"))
	assert.True(t, cidMatchesMarker("1ipx00535zh", "h", "ipx"))
	assert.False(t, cidMatchesMarker("1ipx00535h", "h", "ipxzz"))
	assert.True(t, cidCarriesMarker("1rct00156h", "h"))
	assert.True(t, cidCarriesMarker("1rct00156hd", "h"))
	assert.True(t, cidCarriesMarker("dv00899ai", "ai"))
	assert.False(t, cidCarriesMarker("1rct00156", "h"))
}

func TestRemaster_DVDIDDisplayMatchResolves(t *testing.T) {
	var baseFetched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id=rct-156-hd"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": "RCT-156-HD"}`))
			return
		case strings.Contains(path, "combined=1rct00156h"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": "RCT-156-HD", "title_en": "Remaster"}`))
			return
		case strings.Contains(path, "combined=1rct00156"):
			baseFetched = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156", "dvd_id": "RCT-156"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "1rct00156h", result.ContentID)
	assert.False(t, baseFetched, "release resolution must never fetch the base release")
}

func TestRemaster_FuzzyFallbackRejectsBase_FinalFetchGuarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id=rct-156-hd") || strings.Contains(path, "dvd_id=rct156h") || strings.Contains(path, "dvd_id=rct00156h"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156", "dvd_id": null}`))
			return
		case strings.Contains(path, "combined=rct156h"):
			// Final normalized-fallback fetch returns the BASE release metadata.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156", "dvd_id": "RCT-156", "title_en": "Base 2009"}`))
			return
		case strings.Contains(path, "combined="):
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156H")
	require.Error(t, err, "base-release metadata must be rejected for a marker query")
	assert.Nil(t, result)
}

func TestRemaster_VariationProbeIdentityIsServerOwned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "combined=dv818ai"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "dv00899ai", "dvd_id": null, "title_en": "AI Remaster"}`))
			return
		case strings.Contains(path, "combined="):
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "DV-818AI")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "dv00899ai", result.ContentID, "identity comes verbatim from the server, not the constructed probe")
}

func TestRemaster_BaseQueryStillResolvesBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id=rct156"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156", "dvd_id": "RCT-156"}`))
			return
		case strings.Contains(path, "combined=1rct00156"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156", "dvd_id": "RCT-156", "title_en": "Base 2009"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "1rct00156", result.ContentID)
}
