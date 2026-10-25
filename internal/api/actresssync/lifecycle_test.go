package actresssync

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShutdownCancelsAndJoinsAllServiceOperations(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/status", ""}, {"GET", "/sources", ""}, {"GET", "/jobs", ""}, {"GET", "/jobs/" + testJobID, ""}, {"GET", "/jobs/" + testJobID + "/tasks", ""}, {"POST", "/reviews", reviewBody}, {"POST", "/jobs", confirmBody}, {"POST", "/jobs/" + testJobID + "/cancel", cancelBody},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			f := newFixture(t)
			entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			f.service.hook = func(ctx context.Context, _ serviceCall) error {
				close(entered)
				<-ctx.Done()
				close(cancelled)
				<-release
				return ctx.Err()
			}
			response := make(chan *httptest.ResponseRecorder, 1)
			go func() { response <- f.serve(newRequest(tc.method, tc.path, tc.body)) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("service was not entered")
			}
			shutdown := make(chan struct{})
			go func() { f.runtime.Shutdown(); close(shutdown) }()
			select {
			case <-cancelled:
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown did not cancel the request")
			}
			select {
			case <-shutdown:
				t.Fatal("shutdown returned before the database-bearing operation exited")
			default:
			}
			close(release)
			select {
			case <-shutdown:
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown did not join the request")
			}
			select {
			case w := <-response:
				require.Equal(t, 503, w.Code, w.Body.String())
			case <-time.After(3 * time.Second):
				t.Fatal("request did not exit")
			}
		})
	}
}

func TestClientCancellationPropagatesWithoutStoppingServer(t *testing.T) {
	f := newFixture(t)
	entered := make(chan struct{})
	f.service.hook = func(ctx context.Context, _ serviceCall) error { close(entered); <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := newRequest("GET", "/jobs", "").WithContext(ctx)
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() { response <- f.serve(r) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case w := <-response:
		require.Equal(t, 503, w.Code)
	case <-time.After(3 * time.Second):
		t.Fatal("request did not observe cancellation")
	}
	require.NoError(t, f.runtime.ServerCtx().Err())
}

func TestShutdownRejectsLateServiceAcquisition(t *testing.T) {
	f := newFixture(t)
	f.runtime.Shutdown()
	acquired := false
	f.handler.service = func() syncService { acquired = true; return f.service }
	w := f.serve(newRequest("POST", "/jobs", confirmBody))
	require.Equal(t, 503, w.Code)
	require.False(t, acquired)
	require.Empty(t, f.service.recorded())
}

func TestAlreadyCancelledRequestNeverAcquiresService(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	acquired := false
	f.handler.service = func() syncService { acquired = true; return f.service }
	w := f.serve(newRequest("GET", "/status", "").WithContext(ctx))
	require.Equal(t, 503, w.Code)
	require.False(t, acquired)
	require.Empty(t, f.service.recorded())
}

type blockedWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release <-chan struct{}
}

func (w *blockedWriter) Write(data []byte) (int, error) {
	close(w.entered)
	<-w.release
	return w.ResponseRecorder.Write(data)
}

func TestBlockedResponseWriteCannotPinDatabaseShutdown(t *testing.T) {
	for _, serviceErr := range []error{nil, context.DeadlineExceeded} {
		name := "success"
		if serviceErr != nil {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.service.err = serviceErr
			release := make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			writer := &blockedWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: release}
			response := make(chan struct{})
			go func() { f.router.ServeHTTP(writer, newRequest("GET", "/status", "")); close(response) }()
			select {
			case <-writer.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("response write did not start")
			}
			shutdown := make(chan struct{})
			go func() { f.runtime.Shutdown(); close(shutdown) }()
			select {
			case <-shutdown:
			case <-time.After(3 * time.Second):
				t.Fatal("a blocked response write pinned the database join")
			}
			select {
			case <-response:
				t.Fatal("writer fixture was not blocked")
			default:
			}
			require.Len(t, f.service.recorded(), 1)
			close(release)
			select {
			case <-response:
			case <-time.After(3 * time.Second):
				t.Fatal("response did not exit")
			}
		})
	}
}
