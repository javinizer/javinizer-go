package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/server"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPublicServerAcceptNegotiationPreservesFallbackContract(t *testing.T) {
	previousMode := gin.Mode()
	t.Cleanup(func() { gin.SetMode(previousMode) })
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join(t.TempDir(), "work")
	deps := testkit.CreateTestDeps(t, cfg, "")
	router := server.NewServer(testkit.GetTestRuntime(deps))
	const path = "/accept-contract/unmatched-page"
	for _, tc := range []struct {
		name, method, accept string
		html                 bool
	}{
		{"html positive control", http.MethodGet, "text/html", true},
		{"empty", http.MethodGet, "", false},
		{"whitespace", http.MethodGet, " \t ", false},
		{"empty comma parts", http.MethodGet, ", ,", false},
		{"empty semicolon segments", http.MethodGet, ";;;, ;", false},
		{"JSON", http.MethodGet, "application/json", false},
		{"wildcard alone", http.MethodGet, "*/*", false},
		{"mixed case and whitespace", http.MethodGet, " TEXT/HTML ; Q=0.5 ", true},
		{"mixed leading empty parts", http.MethodGet, ", ;,application/json, text/html;q=0.1,", true},
		{"zero quality", http.MethodGet, "text/html;q=0", false},
		{"zero one decimal", http.MethodGet, "text/html;q=0.0", false},
		{"zero two decimals", http.MethodGet, "text/html;q=0.00", false},
		{"zero three decimals", http.MethodGet, "text/html;q=0.000", false},
		{"later positive alternative", http.MethodGet, "text/html;q=0, text/html;q=0.5", true},
		{"non-quality parameter", http.MethodGet, "text/html;charset=utf-8;q=0.5", true},
		{"HEAD html", http.MethodHead, "text/html", true},
		{"POST html stays JSON", http.MethodPost, "text/html", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, path, nil)
			request.Header.Set("Accept", tc.accept)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if tc.html && tc.method == http.MethodHead {
				require.Equal(t, http.StatusNoContent, response.Code)
				require.Empty(t, response.Body.String())
			} else if tc.html {
				require.Equal(t, http.StatusOK, response.Code, "embedded UI must be available: no silent skip")
				require.Contains(t, response.Header().Get("Content-Type"), "text/html")
				require.Contains(t, strings.ToLower(response.Body.String()), "<html")
				require.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
				require.Contains(t, response.Header().Values("Vary"), "Cookie")
			} else {
				require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
				require.Contains(t, response.Header().Get("Content-Type"), "application/json")
				var body map[string]any
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.Equal(t, map[string]any{"error": "Not Found", "message": "The requested resource does not exist", "path": path, "method": tc.method}, body)
			}
		})
	}
}
