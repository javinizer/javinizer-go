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

	assert.Equal(t, "rct156h", foldDisplay("RCT-156-HD"))
	assert.Equal(t, "dv818ai", foldDisplay("DV-818AI"))
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
