package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestMatcherRawHDToR18Search(t *testing.T) {
	const cid = "h_003abc00123hd"
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	matched := m.MatchFile(models.FileMatchInfo{Name: cid + ".mkv", Extension: ".mkv"})
	require.NotNil(t, matched)
	assert.Equal(t, strings.ToUpper(cid), matched.ID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "combined="+cid+"/json") {
			_, _ = w.Write([]byte(`{"content_id":"` + cid + `","dvd_id":null,"title_en":"Exact HD"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), matched.ID)
	require.NoError(t, err)
	assert.Equal(t, cid, result.ContentID)
	assert.Equal(t, "Exact HD", result.Title)
}
