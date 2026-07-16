package stalltest

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// StallServer is an httptest.Server whose handler holds responses open until
// the provided stall duration elapses, then returns 200 OK. It is used by
// regression tests to assert that a configured timeout (not a hard-coded
// literal) bounds the request.
type StallServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests int
}

// New returns a StallServer that holds each response for the given duration.
func New(stall time.Duration) *StallServer {
	s := &StallServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests++
		s.mu.Unlock()
		time.Sleep(stall)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	return s
}

// Requests returns the number of requests received.
func (s *StallServer) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}
