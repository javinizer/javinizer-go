package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/api/token"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/require"
)

const offlineSyncAPI = "/api/v1/actress-name-sync"

// These probes only coordinate real repository calls. Neither authentication,
// source validation, SQLite transactions nor durable receipts are mocked.
type offlineSyncApplyResult struct {
	receipt *database.ActressSyncTask
	err     error
}

type offlineSyncProblemResult struct {
	code string
	err  error
}

type offlineSyncRepositoryProbe struct {
	*database.ActressRepository
	beforeDue   func(context.Context)
	afterDue    func()
	beforeApply func(context.Context)
	applied     chan offlineSyncApplyResult
	problem     chan offlineSyncProblemResult
}

func (p *offlineSyncRepositoryProbe) DueOfflineNameSyncJobs(ctx context.Context, now time.Time, cursor string, limit int) ([]database.ActressSyncJob, error) {
	if p.beforeDue != nil {
		p.beforeDue(ctx)
	}
	jobs, err := p.ActressRepository.DueOfflineNameSyncJobs(ctx, now, cursor, limit)
	if p.afterDue != nil {
		p.afterDue()
	}
	return jobs, err
}

func (p *offlineSyncRepositoryProbe) ApplyOfflineNameSync(ctx context.Context, lease database.ActressSyncLease, snapshot *actresscache.Snapshot, now time.Time) (*database.ActressSyncTask, error) {
	if p.beforeApply != nil {
		p.beforeApply(ctx)
	}
	receipt, err := p.ActressRepository.ApplyOfflineNameSync(ctx, lease, snapshot, now)
	select {
	case p.applied <- offlineSyncApplyResult{receipt: receipt, err: err}:
	default:
	}
	return receipt, err
}

func (p *offlineSyncRepositoryProbe) RecordOfflineNameSyncJobProblem(ctx context.Context, id, code string, now time.Time) error {
	err := p.ActressRepository.RecordOfflineNameSyncJobProblem(ctx, id, code, now)
	select {
	case p.problem <- offlineSyncProblemResult{code: code, err: err}:
	default:
	}
	return err
}

type offlineSyncFixture struct {
	root              string
	artifactDirectory string
	digest            string
	sourceKeys        []string
	ownerToken        string
	otherToken        string
}

// Exercise the real converter and publisher on supported writer platforms. The
// Windows-only fallback installs canonical synthetic fixture bytes, not a fake
// reader: Windows still runs the complete native artifact-read/service/API path.
func newOfflineSyncFixture(t *testing.T) *offlineSyncFixture {
	t.Helper()
	f := &offlineSyncFixture{root: t.TempDir()}
	f.artifactDirectory = filepath.Join(f.root, "artifacts")
	require.NoError(t, os.Mkdir(f.artifactDirectory, 0700))
	limits := offline.DefaultLimits()
	dump := "COPY public.derived_actress (id, name_kanji, name_romaji) FROM stdin;\nr18-only-900\t新しい名前\tUntrusted source alias\nr18-only-901\t別の新しい名前\tOther source alias\n\\.\n"
	snapshot, receipt, err := r18dump.Convert(t.Context(), strings.NewReader(dump), actresscache.Provenance{
		SourceURL: "https://example.invalid/fixture-dump.sql", ParserRevision: "api-integration-fixture-v1", ObservedAt: time.Now().UTC().Add(-time.Minute),
	}, limits.Source)
	require.NoError(t, err)
	f.digest = snapshot.Digest()
	for _, sourceID := range []string{"r18-only-900", "r18-only-901"} {
		key, err := actresscache.SourceKey("r18dev", sourceID)
		require.NoError(t, err)
		f.sourceKeys = append(f.sourceKeys, key)
	}
	store, err := artifactstore.Open(f.artifactDirectory, limits.Store)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	result, err := store.Publish(t.Context(), bytes.NewReader(snapshot.Bytes()), receipt)
	if runtime.GOOS == "windows" {
		require.Error(t, err, "Windows publication must remain fail-closed until its durability contract is supported")
		require.False(t, result.Published)
		manifest := artifactstore.Manifest{Version: 1, Scope: "supplied-file-only", ArtifactSHA256: f.digest, ArtifactBytes: int64(len(snapshot.Bytes())), Source: receipt}
		data, marshalErr := json.Marshal(manifest)
		require.NoError(t, marshalErr)
		require.NoError(t, os.WriteFile(filepath.Join(f.artifactDirectory, f.digest+".ndjson"), snapshot.Bytes(), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(f.artifactDirectory, "current.json"), append(data, '\n'), 0600))
	} else {
		require.NoError(t, err)
		require.True(t, result.Published)
		require.True(t, result.FilesSynced)
		require.True(t, result.DirectorySynced)
	}
	view, err := store.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, f.digest, view.Snapshot.Digest())
	return f
}

type offlineSyncServer struct {
	db        *database.DB
	rt        *core.APIRuntime
	router    http.Handler
	auth      *auth.AuthManager
	probe     *offlineSyncRepositoryProbe
	closeOnce sync.Once
}

func (f *offlineSyncFixture) open(t *testing.T, configure func(*offlineSyncRepositoryProbe)) *offlineSyncServer {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(f.root, "names.db") + "?_journal_mode=WAL&_busy_timeout=10000", LogLevel: "silent"})
	require.NoError(t, err)
	s := &offlineSyncServer{db: db}
	// Register ownership before migration or other setup can fail. The runtime
	// joins workers before the actual file database and its WAL handles close.
	t.Cleanup(func() { s.close(t) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	repos := db.Repositories()
	manager, err := auth.NewAuthManager(filepath.Join(f.root, "config.yaml"), time.Hour, func(string) (string, bool) { return "", false })
	require.NoError(t, err)
	if !manager.IsInitialized() {
		require.NoError(t, manager.Setup("offline-sync-owner", "disposable-test-password"))
	}
	manager.SetApiTokenRepo(repos.ApiTokenRepo)
	s.auth = manager
	if f.ownerToken == "" {
		for _, principal := range []struct {
			id     string
			target *string
		}{
			{"offline-sync-owner", &f.ownerToken}, {"offline-sync-other", &f.otherToken},
		} {
			full, prefix, err := token.GenerateToken()
			require.NoError(t, err)
			require.NoError(t, repos.ApiTokenRepo.Create(t.Context(), &models.ApiToken{ID: principal.id, Name: principal.id, TokenHash: token.HashToken(full), TokenPrefix: prefix}))
			*principal.target = full
		}
	}
	s.probe = &offlineSyncRepositoryProbe{ActressRepository: database.NewActressRepository(db), applied: make(chan offlineSyncApplyResult, 16), problem: make(chan offlineSyncProblemResult, 16)}
	if configure != nil {
		configure(s.probe)
	}
	repos.ActressRepo = s.probe
	cfg := config.DefaultConfig(nil, nil)
	cfg.Logging.Level = "error"
	cfg.Metadata.ActressDatabase.Enabled = true
	cfg.Metadata.ActressDatabase.OfflineNameSync = config.OfflineNameSyncConfig{Enabled: true, ArtifactDirectory: f.artifactDirectory, MaxAgeHours: 24, MaxQueuedJobs: 4, MaxQueuedTasks: 16}
	deps := &core.APIDeps{
		CoreDeps:   &commandutil.CoreDeps{DB: db, ScraperRegistry: scraperutil.NewScraperRegistry()},
		ConfigFile: filepath.Join(f.root, "config.yaml"), Repos: repos, Auth: manager,
		JobStore: worker.NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, t.TempDir(), nil, nil),
	}
	s.rt = core.NewAPIRuntime(deps)
	s.rt.SetConfig(cfg)
	s.router = NewServer(s.rt)
	require.NotNil(t, s.rt.OfflineNameSync())
	awaitOfflineSync(t, s.rt.OfflineNameSync().Ready(), "initial pinned-artifact read")
	return s
}

func (s *offlineSyncServer) close(t *testing.T) {
	t.Helper()
	s.closeOnce.Do(func() {
		if s.rt != nil {
			testkit.CleanupServerHub(t, s.rt)
		}
		require.NoError(t, s.db.Close())
	})
}

func awaitOfflineSync[T any](t *testing.T, ch <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

func (s *offlineSyncServer) request(t *testing.T, method, path, bearer, body string, change ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://sync.test"+offlineSyncAPI+path, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://sync.test")
		req.Header.Set("X-Requested-With", "javinizer-offline-name-sync")
	}
	for _, apply := range change {
		apply(req)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, req)
	return response
}

func decodeOfflineSync[T any](t *testing.T, response *httptest.ResponseRecorder, status int) T {
	t.Helper()
	require.Equal(t, status, response.Code, response.Body.String())
	var result T
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result), response.Body.String())
	return result
}

func (s *offlineSyncServer) actor(t *testing.T, id int, name string) models.Actress {
	t.Helper()
	actor := models.Actress{DMMID: id, FirstName: fmt.Sprintf("Managed%d", id), LastName: "Actor", JapaneseName: name, Aliases: "Keep local metadata", ThumbURL: "https://example.invalid/keep.jpg"}
	require.NoError(t, s.probe.ImportUpsert(t.Context(), &actor))
	stored, err := s.probe.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	return *stored
}

func (s *offlineSyncServer) review(t *testing.T, bearer string, actorID uint, sourceKey string) actressnamesync.Review {
	t.Helper()
	body := fmt.Sprintf(`{"choices":[{"actress_id":%d,"source_key":%q}]}`, actorID, sourceKey)
	return decodeOfflineSync[actressnamesync.Review](t, s.request(t, http.MethodPost, "/reviews", bearer, body), http.StatusCreated)
}

func offlineSyncConfirmBody(reviewID string) string {
	return fmt.Sprintf(`{"review_id":%q,"confirmed":true}`, reviewID)
}

func assertOfflineSyncRows(t *testing.T, db *database.DB, jobs, tasks, bindings int64) {
	t.Helper()
	for table, expected := range map[string]int64{"actress_sync_jobs": jobs, "actress_sync_tasks": tasks, "actress_sync_bindings": bindings} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		require.Equal(t, expected, count, table)
	}
}

func TestOfflineNameSyncAPIAuthenticatedConfirmAndRestart(t *testing.T) {
	f := newOfflineSyncFixture(t)
	s := f.open(t, nil)
	actor := s.actor(t, 7, "旧名")
	other := s.actor(t, 8, "別の旧名")
	movie := models.Movie{ContentID: "sync-api-film", ID: "sync-api-film", Title: "Fixture", RenderGeneration: 9}
	require.NoError(t, s.db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actor.ID, CreditedName: "Original reported name", CreditedJapaneseName: "Original reported Japanese", Source: "dmm", Origin: string(models.CreditOriginScrape), UserOverride: true, OverrideName: "Manual display", Suppressed: true}
	require.NoError(t, s.db.Create(&credit).Error)
	require.NoError(t, s.db.Create(&models.ActressTranslation{ActressID: actor.ID, Language: "en", DisplayName: "Stale translation"}).Error)

	status := decodeOfflineSync[actressnamesync.SourceInfo](t, s.request(t, http.MethodGet, "/status", f.ownerToken, ""), http.StatusOK)
	require.True(t, status.Enabled)
	require.True(t, status.Available)
	require.Equal(t, f.digest, status.ArtifactDigest)
	require.True(t, status.RestartRequiredForConfigChanges)
	session, err := s.auth.Login("offline-sync-owner", "disposable-test-password", false)
	require.NoError(t, err)
	decodeOfflineSync[actressnamesync.SourceInfo](t, s.request(t, http.MethodGet, "/status", "", "", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "javinizer_session", Value: session})
	}), http.StatusOK)
	require.Equal(t, http.StatusUnauthorized, s.request(t, http.MethodGet, "/status?session="+session, "", "").Code, "a real session may not confer URL-only authority on this API")
	sources := decodeOfflineSync[actressnamesync.SourcePage](t, s.request(t, http.MethodGet, "/sources?q=r18-only-900&limit=1", f.ownerToken, ""), http.StatusOK)
	require.Len(t, sources.Items, 1)
	require.Equal(t, f.sourceKeys[0], sources.Items[0].SourceKey)
	require.Equal(t, "新しい名前", sources.Items[0].JapaneseName)

	// Selecting a source, even when its local name resembles an actor, grants
	// no implicit identity binding. The request explicitly selects both IDs.
	review := s.review(t, f.ownerToken, actor.ID, sources.Items[0].SourceKey)
	abandoned := s.review(t, f.ownerToken, other.ID, f.sourceKeys[1])
	require.Len(t, review.Proposals, 1)
	require.Equal(t, actor.ID, review.Proposals[0].ActressID)
	require.Equal(t, "旧名", review.Proposals[0].BeforeName)
	require.Equal(t, "新しい名前", review.Proposals[0].AfterName)
	assertOfflineSyncRows(t, s.db, 0, 0, 0)
	before, err := s.probe.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, actor, *before)

	for _, path := range []string{"/status", "/sources", "/jobs"} {
		require.Equal(t, http.StatusUnauthorized, s.request(t, http.MethodGet, path, "", "").Code)
	}
	confirm := offlineSyncConfirmBody(review.ReviewID)
	require.Equal(t, http.StatusUnauthorized, s.request(t, http.MethodPost, "/jobs", "", confirm).Code)
	require.Equal(t, http.StatusForbidden, s.request(t, http.MethodPost, "/jobs", f.ownerToken, confirm, func(r *http.Request) { r.Header.Set("Origin", "https://attacker.invalid") }).Code)
	require.Equal(t, http.StatusForbidden, s.request(t, http.MethodPost, "/jobs", f.ownerToken, confirm, func(r *http.Request) { r.Header.Del("X-Requested-With") }).Code)
	require.Equal(t, http.StatusForbidden, s.request(t, http.MethodPost, "/jobs", f.otherToken, confirm).Code, "another genuinely authenticated token is not the review's principal")
	for _, body := range []string{
		fmt.Sprintf(`{"review_id":%q,"confirmed":false}`, review.ReviewID),
		fmt.Sprintf(`{"review_id":%q,"confirmed":true,"after_name":"Injected"}`, review.ReviewID),
		fmt.Sprintf(`{"review_id":%q,"confirmed":true,"artifact_directory":"/tmp/client-selected"}`, review.ReviewID),
		fmt.Sprintf(`{"review_id":%q,"confirmed":true,"principal":"token:offline-sync-owner"}`, review.ReviewID),
		fmt.Sprintf(`{"review_id":%q,"confirmed":false,"confirmed":true}`, review.ReviewID),
	} {
		require.Equal(t, http.StatusBadRequest, s.request(t, http.MethodPost, "/jobs", f.ownerToken, body).Code, body)
	}
	assertOfflineSyncRows(t, s.db, 0, 0, 0)
	unchanged, err := s.probe.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, before, unchanged)

	job := decodeOfflineSync[actressnamesync.JobView](t, s.request(t, http.MethodPost, "/jobs", f.ownerToken, confirm), http.StatusAccepted)
	applied := awaitOfflineSync(t, s.probe.applied, "committed worker result")
	require.NoError(t, applied.err)
	require.NotNil(t, applied.receipt)
	require.Equal(t, "applied", applied.receipt.Status)
	assertOfflineSyncRows(t, s.db, 1, 1, 1)
	after, err := s.probe.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, "新しい名前", after.JapaneseName)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, before.DMMID, after.DMMID)
	require.Equal(t, before.FirstName, after.FirstName)
	require.Equal(t, before.LastName, after.LastName)
	require.Equal(t, before.ThumbURL, after.ThumbURL)
	require.Equal(t, before.Aliases, after.Aliases)
	require.Equal(t, before.Verified, after.Verified)
	require.Equal(t, before.Origin, after.Origin)
	require.Equal(t, before.NameKey, after.NameKey)
	require.Equal(t, before.AmbiguityQuarantined, after.AmbiguityQuarantined)
	var afterCredit models.MovieCredit
	require.NoError(t, s.db.First(&afterCredit, credit.ID).Error)
	require.Equal(t, credit, afterCredit, "historical reported attribution and manual display must survive")
	var translations, importedAliases int64
	require.NoError(t, s.db.Model(&models.ActressTranslation{}).Where("actress_id = ?", actor.ID).Count(&translations).Error)
	require.Zero(t, translations)
	require.NoError(t, s.db.Model(&models.ActressAlias{}).Where("alias_name = ?", "Untrusted source alias").Count(&importedAliases).Error)
	require.Zero(t, importedAliases, "offline source aliases are evidence, never resolver authority")

	jobPath := "/jobs/" + job.ID
	finished := decodeOfflineSync[actressnamesync.JobView](t, s.request(t, http.MethodGet, jobPath, f.ownerToken, ""), http.StatusOK)
	require.Equal(t, "completed", finished.Status)
	require.Equal(t, 1, finished.Counts["applied"])
	taskResponse := s.request(t, http.MethodGet, jobPath+"/tasks?limit=1", f.ownerToken, "")
	tasks := decodeOfflineSync[actressnamesync.TaskPage](t, taskResponse, http.StatusOK)
	require.Len(t, tasks.Items, 1)
	require.Equal(t, actor.ID, tasks.Items[0].ActressID)
	require.Equal(t, "applied", tasks.Items[0].Status)
	require.Equal(t, "旧名", tasks.Items[0].BeforeName)
	require.Equal(t, "新しい名前", tasks.Items[0].AfterName)
	require.NotNil(t, tasks.Items[0].FinishedAt)
	for _, private := range []string{"actor_token", "lease_owner", "lease_generation", "request_key", f.artifactDirectory} {
		require.NotContains(t, taskResponse.Body.String(), private)
	}
	history := decodeOfflineSync[actressnamesync.JobPage](t, s.request(t, http.MethodGet, "/jobs?limit=2", f.ownerToken, ""), http.StatusOK)
	require.Len(t, history.Items, 1)
	require.Equal(t, job.ID, history.Items[0].ID)
	storedToken, err := s.db.Repositories().ApiTokenRepo.FindByID(t.Context(), "offline-sync-owner")
	require.NoError(t, err)
	require.NotNil(t, storedToken.LastUsedAt, "the real token repository authenticated these requests")

	s.close(t)
	reopened := f.open(t, nil)
	replay := decodeOfflineSync[actressnamesync.JobView](t, reopened.request(t, http.MethodPost, "/jobs", f.ownerToken, confirm), http.StatusAccepted)
	require.Equal(t, job.ID, replay.ID)
	require.Equal(t, "completed", replay.Status)
	assertOfflineSyncRows(t, reopened.db, 1, 1, 1)
	replayedTasks := decodeOfflineSync[actressnamesync.TaskPage](t, reopened.request(t, http.MethodGet, jobPath+"/tasks", f.ownerToken, ""), http.StatusOK)
	require.Equal(t, tasks.Items, replayedTasks.Items)
	require.Equal(t, http.StatusGone, reopened.request(t, http.MethodPost, "/jobs", f.ownerToken, offlineSyncConfirmBody(abandoned.ReviewID)).Code, "unconfirmed server-held authority must not survive restart")
	require.Equal(t, http.StatusGone, reopened.request(t, http.MethodPost, "/jobs", f.otherToken, confirm).Code, "a different persisted principal cannot replay another token's nonce")
	assertOfflineSyncRows(t, reopened.db, 1, 1, 1)
	stillOther, err := reopened.probe.FindByID(t.Context(), other.ID)
	require.NoError(t, err)
	require.Equal(t, other, *stillOther)
}

func TestOfflineNameSyncAPICancelFencesRealWorker(t *testing.T) {
	f := newOfflineSyncFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	s := f.open(t, func(p *offlineSyncRepositoryProbe) {
		var once sync.Once
		p.beforeApply = func(ctx context.Context) {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	actor := s.actor(t, 21, "取消前の名前")
	review := s.review(t, f.ownerToken, actor.ID, f.sourceKeys[0])
	job := decodeOfflineSync[actressnamesync.JobView](t, s.request(t, http.MethodPost, "/jobs", f.ownerToken, offlineSyncConfirmBody(review.ReviewID)), http.StatusAccepted)
	awaitOfflineSync(t, entered, "worker's real committed lease")
	path := "/jobs/" + job.ID
	running := decodeOfflineSync[actressnamesync.JobView](t, s.request(t, http.MethodGet, path, f.ownerToken, ""), http.StatusOK)
	require.Equal(t, "running", running.Status)
	require.Equal(t, http.StatusBadRequest, s.request(t, http.MethodPost, path+"/cancel", f.ownerToken, `{"confirmed":false}`).Code)
	stillRunning := decodeOfflineSync[actressnamesync.JobView](t, s.request(t, http.MethodGet, path, f.ownerToken, ""), http.StatusOK)
	require.False(t, stillRunning.Cancelled)
	cancelled := decodeOfflineSync[actressnamesync.JobView](t, s.request(t, http.MethodPost, path+"/cancel", f.ownerToken, `{"confirmed":true}`), http.StatusOK)
	require.True(t, cancelled.Cancelled)
	require.Equal(t, "cancelled", cancelled.Status)
	releaseOnce.Do(func() { close(release) })
	result := awaitOfflineSync(t, s.probe.applied, "fenced apply attempt")
	require.ErrorIs(t, result.err, database.ErrActressSyncLease)
	unchanged, err := s.probe.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, actor, *unchanged)
	tasks := decodeOfflineSync[actressnamesync.TaskPage](t, s.request(t, http.MethodGet, path+"/tasks", f.ownerToken, ""), http.StatusOK)
	require.Len(t, tasks.Items, 1)
	require.Equal(t, "cancelled", tasks.Items[0].Status)
	require.Equal(t, "job_cancelled", tasks.Items[0].Reason)
}

func TestOfflineNameSyncAPIShutdownJoinsAndMissingSnapshotStaysCancelable(t *testing.T) {
	f := newOfflineSyncFixture(t)
	entered, cancelled, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	s := f.open(t, func(p *offlineSyncRepositoryProbe) {
		var once sync.Once
		p.beforeDue = func(ctx context.Context) {
			once.Do(func() {
				close(entered)
				<-ctx.Done()
				close(cancelled)
				<-release
			})
		}
		p.afterDue = func() { close(returned) }
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	awaitOfflineSync(t, entered, "worker's initial durable queue read")
	actor := s.actor(t, 31, "失われた証拠の旧名")
	review := s.review(t, f.ownerToken, actor.ID, f.sourceKeys[0])
	job := decodeOfflineSync[actressnamesync.JobView](t, s.request(t, http.MethodPost, "/jobs", f.ownerToken, offlineSyncConfirmBody(review.ReviewID)), http.StatusAccepted)
	shutdownDone := make(chan struct{})
	go func() { s.rt.Shutdown(); close(shutdownDone) }()
	awaitOfflineSync(t, cancelled, "server-lifetime cancellation reaching the worker")
	select {
	case <-shutdownDone:
		t.Fatal("Shutdown returned while the worker still held its repository call")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	awaitOfflineSync(t, shutdownDone, "Shutdown joining the worker")
	select {
	case <-returned:
	default:
		t.Fatal("Shutdown returned before the worker finished its real repository call")
	}
	s.close(t)
	// The job was explicitly confirmed and persisted, but no task was claimed.
	// Remove only its immutable fixture artifact, then reopen the same database.
	require.NoError(t, os.Remove(filepath.Join(f.artifactDirectory, f.digest+".ndjson")))
	reopened := f.open(t, nil)
	problem := awaitOfflineSync(t, reopened.probe.problem, "durable missing-snapshot diagnostic")
	require.NoError(t, problem.err)
	require.Equal(t, database.ActressSyncProblemSnapshotMissing, problem.code)
	statusResponse := reopened.request(t, http.MethodGet, "/status", f.ownerToken, "")
	status := decodeOfflineSync[actressnamesync.SourceInfo](t, statusResponse, http.StatusOK)
	require.True(t, status.Enabled)
	require.False(t, status.Available)
	require.Equal(t, "snapshot_unavailable", status.Code)
	require.NotContains(t, statusResponse.Body.String(), f.artifactDirectory)
	require.Equal(t, http.StatusServiceUnavailable, reopened.request(t, http.MethodGet, "/sources", f.ownerToken, "").Code)
	path := "/jobs/" + job.ID
	blocked := decodeOfflineSync[actressnamesync.JobView](t, reopened.request(t, http.MethodGet, path, f.ownerToken, ""), http.StatusOK)
	require.Equal(t, "blocked", blocked.Status)
	tasks := decodeOfflineSync[actressnamesync.TaskPage](t, reopened.request(t, http.MethodGet, path+"/tasks", f.ownerToken, ""), http.StatusOK)
	require.Len(t, tasks.Items, 1)
	require.Equal(t, "pending", tasks.Items[0].Status)
	require.Equal(t, database.ActressSyncProblemSnapshotMissing, tasks.Items[0].ProblemCode)
	require.GreaterOrEqual(t, tasks.Items[0].ProblemAttempts, 1)
	require.Greater(t, tasks.Items[0].RetryAfterMS, int64(0))
	cancelledJob := decodeOfflineSync[actressnamesync.JobView](t, reopened.request(t, http.MethodPost, path+"/cancel", f.ownerToken, `{"confirmed":true}`), http.StatusOK)
	require.True(t, cancelledJob.Cancelled)
	require.Equal(t, "cancelled", cancelledJob.Status)
	unchanged, err := reopened.probe.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, actor, *unchanged)
	assertOfflineSyncRows(t, reopened.db, 1, 1, 1)
}

func TestOfflineNameSyncExplicitReloadKeepsUnconfirmedConsentForSameDigest(t *testing.T) {
	f := newOfflineSyncFixture(t)
	s := f.open(t, nil)
	actor := s.actor(t, 8123, "旧名")
	review := s.review(t, f.ownerToken, actor.ID, f.sourceKeys[0])
	// Saving unrelated runtime configuration must not activate new source or
	// consent policy, even when the operator explicitly reloads artifact bytes.
	cfg := s.rt.Snapshot().Config().Clone()
	cfg.Metadata.ActressDatabase.OfflineNameSync = config.OfflineNameSyncConfig{
		Enabled: false, ArtifactDirectory: filepath.Join(t.TempDir(), "different"),
		MaxAgeHours: 240, MaxQueuedJobs: 1, MaxQueuedTasks: 1,
	}
	s.rt.ReplaceReloadable(cfg, s.rt.Snapshot().Registry())
	info := decodeOfflineSync[actressnamesync.SourceInfo](t, s.request(t, http.MethodPost, "/sources/reload", f.ownerToken, `{}`), http.StatusOK)
	require.Equal(t, f.digest, info.ArtifactDigest)
	require.True(t, info.Enabled)
	require.Equal(t, int64(24*60*60), info.MaxAgeSeconds)
	require.True(t, info.RestartRequiredForConfigChanges)
	require.True(t, info.Available)
	assertOfflineSyncRows(t, s.db, 0, 0, 0)
	stored, err := s.probe.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, actor, *stored)
	page := decodeOfflineSync[actressnamesync.SourcePage](t, s.request(t, http.MethodGet, "/sources?artifact_digest="+f.digest, f.ownerToken, ""), http.StatusOK)
	require.Equal(t, f.digest, page.ArtifactDigest)
	// The unchanged generation retains explicit review authority; reload itself
	// neither creates a receipt nor applies metadata.
	job := decodeOfflineSync[actressnamesync.JobView](t, s.request(t, http.MethodPost, "/jobs", f.ownerToken, offlineSyncConfirmBody(review.ReviewID)), http.StatusAccepted)
	require.Equal(t, f.digest, job.SnapshotDigest)
}
