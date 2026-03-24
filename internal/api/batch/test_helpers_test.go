package batch

import (
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/websocket"
)

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.ServerDependencies {
	return testkit.CreateTestDeps(t, cfg, configFile)
}

func initTestWebSocket(t *testing.T) {
	testkit.InitTestWebSocket(t)
}

type mockWebSocketHub struct {
	mu       sync.Mutex
	messages []*websocket.ProgressMessage
}

func (m *mockWebSocketHub) BroadcastProgress(msg *websocket.ProgressMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *msg
	m.messages = append(m.messages, &cp)
	return nil
}

func (m *mockWebSocketHub) GetMessages() []*websocket.ProgressMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*websocket.ProgressMessage, len(m.messages))
	copy(out, m.messages)
	return out
}
