package javstash

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchSceneRequestWireBody(t *testing.T) {
	const term = "雪 <&> \"Scene\""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "fixture-key", r.Header.Get("ApiKey"))
		raw, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var sent struct {
			Query string `json:"query"`
		}
		if !assert.NoError(t, json.Unmarshal(raw, &sent)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		assert.Contains(t, sent.Query, "query searchScene($term: String!, $limit: Int)")
		assert.Contains(t, sent.Query, "searchScene(term: $term, limit: $limit)")
		query, err := json.Marshal(sent.Query)
		assert.NoError(t, err)
		want := `{"query":` + string(query) + `,"variables":{"limit":5,"term":"雪 \u003c\u0026\u003e \"Scene\""}}`
		assert.Equal(t, want, string(raw))
		assert.Equal(t, int64(len(want)), r.ContentLength)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"searchScene":[{"id":"fixture","code":"RECORD-100","title":"Neutral entry"}]}}`)
	}))
	defer server.Close()

	s := &scraper{
		client:      NewTestClient(server),
		apiKey:      "fixture-key",
		baseURL:     server.URL + "/graphql",
		rateLimiter: ratelimit.NewLimiter(0),
	}
	got, err := s.Search(context.Background(), "  "+term+"  ")
	require.NoError(t, err)
	require.NotNil(t, got)
}
