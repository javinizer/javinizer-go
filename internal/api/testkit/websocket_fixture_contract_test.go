package testkit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/realtime"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/stretchr/testify/require"
)

func TestCreatedDepsWebSocketAllowsTestOriginAndClosesOwnedResources(t *testing.T) {
	var deps *core.APIDeps
	t.Run("owner", func(t *testing.T) {
		deps = testkit.CreateTestDeps(t, config.DefaultConfig(nil, nil), "")
		rt := testkit.GetTestRuntime(deps)
		serverCtx := rt.ServerCtx()
		router := gin.New()
		realtime.RegisterRoutes(router, rt, func(c *gin.Context) { c.Next() })
		server := httptest.NewServer(router)
		t.Cleanup(server.Close)
		// Deliberately different from the owned server's origin: this is the
		// permissive test fixture contract, not production origin policy.
		headers := http.Header{"Origin": []string{"https://fixture.example.invalid"}}
		dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
		conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/progress", headers)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
		deadline := time.Now().Add(5 * time.Second)
		require.NoError(t, conn.SetReadDeadline(deadline))
		pong := make(chan string, 1)
		conn.SetPongHandler(func(payload string) error { pong <- payload; return nil })
		type received struct {
			message ws.ProgressMessage
			err     error
		}
		result := make(chan received, 1)
		joined := make(chan struct{})
		go func() {
			defer close(joined)
			var message ws.ProgressMessage
			err := conn.ReadJSON(&message)
			result <- received{message, err}
		}()
		t.Cleanup(func() {
			_ = conn.Close()
			select {
			case <-joined:
			case <-time.After(5 * time.Second):
				t.Error("WebSocket reader did not stop")
			}
		})
		require.NoError(t, conn.WriteControl(websocket.PingMessage, []byte("fixture-ready"), deadline))
		select {
		case payload := <-pong:
			require.Equal(t, "fixture-ready", payload)
		case <-time.After(5 * time.Second):
			t.Fatal("fixture did not answer readiness ping")
		}
		// Pong proves the read pump is live, but hub registration is queued.
		// Retry delivery until receipt rather than assume scheduler ordering.
		want := ws.ProgressMessage{JobID: "fixture-job", FilePath: "owned.mp4", Progress: 50, Message: "fixture progress"}
		ticker := time.NewTicker(10 * time.Millisecond)
		t.Cleanup(ticker.Stop)
		deliveryDeadline := time.NewTimer(5 * time.Second)
		t.Cleanup(func() { deliveryDeadline.Stop() })
		delivered := false
		for !delivered {
			select {
			case <-ticker.C:
				require.NoError(t, rt.GetRuntime().WebSocketHub().BroadcastProgress(&want))
			case got := <-result:
				require.NoError(t, got.err)
				require.Equal(t, want, got.message)
				delivered = true
			case <-deliveryDeadline.C:
				t.Fatal("fixture did not deliver progress")
			}
		}
		ticker.Stop()
		deliveryDeadline.Stop()
		<-joined
		testkit.CleanupTestRuntime(t, deps)
		require.ErrorIs(t, serverCtx.Err(), context.Canceled)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		for {
			_, _, err = conn.ReadMessage()
			if err != nil {
				break
			}
		}
		var closeErr *websocket.CloseError
		require.ErrorAs(t, err, &closeErr, "expected peer close, not a read timeout")
	})
	require.NotNil(t, deps)
	_, err := deps.Repos.MovieRepo.FindByID(context.Background(), "fixture-missing")
	require.ErrorContains(t, err, "database is closed")
}
