package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// gatingPosterGen counts concurrent GeneratePoster calls and can hold the
// first call open, so the test can prove the single-scrape handlers serialize
// same-movie poster generation via the shared poster-source lock (L2).
type gatingPosterGen struct {
	inFlight int32
	peak     int32
	entered  chan struct{}
	finish   chan struct{}
	calls    int32
}

func (g *gatingPosterGen) GeneratePoster(_ context.Context, _ string, movie *models.Movie) error {
	cur := atomic.AddInt32(&g.inFlight, 1)
	for {
		p := atomic.LoadInt32(&g.peak)
		if cur <= p || atomic.CompareAndSwapInt32(&g.peak, p, cur) {
			break
		}
	}
	atomic.AddInt32(&g.calls, 1)
	if atomic.LoadInt32(&g.calls) == 1 {
		close(g.entered)
		<-g.finish
	}
	atomic.AddInt32(&g.inFlight, -1)
	return nil
}

// TestScrapeMovie_ConcurrentSameMoviePosterGenerationSerialized pins L2 at
// the single-movie API seam: two concurrent scrapes resolving to the same
// movie ID each regenerate the shared {movieID}-full.jpg; the second must
// wait on the shared per-(jobID, movieID) poster-source lock (SentinelJobID)
// instead of interleaving the cache's Remove→Rename window.
func TestScrapeMovie_ConcurrentSameMoviePosterGenerationSerialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}}}
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result:  &models.ScraperResult{Source: "r18dev", ID: "LOCK-001", Title: "Lock Test"},
	})

	gen := &gatingPosterGen{entered: make(chan struct{}), finish: make(chan struct{})}
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow), WithPosterGen(gen))
	router := gin.New()
	router.POST("/scrape", scrapeMovie(movieDeps))

	doReq := func() int {
		body, _ := json.Marshal(contracts.ScrapeRequest{ID: "LOCK-001"})
		req := httptest.NewRequest(http.MethodPost, "/scrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}

	first := make(chan int, 1)
	go func() { first <- doReq() }()

	select {
	case <-gen.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first scrape never reached poster generation")
	}

	second := make(chan int, 1)
	go func() { second <- doReq() }()

	// While the first request is mid-generation, the second must be blocked
	// on the poster-source lock — not inside GeneratePoster.
	time.Sleep(250 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&gen.peak),
		"concurrent same-movie scrapes interleaved poster generation (L2 regression)")

	close(gen.finish)
	require.Equal(t, http.StatusOK, <-first)
	require.Equal(t, http.StatusOK, <-second)
	assert.Equal(t, int32(2), atomic.LoadInt32(&gen.calls), "both scrapes generated, serialized")
	assert.Equal(t, int32(1), atomic.LoadInt32(&gen.peak))
}

// panickingPosterGen panics inside GeneratePoster once, then delegates
// normally: the recovered-panic lock-leak regression from Codex P1-2.
type panickingPosterGen struct {
	armed int32
}

func (g *panickingPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	if atomic.CompareAndSwapInt32(&g.armed, 1, 0) {
		panic("simulated poster-generation panic")
	}
	return nil
}

// TestScrapeMovie_PanicInPosterGenerationReleasesPosterLock pins L1 at the
// single-scrape API seam: the poster-source lock is refcounted, so when the
// generation panics and gin's Recovery middleware recovers it, the release
// MUST still run (deferred) — an explicit release after the call leaks the
// refcount entry and every later scrape of the same movie ID blocks forever.
func TestScrapeMovie_PanicInPosterGenerationReleasesPosterLock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}}}
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result:  &models.ScraperResult{Source: "r18dev", ID: "PANIC-001", Title: "Panic"},
	})

	gen := &panickingPosterGen{armed: 1}
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow), WithPosterGen(gen))
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/scrape", scrapeMovie(movieDeps))

	doReq := func(out chan<- int) {
		body, _ := json.Marshal(contracts.ScrapeRequest{ID: "PANIC-001"})
		req := httptest.NewRequest(http.MethodPost, "/scrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		out <- w.Code
	}

	first := make(chan int, 1)
	go doReq(first)
	require.Equal(t, http.StatusInternalServerError, <-first,
		"the recovered panic surfaces as 500")

	// Pre-fix the first request's lock entry is still held (refcount leak):
	// this second scrape of the same movie ID would block on it forever.
	second := make(chan int, 1)
	go doReq(second)
	select {
	case code := <-second:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(10 * time.Second):
		t.Fatal("second scrape blocked: poster-source lock leaked by the recovered panic (Codex P1-2)")
	}
}

// TestRescrapeMovie_PanicInPosterGenerationReleasesPosterLock pins the same
// L1 release at the single-rescrape API seam (internal/api/movie/rescrape.go).
func TestRescrapeMovie_PanicInPosterGenerationReleasesPosterLock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}}}
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result:  &models.ScraperResult{Source: "r18dev", ID: "PANIC-002", Title: "Panic"},
	})

	gen := &panickingPosterGen{armed: 1}
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow), WithPosterGen(gen))
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/movies/:id/rescrape", rescrapeMovie(movieDeps))

	doReq := func(out chan<- int) {
		body, _ := json.Marshal(contracts.RescrapeRequest{SelectedScrapers: []string{"r18dev"}})
		req := httptest.NewRequest(http.MethodPost, "/movies/PANIC-002/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		out <- w.Code
	}

	first := make(chan int, 1)
	go doReq(first)
	firstCode := <-first
	require.Contains(t, []int{http.StatusInternalServerError, http.StatusOK}, firstCode,
		"the recovered panic surfaces (500) or a cached second pass succeeds")

	second := make(chan int, 1)
	go doReq(second)
	select {
	case code := <-second:
		require.Contains(t, []int{http.StatusOK, http.StatusNotFound}, code)
	case <-time.After(10 * time.Second):
		t.Fatal("second rescrape blocked: poster-source lock leaked by the recovered panic (Codex P1-2)")
	}
}
