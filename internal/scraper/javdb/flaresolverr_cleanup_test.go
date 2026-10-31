package javdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestClosePropagatesFlareSolverrCleanupFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Cmd string `json:"cmd"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")
		if req.Cmd == "sessions.create" {
			_, _ = w.Write([]byte(`{"status":"ok","session":"owned"}`))
			return
		}
		require.Equal(t, "sessions.destroy", req.Cmd)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	result, err := httpclient.NewRestyClientWithFlareSolverr(nil, models.FlareSolverrConfig{Enabled: true, URL: server.URL, Timeout: 1}, time.Second, 0)
	require.NoError(t, err)
	_, err = result.FlareSolverr.CreateSession()
	require.NoError(t, err)
	s := &scraper{flaresolverr: result.FlareSolverr}
	require.ErrorContains(t, s.Close(), "status 500")
}
