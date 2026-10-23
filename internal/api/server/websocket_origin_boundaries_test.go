package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func TestServerWebSocketOriginPolicyWithEmptyAllowlist(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedOrigins = nil
	deps := createTestDeps(t, cfg, filepath.Join(t.TempDir(), "config.yaml"))
	rt := testkit.GetTestRuntime(deps)
	NewServer(rt)
	t.Cleanup(func() { cleanupServerHub(t, deps) })
	upgrader := rt.GetRuntime().WebSocketUpgrader()
	require.NotNil(t, upgrader.CheckOrigin)
	for _, tc := range []struct {
		name, origin string
		allowed      bool
	}{
		{"non-browser client", "", true},
		{"same origin", "http://localhost:8080", true},
		{"foreign origin", "http://untrusted.invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/ws/progress", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			require.Equal(t, tc.allowed, upgrader.CheckOrigin(req))
		})
	}
}
