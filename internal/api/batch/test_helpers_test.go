package batch

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

type updateOptions struct {
	ForceOverwrite bool
	PreserveNFO    bool
	Preset         string
	ScalarStrategy nfo.MergeStrategy
	ArrayStrategy  bool // true=merge, false=replace
	SkipNFO        bool
	SkipDownload   bool
}

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.APIDeps {
	deps := testkit.CreateTestDeps(t, cfg, configFile)
	return deps
}

func lifecycleDepsFromCore(d *core.APIDeps) *core.APIDeps {
	return d
}

func organizeDepsFromCore(d *core.APIDeps) *core.APIDeps {
	return d
}

func movieEditDepsFromCore(d *core.APIDeps) *core.APIRuntime {
	return testkit.GetTestRuntime(d)
}

func rescrapeDepsFromCore(d *core.APIDeps) *core.APIDeps {
	return d
}

// initTestWebSocket is a compatibility stub — CreateTestDeps now initializes the
// WebSocket directly on deps.Runtime. Tests that need a standalone hub should call
// testkit.InitTestWebSocket or testkit.StartStandaloneHub directly.
func initTestWebSocket(t *testing.T) {
	// No-op: WebSocket is initialized in CreateTestDeps
}

func testStartScrape(ctx context.Context, job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *scraperutil.ScraperRegistry, selectedScrapers []string, strict bool, force bool) error {
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, registry, db.Repositories())
	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		return err
	}
	wf, err := factory.NewWorkflow("")
	if err != nil {
		return err
	}

	scrapeOpts := worker.ScrapePhaseConfig{
		SelectedScrapers: selectedScrapers,
		Strict:           strict,
		Force:            force,
	}
	// Per DEEP-6: WF and BatchCfg set on job.deps, not on phase config overrides
	job.Controller().SetWorkflow(wf)
	job.Controller().SetBatchCfg(worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	})
	if err := job.Controller().StartScrape(ctx, job.ResultsWriter().GetFiles(), scrapeOpts); err != nil {
		return err
	}
	return job.Controller().Wait()
}

func testStartUpdateApply(ctx context.Context, job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *scraperutil.ScraperRegistry, emitter eventlog.EventEmitter, opts *updateOptions) error {
	if opts == nil {
		opts = &updateOptions{}
	}
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, registry, db.Repositories())

	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		return err
	}
	wf, err := factory.NewWorkflow(job.ID.String())
	if err != nil {
		return err
	}

	applyOpts := worker.ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{Skip: true},
		MergeOptions: workflow.MergeOptions{
			ForceOverwrite: opts.ForceOverwrite,
			PreserveNFO:    opts.PreserveNFO,
			ScalarStrategy: opts.ScalarStrategy,
			ArrayStrategy:  opts.ArrayStrategy,
		},
		GenerateNFO: !opts.SkipNFO,
		Download:    !opts.SkipDownload,
		PostApplyFunc: func(ctx context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
			if afr.Err != nil && emitter != nil {
				_ = emitter.EmitOrganizeEvent(context.Background(), "nfo_gen", fmt.Sprintf("Update failed for %s", afc.Movie.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "movie_id": afc.Movie.ID, "error": afr.Err.Error()})
			}
		},
	}
	// Per DEEP-6: WF and BatchCfg set on job.deps, not on phase config overrides
	job.Controller().SetWorkflow(wf)
	job.Controller().SetBatchCfg(worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	})
	// API-1+2: StartApply requires Completed lifecycle status (CAS fix for double-start race)
	setJobStatus(job, models.JobStatusCompleted)
	if err := job.Controller().StartApply(ctx, applyOpts); err != nil {
		setJobStatus(job, models.JobStatusFailed)
		return err
	}
	return job.Controller().Wait()
}

func testStartOrganizeApply(ctx context.Context, job *worker.BatchJob, jobStore worker.JobStoreInterface, destination string, copyOnly bool, linkModeRaw string, skipNFO bool, skipDownload bool, db *database.DB, cfg *config.Config, registry *scraperutil.ScraperRegistry, emitter eventlog.EventEmitter) error {
	var opModeOverride *operationmode.OperationMode
	if job.GetOperationModeOverride() != operationmode.OperationModeOrganize {
		m := job.GetOperationModeOverride()
		opModeOverride = &m
	}
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, registry, db.Repositories())

	fc.OperationMode = opModeOverride
	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		setJobStatus(job, models.JobStatusFailed)
		if jobStore != nil {
			jobStore.PersistJob(job)
		}
		return err
	}
	wf, err := factory.NewWorkflow(job.ID.String())
	if err != nil {
		setJobStatus(job, models.JobStatusFailed)
		if jobStore != nil {
			jobStore.PersistJob(job)
		}
		return err
	}

	linkMode, err := workflow.ResolveLinkMode(linkModeRaw)
	if err != nil {
		setJobStatus(job, models.JobStatusFailed)
		if jobStore != nil {
			jobStore.PersistJob(job)
		}
		return err
	}

	applyOpts := worker.ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{
			MoveFiles:   !copyOnly,
			LinkMode:    linkMode,
			ForceUpdate: true,
		},
		MergeOptions: workflow.MergeOptions{ForceOverwrite: true},
		Destination:  destination,
		GenerateNFO:  !skipNFO,
		Download:     !skipDownload,
	}
	// Per DEEP-6: WF and BatchCfg set on job.deps, not on phase config overrides
	job.Controller().SetWorkflow(wf)
	job.Controller().SetBatchCfg(worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	})
	// API-1+2: StartApply requires Completed lifecycle status (CAS fix for double-start race)
	setJobStatus(job, models.JobStatusCompleted)
	if err := job.Controller().StartApply(ctx, applyOpts); err != nil {
		setJobStatus(job, models.JobStatusFailed)
		if jobStore != nil {
			jobStore.PersistJob(job)
		}
		return err
	}
	return job.Controller().Wait()
}

// setJobResult sets a file result on a BatchJob for test setup.
// This replaces the deleted UpdateFileResult method — test code sets
// Results directly and adjusts counters manually, with mutex protection.
// If the result does not have a ResultID, one is auto-generated from the MovieID.
func setJobResult(job *worker.BatchJob, filePath string, result *resultstore.MovieResult) {
	if result.ResultID == "" {
		result.ResultID = result.FileMatchInfo.MovieID
	}
	job.ResultsWriter().UpdateFileResult(filePath, result)
}

// setJobStatus sets the job status for test setup.
// This replaces the deleted MarkStarted/MarkCompleted/MarkOrganized/MarkFailed/MarkCancelled methods.
// It also sets the corresponding timestamp fields, matching the behavior of the deleted Mark* methods.
func setJobStatus(job *worker.BatchJob, status models.JobStatus) {
	job.Controller().SetJobStatus(status)
}

// createJobWithWF creates a BatchJob with a Workflow attached at construction.
// This matches the production flow where lifecycle.go creates jobs with jobConfig.WF.
// Tests that hit HTTP handler endpoints (rescrape, organize, etc.) should use this
// instead of bare CreateJob, since handlers assume the job has a WF.
func createJobWithWF(deps *core.APIDeps, cfg *config.Config, files []string) *worker.BatchJob {
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.ScraperRegistry, deps.CoreDeps.DB.Repositories())
	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		panic(fmt.Sprintf("createJobWithWF: failed to create workflow factory: %v", err))
	}
	wf, err := factory.NewWorkflow("")
	if err != nil {
		panic(fmt.Sprintf("createJobWithWF: failed to create workflow: %v", err))
	}

	return deps.JobStore.CreateJobBatch(files, &worker.JobConfig{
		BatchJobDeps: worker.BatchJobDeps{
			WF: wf,
			BatchCfg: worker.BatchJobConfig{
				MaxWorkers:      cfg.Performance.MaxWorkers,
				WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
				ScraperPriority: cfg.Scrapers.Priority,
				NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
			},
		},
	})
}

// excludeFile excludes a file from the batch job for test setup.
// Per DEEP-1: BatchJob no longer has ExcludeFile — this helper provides
// equivalent behavior by calling ResultTracker and JobLifecycle directly.
func excludeFile(job *worker.BatchJob, filePath string) {
	job.ResultsWriter().MarkExcluded(filePath)

	if job.ResultsWriter().IsAllExcluded() {
		job.Lifecycle().Cancel()
	}
}

// newTestWSConn establishes a real local WebSocket connection pair (server +
// client) for end-to-end hub-broadcast tests, mirroring the websocket package's
// createTestConnections. Returns (serverConn, clientConn, server). The caller
// wraps serverConn in ws.NewClient, registers it on a running hub, starts its
// WritePump, and reads broadcasts from clientConn. Caller MUST close the conns
// and server (t.Cleanup is recommended). Used by the e2e organize-progress
// wiring test to prove the resolver's production sink delivers to a real client.
func newTestWSConn(t *testing.T) (*websocket.Conn, *websocket.Conn, *httptest.Server) {
	t.Helper()
	var upgrader = websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		serverConnCh <- conn
	}))
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket failed: %v", err)
	}
	select {
	case serverConn := <-serverConnCh:
		return serverConn, clientConn, server
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server-side websocket connection")
		return nil, nil, nil
	}
}

// registerClientOnHub registers a real ws.Client (wrapping serverConn) on the
// running hub, starts its WritePump so broadcasts reach clientConn, and waits
// deterministically (by spec, not just by runtime scheduler behavior) for
// registration to complete. Readiness is confirmed by a probe round-trip: a
// probe message is broadcast and we block until clientConn receives it. A
// broadcast is only forwarded to REGISTERED clients, so receiving the probe
// proves the hub's Run loop has processed the registration.
//
// Why re-broadcast: Hub.Run drains its register and broadcast channels via a
// select whose case choice among multiple-ready cases is not spec-guaranteed,
// so a single probe could (in principle) be processed before the registration
// and dropped, leaving the helper blocked. Re-broadcasting on a ticker until
// the probe is received makes success independent of select ordering.
//
// Why a reader goroutine instead of read-deadline retries: gorilla/websocket
// permanently caches the FIRST read error on a Conn — including a read
// deadline timeout (NextReader sets c.readErr and every subsequent read
// returns that same cached error; after 1000 such reads it panics with
// "repeated read on failed websocket connection"). A retry loop built on
// per-attempt read deadlines therefore never actually retries: if the first
// read times out, all later reads fail instantly with the cached timeout and
// the loop spin-panics. That is the coverage-run-only CI flake this replaces
// (coverage timing let the first 20ms read miss the probe; locally the probe
// always landed in time). Frames are read on one goroutine into a channel and
// the main goroutine never imposes a per-attempt read deadline.
//
// Why the trailing sentinel: after the probe matches, one more re-broadcast
// may already be in flight. The drain sentinel is broadcast LAST (FIFO: hub
// broadcast channel → client send buffer → WritePump → TCP all preserve
// order) and the reader goroutine swallows every frame up to and including
// it, guaranteeing no stale probe remains on the wire when the caller's test
// body performs its own ReadMessage. Bounded by an overall deadline so a
// genuinely broken setup fails fast rather than hanging.
func registerClientOnHub(t *testing.T, hub *ws.Hub, serverConn *websocket.Conn, clientConn *websocket.Conn) {
	t.Helper()
	const probeJobID = "__probe_ready__"
	const drainSentinel = "__probe_drain_done__" // must NOT contain probeJobID
	probe := &ws.ProgressMessage{JobID: probeJobID, Status: ws.ProgressStatusPending}
	sentinel := &ws.ProgressMessage{JobID: drainSentinel, Status: ws.ProgressStatusPending}
	client := ws.NewClient(serverConn)
	hub.Register(client)
	go client.WritePump()

	const (
		probeInterval   = 20 * time.Millisecond
		overallDeadline = 2 * time.Second
	)

	// Sole reader on clientConn. Forwards frames until it swallows the drain
	// sentinel, then exits; readerDone is closed on any exit path. Sends are
	// non-blocking so a full channel (junk frames the main goroutine momentarily
	// is not draining) never wedges the only reader.
	frames := make(chan []byte, 64)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			_, data, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if bytes.Contains(data, []byte(drainSentinel)) {
				return
			}
			select {
			case frames <- data:
			default:
			}
		}
	}()

	deadline := time.NewTimer(overallDeadline)
	defer deadline.Stop()
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()

	// Phase 1: re-broadcast the probe until the client receives it — receiving
	// a broadcast proves the hub's Run loop has processed the registration, so
	// the pipeline is ready for the real broadcast the test body will drive.
	for probeAcked := false; !probeAcked; {
		if err := hub.BroadcastProgress(probe); err != nil {
			t.Fatalf("probe broadcast failed: %v", err)
		}
		select {
		case data := <-frames:
			if bytes.Contains(data, []byte(probeJobID)) {
				probeAcked = true
			}
			// else: not the probe (e.g. a ping frame payload) — keep waiting
			// without re-broadcasting.
		case <-readerDone:
			t.Fatal("client read loop ended before the probe was received (connection failed)")
		case <-deadline.C:
			t.Fatalf("did not receive probe (hub registration not processed within %v)", overallDeadline)
		case <-ticker.C:
			// re-broadcast on the next iteration
		}
	}

	// Phase 2: no more probes will be broadcast; the sentinel is the last frame
	// enqueued for this connection. Wait for the reader to swallow it (draining
	// frames meanwhile so the reader never blocks on a full channel), leaving
	// the wire clean: the caller's next ReadMessage sees the real broadcast.
	if err := hub.BroadcastProgress(sentinel); err != nil {
		t.Fatalf("drain sentinel broadcast failed: %v", err)
	}
	for {
		select {
		case <-frames: // drain lingering probe frames
		case <-readerDone:
			return // sentinel swallowed; reader exited; wire is clean
		case <-deadline.C:
			t.Fatalf("did not receive drain sentinel within %v", overallDeadline)
		}
	}
}
