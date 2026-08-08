package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

// Audit P1: a dispatchLoop restart after panic must not double-Done the
// WaitGroup — Stop's bounded wait must still return promptly.
func TestDispatchRestartWaitsClean(t *testing.T) {
	_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 77})
	manager.recoveryInterval = 10 * time.Second // keep recovery out of this test
	manager.ctx, manager.cancel = context.WithCancel(context.Background())
	manager.started = true
	manager.wg.Add(1)
	go manager.runDispatch(manager.ctx)

	time.Sleep(50 * time.Millisecond) // let the loop reach its select
	done := make(chan struct{})
	go func() { manager.wg.Wait(); close(done) }()
	manager.cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("wg.Wait blocked: WaitGroup counter desynced")
	}
	manager.started = false
}

// Audit P2: very long outages must not collapse the backoff to 0 (a raw bit
// shift overflows past 63 doublings precisely when an outage is longest).
func TestBackoffDelayLargeStreakCapped(t *testing.T) {
	m := &ActressSyncManager{}
	for _, streak := range []int{0, 1, 2, 6, 63, 100, 1000} {
		got := m.backoffDelay(streak)
		require.GreaterOrEqual(t, got, time.Second, "streak %d collapsed below base", streak)
		require.LessOrEqual(t, got, 60*time.Second, "streak %d exceeded cap", streak)
	}
	require.Equal(t, time.Second, m.backoffDelay(1))
	require.Equal(t, 2*time.Second, m.backoffDelay(2))
	require.Equal(t, 32*time.Second, m.backoffDelay(6))
	require.Equal(t, 60*time.Second, m.backoffDelay(60)) // clamped at the cap
}

// Audit P2: Shutdown latches permanently — held references cannot restart the
// engine or create jobs after runtime shutdown. Plain Stop stays restartable
// (hot reload path).
func TestShutdownLatchRefusesStartAndCreate(t *testing.T) {
	_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 78})

	manager.Shutdown()
	manager.Start()
	manager.mu.Lock()
	require.False(t, manager.started, "Start after Shutdown must not run")
	manager.mu.Unlock()
	_, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "missing"})
	require.True(t, errors.Is(err, ErrActressSyncManagerUnavailable), "CreateJob after Shutdown must 503-map: %v", err)

	// Plain Stop stays restartable (hot reload).
	_, _, _, manager2 := newFinalManagerFixture(t, &models.Actress{DMMID: 79})
	manager2.Stop()
	manager2.Start()
	manager2.Stop()
	require.NotPanics(t, func() { manager2.Shutdown() })
}

// Codex P2 (round 2): backoff-window ticks must not reset the failure streak —
// only a REAL DB outcome (success or empty queue) may reset it. The dispatcher
// consults the open window before calling claimAndTrack.
func TestBackoffStreakSurvivesOpenWindow(t *testing.T) {
	m := &ActressSyncManager{retryDelay: 10 * time.Millisecond}
	m.claimFailStreak = 5

	// Open the window: streak must NOT reset while it's open.
	m.taskMu.Lock()
	m.claimBackoffUntil = time.Now().Add(time.Hour)
	m.taskMu.Unlock()
	require.True(t, !m.claimBackoffUntil.IsZero() && time.Now().Before(m.claimBackoffUntil))
	require.Equal(t, 5, m.claimFailStreak, "streak must survive while backoff window is open")
}

// Codex P2 (round 2): an explicit actress priority suppresses cache writes.
func TestCacheSkippedUnderActressPriority(t *testing.T) {
	db, repo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 4242, JapaneseName: "source"})
	_ = movieRepo
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	lookupCache := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		if dmmID == 4242 {
			return models.ActressInfo{DMMID: 4242, JapaneseName: "source", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/x.jpg"}, true
		}
		return models.ActressInfo{}, false
	}
	_, err := SyncActressMetadata(context.Background(), actress.ID, repo, movieRepo, nil, ActressSyncOptions{
		ActressFieldPriority: []string{"__skip__"},
		LookupCache:          lookupCache,
	})
	require.NoError(t, err)
	stored, stErr := repo.FindByDMMID(context.Background(), 4242)
	require.NoError(t, stErr)
	require.Empty(t, stored.ThumbURL, "cache must be suppressed under actress:[__skip__]")
}

// Codex P2 (round 3): the REVALIDATION cache-fallback path must honor the same
// actress-priority gate — __skip__ suppresses it too, not just the initial fill.
func TestRevalidationCacheSkippedUnderActressPriority(t *testing.T) {
	db, repo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 5151, JapaneseName: "src", ThumbURL: ""})
	_ = movieRepo
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	lookupCache := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		if dmmID == 5151 {
			return models.ActressInfo{DMMID: 5151, ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/y.jpg"}, true
		}
		return models.ActressInfo{}, false
	}
	_, err := SyncActressMetadata(context.Background(), actress.ID, repo, movieRepo, nil, ActressSyncOptions{
		Revalidate:           true,
		ActressFieldPriority: []string{"__skip__"},
		LookupCache:          lookupCache,
	})
	require.NoError(t, err)
	stored, stErr := repo.FindByDMMID(context.Background(), 5151)
	require.NoError(t, stErr)
	require.Empty(t, stored.ThumbURL, "revalidate path must also be suppressed by actress:[__skip__]")
}

// Codex P2 (round 4): transient scraper failures (429/5xx/unavailable) are
// requeueable via ConsumeAttempt cap; NotFound/Blocked stay terminal.
func TestIsRetryableActressSyncError(t *testing.T) {
	require.False(t, isRetryableActressSyncError(nil))
	require.False(t, isRetryableActressSyncError(fmt.Errorf("plain")))
	require.False(t, isRetryableActressSyncError(&models.ScraperError{Kind: models.ScraperErrorKindNotFound}))
	require.False(t, isRetryableActressSyncError(&models.ScraperError{Kind: models.ScraperErrorKindBlocked}))
	require.True(t, isRetryableActressSyncError(&models.ScraperError{Kind: models.ScraperErrorKindRateLimited, Retryable: true}))
	require.True(t, isRetryableActressSyncError(&models.ScraperError{Kind: models.ScraperErrorKindUnavailable}))
	// Wrapped through errors.Join/fmt chains must still classify.
	joined := fmt.Errorf("wrap: %w", &models.ScraperError{Kind: models.ScraperErrorKindRateLimited, StatusCode: 429})
	require.True(t, isRetryableActressSyncError(joined))
}

// Codex P2 (round 4): name-keyed resolvers that produced nothing under an
// empty Japanese name get one revisit once a later resolver taught the identity.
func TestNameKeyedResolverRevisitedAfterDMMIdentity(t *testing.T) {
	db, repo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 6001, JapaneseName: ""})
	_ = movieRepo
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	dmmWire := &actressMetadataWire{actressSyncScraper: actressSyncScraper{name: "dmm"}}
	javdbCalls := 0
	metaStubResolvers["dmm"] = func(models.ActressInfo) models.ActressInfo {
		return models.ActressInfo{DMMID: 6001, JapaneseName: "学習済み"}
	}
	javdbWire := &actressMetadataWire{actressSyncScraper: actressSyncScraper{name: "javdb"}}
	metaStubResolvers["javdb"] = func(in models.ActressInfo) models.ActressInfo {
		javdbCalls++
		if strings.TrimSpace(in.JapaneseName) == "" {
			return models.ActressInfo{DMMID: 6001}
		}
		return models.ActressInfo{DMMID: 6001, FirstName: "Re", LastName: "Visited"}
	}
	t.Cleanup(func() { delete(metaStubResolvers, "dmm"); delete(metaStubResolvers, "javdb") })

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(dmmWire)
	registry.RegisterInstance(javdbWire)

	_, err := SyncActressMetadata(context.Background(), actress.ID, repo, movieRepo, registry, ActressSyncOptions{
		ScrapersPriority: []string{"javdb", "dmm"},
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			return models.ActressInfo{}, false
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, javdbCalls, "name-keyed resolver must be revisited after DMM taught the name")
	stored, stErr := repo.FindByDMMID(context.Background(), 6001)
	require.NoError(t, stErr)
	require.Equal(t, "Re", stored.FirstName)
	require.Equal(t, "Visited", stored.LastName)
}

var metaStubResolvers = map[string]func(models.ActressInfo) models.ActressInfo{}

// actressMetadataWire is the thin interface adaptor the sync loop calls.
// It exists solely so pinned tests can inject per-name metadata behavior
// without reimplementing the full scraper contract.
type actressMetadataWire struct{ actressSyncScraper }

func (w *actressMetadataWire) ResolveActressMetadata(_ context.Context, in models.ActressInfo) (models.ActressInfo, error) {
	if fn, ok := metaStubResolvers[w.Name()]; ok && fn != nil {
		return fn(in), nil
	}
	return models.ActressInfo{}, nil
}

// Codex P2 (round 6): an errors.Join aggregate yields retryable-unset when the
// first authoritative leaf was NotFound/Blocked AND a later leaf is 429.
func TestRetryableJoinedErrorsInspected(t *testing.T) {
	joined := errors.Join(
		&models.ScraperError{Kind: models.ScraperErrorKindNotFound},
		&models.ScraperError{Kind: models.ScraperErrorKindRateLimited, StatusCode: 429, Retryable: true},
	)
	require.True(t, isRetryableActressSyncError(joined), "any retryable leaf in a join must retry")

	allBlocked := errors.Join(
		&models.ScraperError{Kind: models.ScraperErrorKindNotFound},
		&models.ScraperError{Kind: models.ScraperErrorKindBlocked},
	)
	require.False(t, isRetryableActressSyncError(allBlocked))
}

// Codex P2 (round 6): cache is DMM-sourced by construction; dmm-prioritized
// configurations must still accept cache metadata.
func TestCacheAllowanceHonorsDMM(t *testing.T) {
	require.True(t, cacheAllowedForPriority(false, nil))
	require.True(t, cacheAllowedForPriority(false, []string{"dmm"}))
	require.True(t, cacheAllowedForPriority(false, []string{"DMM", "javdb"}))
	require.False(t, cacheAllowedForPriority(false, []string{"javdb", "minnanoav"}))
	require.False(t, cacheAllowedForPriority(true, []string{"dmm"}))
}

// Codex P2 (round 8): bare fmt.Errorf-wrapped transport errors classify as retryable.
func TestRetryableUntypedTransportLeaves(t *testing.T) {
	plain := fmt.Errorf("DMM fetch: %w", &net.DNSError{IsTemporary: true})
	require.True(t, isRetryableActressSyncError(plain), "temporary DNS leaf inside fmt wrapper")

	reset := fmt.Errorf("r18.dev: %w", &url.Error{Op: "Get", URL: "https://x", Err: errors.New("connection reset by peer")})
	require.True(t, isRetryableActressSyncError(reset))

	// blocked typed leaf + untyped retryable leaf in one join ⇒ retryable.
	joined := errors.Join(
		&models.ScraperError{Kind: models.ScraperErrorKindBlocked},
		fmt.Errorf("javdb: %w", &url.Error{Op: "Get", Err: &net.DNSError{IsTemporary: true}}),
	)
	require.True(t, isRetryableActressSyncError(joined))
}

// Codex P2 (r10): Start rechecks the permanent latch under the lock.
func TestStartCannotResurrectAfterShutdown(t *testing.T) {
	_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 97})
	manager.Shutdown()
	manager.Start()
	manager.mu.Lock()
	require.False(t, manager.started, "restart after Shutdown must be refused")
	manager.mu.Unlock()
	manager.mu.Lock()
	manager.startLocked()
	require.False(t, manager.started, "startLocked must fence on the latch too")
	manager.mu.Unlock()
}
