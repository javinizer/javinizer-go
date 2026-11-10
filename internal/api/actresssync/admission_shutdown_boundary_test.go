package actresssync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/stretchr/testify/require"
)

// Pause immediately after real task admission until shutdown has cancelled
// the server context. Shutdown must still join this admitted task.
type shutdownAfterAdmission struct {
	*core.APIRuntime
	stopped chan struct{}
	cleanup func(func())
}

func (s *shutdownAfterAdmission) TrackServerTask() func() {
	done := s.APIRuntime.TrackServerTask()
	if done == nil {
		return nil
	}
	var once sync.Once
	onceDone := func() { once.Do(done) }
	// Release admission during failed-test cleanup before the fixture joins shutdown.
	s.cleanup(onceDone)
	go func() { s.Shutdown(); close(s.stopped) }()
	<-s.ServerCtx().Done()
	return onceDone
}

func TestShutdownBetweenAdmissionAndContextLinkSkipsService(t *testing.T) {
	f := newFixture(t)
	lifetime := &shutdownAfterAdmission{APIRuntime: f.runtime, stopped: make(chan struct{}), cleanup: t.Cleanup}
	f.handler.lifetime = lifetime
	serviceResolved := false
	f.handler.service = func() syncService { serviceResolved = true; return f.service }
	result, err := f.handler.call(context.Background(), func(context.Context, syncService) (any, error) {
		t.Fatal("operation must not run after admitted server shutdown")
		return nil, nil
	})
	require.Nil(t, result)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, serviceResolved, "cancelled admission must not initialize a service")
	select {
	case <-lifetime.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled request did not release its shutdown join")
	}
}
