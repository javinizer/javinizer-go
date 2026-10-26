package actresssync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/token"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/database"
)

const testReviewID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testJobID = "name-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const testSourceKey = "v1:r18dev:NDI"
const reviewBody = `{"choices":[{"actress_id":7,"source_key":"v1:r18dev:NDI"}]}`
const confirmBody = `{"review_id":"` + testReviewID + `","confirmed":true}`
const cancelBody = `{"confirmed":true}`

// Embedding the provider contract (not testkit.NoOpAuth) means the production
// RequireTokenOrSession middleware really verifies our fixture credentials.
type authStub struct{ commandutil.AuthProvider }

func (authStub) IsInitialized() bool { return true }
func (authStub) AuthenticateSession(id string) (string, error) {
	switch id {
	case "session-alice", "session-alice-rotated":
		return "alice", nil
	case "session-bob":
		return "bob", nil
	default:
		return "", auth.ErrInvalidSession
	}
}
func (authStub) ValidateToken(_ context.Context, hash string) (string, error) {
	switch hash {
	case token.HashToken(token.TokenPrefix + "alice"):
		return "token-alice", nil
	case token.HashToken(token.TokenPrefix + "bob"):
		return "token-bob", nil
	default:
		return "", errors.New("bad credential")
	}
}
func (authStub) UpdateTokenLastUsed(context.Context, string) error { return nil }

type serviceCall struct {
	method        string
	principal     string
	id            string
	query         string
	digest        string
	offset, limit int
	choices       []database.ActressSyncChoice
	confirmed     bool
}

type serviceStub struct {
	mu    sync.Mutex
	calls []serviceCall
	err   error
	hook  func(context.Context, serviceCall) error
}

func (s *serviceStub) called(ctx context.Context, call serviceCall) error {
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
	if s.hook != nil {
		return s.hook(ctx, call)
	}
	return s.err
}
func (s *serviceStub) recorded() []serviceCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]serviceCall(nil), s.calls...)
}
func (s *serviceStub) Info(ctx context.Context) (actressnamesync.SourceInfo, error) {
	err := s.called(ctx, serviceCall{method: "status"})
	return actressnamesync.SourceInfo{Enabled: true, Available: true}, err
}
func (s *serviceStub) SourcesAt(ctx context.Context, query string, offset, limit int, digest string) (*actressnamesync.SourcePage, error) {
	err := s.called(ctx, serviceCall{method: "sources", query: query, offset: offset, limit: limit, digest: digest})
	return &actressnamesync.SourcePage{Items: []actressnamesync.SourceChoice{{SourceKey: testSourceKey, JapaneseName: "名前"}}}, err
}
func (s *serviceStub) ReviewAt(ctx context.Context, principal string, choices []database.ActressSyncChoice, digest string) (*actressnamesync.Review, error) {
	err := s.called(ctx, serviceCall{method: "review", principal: principal, choices: choices, digest: digest})
	return &actressnamesync.Review{ReviewID: testReviewID}, err
}
func (s *serviceStub) Confirm(ctx context.Context, principal, id string, confirmed bool) (*actressnamesync.JobView, error) {
	err := s.called(ctx, serviceCall{method: "confirm", principal: principal, id: id, confirmed: confirmed})
	return &actressnamesync.JobView{ID: testJobID}, err
}
func (s *serviceStub) Job(ctx context.Context, id string) (*actressnamesync.JobView, error) {
	err := s.called(ctx, serviceCall{method: "job", id: id})
	return &actressnamesync.JobView{ID: id}, err
}
func (s *serviceStub) Tasks(ctx context.Context, id string, offset, limit int) (*actressnamesync.TaskPage, error) {
	err := s.called(ctx, serviceCall{method: "tasks", id: id, offset: offset, limit: limit})
	return &actressnamesync.TaskPage{Items: []actressnamesync.TaskView{{ActressID: 7, Status: "applied"}}}, err
}
func (s *serviceStub) ListJobs(ctx context.Context, cursor string, limit int) (*actressnamesync.JobPage, error) {
	err := s.called(ctx, serviceCall{method: "jobs", id: cursor, limit: limit})
	return &actressnamesync.JobPage{Items: []actressnamesync.JobView{{ID: testJobID}}}, err
}
func (s *serviceStub) Cancel(ctx context.Context, id string) (*actressnamesync.JobView, error) {
	err := s.called(ctx, serviceCall{method: "cancel", id: id})
	return &actressnamesync.JobView{ID: id, Cancelled: true}, err
}

type fixture struct {
	router  *gin.Engine
	runtime *core.APIRuntime
	service *serviceStub
	handler *handler
}

func newFixture(t *testing.T, writeMiddleware ...gin.HandlerFunc) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	deps := &core.APIDeps{Auth: authStub{}}
	rt := core.NewAPIRuntime(deps)
	t.Cleanup(rt.Shutdown)
	svc := &serviceStub{}
	h := &handler{service: func() syncService { return svc }, lifetime: rt, origins: func() []string { return []string{"https://trusted.example", "*", "null", "wails://wails.localhost"} }}
	router := gin.New()
	protected := router.Group("/api/v1", auth.RequireTokenOrSession(deps))
	writes := protected.Group("", writeMiddleware...)
	h.register(protected, writes)
	return &fixture{router: router, runtime: rt, service: svc, handler: h}
}

func newRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "http://api.example/api/v1"+routePrefix+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token.TokenPrefix+"alice")
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(intentHeader, intentValue)
	}
	return r
}

func (f *fixture) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

func (s *serviceStub) Reload(ctx context.Context) (actressnamesync.SourceInfo, error) {
	err := s.called(ctx, serviceCall{method: "reload"})
	return actressnamesync.SourceInfo{Enabled: true, Available: true}, err
}
