package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestFlareSolverrCloseDestroysOwnedPersistentSession(t *testing.T) {
	var mu sync.Mutex
	var commands []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request flareSolverrRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		mu.Lock()
		commands = append(commands, request.Cmd)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch request.Cmd {
		case "sessions.create":
			_, _ = w.Write([]byte(`{"status":"ok","session":"owned"}`))
		case "request.get":
			_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"ok"}}`))
		case "sessions.destroy":
			require.Equal(t, "owned", request.Session)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected command %s", request.Cmd)
		}
	}))
	defer server.Close()
	fs, err := newFlareSolverr(&models.FlareSolverrConfig{URL: server.URL, Timeout: 1})
	require.NoError(t, err)
	_, _, err = fs.ResolveURL("https://example.com")
	require.NoError(t, err)
	require.NoError(t, fs.Close())
	require.NoError(t, fs.Close())
	mu.Lock()
	require.Equal(t, []string{"sessions.create", "request.get", "sessions.destroy"}, commands)
	mu.Unlock()
}

func TestFlareSolverrCloseOwnsOnlyCreatedSessions(t *testing.T) {
	var mu sync.Mutex
	var destroyed []string
	creates := 0
	failDestroy := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req flareSolverrRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch req.Cmd {
		case "sessions.create":
			creates++
			fmt.Fprintf(w, `{"status":"ok","session":"owned-%d"}`, creates)
		case "request.get":
			_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"ok"}}`))
		case "sessions.destroy":
			destroyed = append(destroyed, req.Session)
			if failDestroy {
				failDestroy = false
				w.WriteHeader(500)
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected command %q", req.Cmd)
		}
	}))
	defer server.Close()
	fs, err := newFlareSolverr(&models.FlareSolverrConfig{URL: server.URL, Timeout: 1})
	require.NoError(t, err)
	first, err := fs.CreateSession()
	require.NoError(t, err)
	require.Error(t, fs.DestroySession(first))
	_, err = fs.CreateSession()
	require.NoError(t, err)
	// Borrowed IDs must not become owned merely because they are used.
	_, _, err = fs.ResolveURLWithSession("https://example.com", "borrowed")
	require.NoError(t, err)
	require.NoError(t, fs.Close())
	mu.Lock()
	require.Equal(t, []string{"owned-1", "owned-1", "owned-2"}, destroyed)
	mu.Unlock()
	_, err = fs.CreateSession()
	require.ErrorIs(t, err, errFlareSolverrClosed)
	_, _, err = fs.ResolveURL("https://example.com")
	require.ErrorIs(t, err, errFlareSolverrClosed)
	_, _, err = fs.ResolveURLWithSession("https://example.com", "borrowed")
	require.ErrorIs(t, err, errFlareSolverrClosed)
	require.ErrorIs(t, fs.DestroySession("borrowed"), errFlareSolverrClosed)
	require.Empty(t, fs.persistentSessionID)
	count := 0
	fs.sessions.Range(func(_, _ any) bool { count++; return true })
	require.Zero(t, count)
}

func TestFlareSolverrCloseWaitsForAdmittedRequest(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var commands []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req flareSolverrRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		mu.Lock()
		commands = append(commands, req.Cmd)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch req.Cmd {
		case "sessions.create":
			_, _ = w.Write([]byte(`{"status":"ok","session":"owned"}`))
		case "request.get":
			close(entered)
			<-release
			_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"ok"}}`))
		case "sessions.destroy":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer server.Close()
	fs, err := newFlareSolverr(&models.FlareSolverrConfig{URL: server.URL, Timeout: 2})
	require.NoError(t, err)
	requested := make(chan error, 1)
	go func() { _, _, err := fs.ResolveURL("https://example.com"); requested <- err }()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- fs.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned while request active: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-requested)
	require.NoError(t, <-closed)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"sessions.create", "request.get", "sessions.destroy"}, commands)
}

func TestFlareSolverrCloseBoundsCleanupAndRemembersFailure(t *testing.T) {
	fs, err := newFlareSolverr(&models.FlareSolverrConfig{URL: "http://fixture.invalid", Timeout: 60})
	require.NoError(t, err)
	fs.ownedSessions = map[string]struct{}{"owned-1": {}, "owned-2": {}}
	calls := 0
	var firstDeadline time.Time
	fs.client.SetTransport(recoveryRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := req.Context().Deadline()
		require.True(t, ok)
		if firstDeadline.IsZero() {
			firstDeadline = deadline
		} else {
			require.Equal(t, firstDeadline, deadline)
		}
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), flareSolverrCleanupTimeout)
		return nil, context.DeadlineExceeded
	}))
	err = fs.Close()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, err, fs.Close())
	require.Equal(t, 2, calls)
}

func TestFlareSolverrCloseWithoutSession(t *testing.T) {
	fs := &FlareSolverr{}
	require.NoError(t, fs.Close())
	require.NoError(t, fs.Close())
}

func TestFlareSolverrCloseRetriesFailedResetAndConcurrentClosers(t *testing.T) {
	var mu sync.Mutex
	creates := 0
	var destroyed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req flareSolverrRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch req.Cmd {
		case "sessions.create":
			creates++
			fmt.Fprintf(w, `{"status":"ok","session":"owned-%d"}`, creates)
		case "request.get":
			if req.Session == "owned-1" {
				w.WriteHeader(500)
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"ok"}}`))
		case "sessions.destroy":
			destroyed = append(destroyed, req.Session)
			if len(destroyed) == 1 {
				w.WriteHeader(500)
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected command %q", req.Cmd)
		}
	}))
	defer server.Close()
	fs, err := newFlareSolverr(&models.FlareSolverrConfig{URL: server.URL, Timeout: 1})
	require.NoError(t, err)
	_, _, err = fs.ResolveURL("https://example.com")
	require.NoError(t, err)
	results := make(chan error, 8)
	for range 8 {
		go func() { results <- fs.Close() }()
	}
	for range 8 {
		require.NoError(t, <-results)
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, creates)
	require.Equal(t, []string{"owned-1", "owned-1", "owned-2"}, destroyed)
}
