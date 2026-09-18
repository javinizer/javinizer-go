package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/applyplan"
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
	"go.uber.org/goleak"
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
func createJobWithWF(deps *core.APIDeps, cfg *config.Config, files []string, plans ...*applyplan.Plan) *worker.BatchJob {
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.ScraperRegistry, deps.CoreDeps.DB.Repositories())
	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		panic(fmt.Sprintf("createJobWithWF: failed to create workflow factory: %v", err))
	}
	wf, err := factory.NewWorkflow("")
	if err != nil {
		panic(fmt.Sprintf("createJobWithWF: failed to create workflow: %v", err))
	}

	var plan *applyplan.Plan
	if len(plans) > 0 {
		plan = plans[0]
	}
	return deps.JobStore.CreateJobBatch(files, &worker.JobConfig{
		ApplyPlan: plan,
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

type websocketDialFunc func(string, http.Header) (*websocket.Conn, *http.Response, error)
type websocketUpgradeFunc func(http.ResponseWriter, *http.Request, http.Header) (*websocket.Conn, error)

type testWSConnOptions struct {
	dial            websocketDialFunc
	upgrade         websocketUpgradeFunc
	resultGate      <-chan struct{}
	acceptTimeout   time.Duration
	shutdownStarted chan<- struct{}
	handlerDone     chan<- struct{}
}

type testWSConnResult struct {
	conn *websocket.Conn
	err  error
}

type testWSConnections struct {
	serverConn *websocket.Conn
	clientConn *websocket.Conn
	server     *httptest.Server

	stop            chan struct{}
	result          <-chan testWSConnResult
	shutdownStarted chan<- struct{}
	closeOnce       sync.Once

	handlerMu       sync.Mutex
	handlerStopping bool
	handlerWG       sync.WaitGroup
	handlerErr      error
}

func (c *testWSConnections) beginHandler() bool {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	if c.handlerStopping {
		return false
	}
	c.handlerWG.Add(1)
	return true
}

func (c *testWSConnections) publishHandlerResult(result chan<- testWSConnResult, value testWSConnResult) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	if c.handlerStopping {
		if value.conn != nil {
			_ = value.conn.Close()
		}
		return
	}
	select {
	case result <- value:
	default:
		if value.conn != nil {
			_ = value.conn.Close()
		}
	}
}

// Close first prevents new handler ownership, then closes known resources and
// the listener before joining every handler. Only after the join can it safely
// drain a result that raced with timeout; no handler can publish afterward.
func (c *testWSConnections) Close() {
	c.closeOnce.Do(func() {
		c.handlerMu.Lock()
		close(c.stop)
		c.handlerStopping = true
		c.handlerMu.Unlock()
		if c.shutdownStarted != nil {
			c.shutdownStarted <- struct{}{}
		}

		if c.clientConn != nil {
			_ = c.clientConn.Close()
		}
		if c.serverConn != nil {
			_ = c.serverConn.Close()
		}
		c.server.Close()
		c.handlerWG.Wait()

		select {
		case result := <-c.result:
			if result.conn != nil && result.conn != c.serverConn {
				_ = result.conn.Close()
			}
			c.handlerErr = result.err
		default:
		}
	})
}

// newTestWSConn establishes a real local WebSocket pair. Setup errors are
// returned to the test goroutine; the HTTP handler only publishes its result
// through a bounded channel. Failed setup closes every acquired resource before
// returning, while successful callers own the returned idempotent Close method.
func newTestWSConn() (*testWSConnections, error) {
	return newTestWSConnWithOptions(testWSConnOptions{})
}

func newTestWSConnWithOptions(options testWSConnOptions) (*testWSConnections, error) {
	if options.acceptTimeout == 0 {
		options.acceptTimeout = 2 * time.Second
	}
	if options.dial == nil {
		options.dial = websocket.DefaultDialer.Dial
	}
	if options.upgrade == nil {
		upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}
		options.upgrade = upgrader.Upgrade
	}

	resultCh := make(chan testWSConnResult, 1)
	connections := &testWSConnections{
		stop:            make(chan struct{}),
		result:          resultCh,
		shutdownStarted: options.shutdownStarted,
	}
	connections.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !connections.beginHandler() {
			return
		}
		defer connections.handlerWG.Done()
		if options.handlerDone != nil {
			defer func() { options.handlerDone <- struct{}{} }()
		}

		conn, err := options.upgrade(w, r, nil)
		if err == nil && options.resultGate != nil {
			<-options.resultGate
		}
		connections.publishHandlerResult(resultCh, testWSConnResult{conn: conn, err: err})
	}))

	wsURL := "ws" + strings.TrimPrefix(connections.server.URL, "http")
	clientConn, _, err := options.dial(wsURL, nil)
	connections.clientConn = clientConn
	if err != nil {
		connections.Close()
		if connections.handlerErr != nil {
			return nil, fmt.Errorf("upgrade websocket: %w", connections.handlerErr)
		}
		return nil, fmt.Errorf("dial websocket: %w", err)
	}

	timer := time.NewTimer(options.acceptTimeout)
	defer timer.Stop()
	select {
	case result := <-resultCh:
		if result.err != nil {
			connections.Close()
			return nil, fmt.Errorf("upgrade websocket: %w", result.err)
		}
		connections.serverConn = result.conn
		return connections, nil
	case <-timer.C:
		connections.Close()
		return nil, fmt.Errorf("timeout waiting for server-side websocket connection after %v", options.acceptTimeout)
	}
}

type websocketMessageReader interface {
	ReadMessage() (messageType int, data []byte, err error)
}

// readUntilMessage performs one Gorilla-compatible read loop. Successful
// unrelated frames may be skipped, but a read error is terminal: Gorilla
// WebSocket connections must never be read again after ReadMessage reports an
// error. The caller must set a read deadline before entering the loop.
func readUntilMessage(reader websocketMessageReader, marker string) ([]byte, error) {
	for {
		_, data, err := reader.ReadMessage()
		if err != nil {
			return nil, err
		}
		var message ws.ProgressMessage
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, fmt.Errorf("decode websocket progress message: %w", err)
		}
		if message.JobID == "" {
			return nil, fmt.Errorf("decode websocket progress message: missing job_id")
		}
		if message.JobID == marker {
			return data, nil
		}
	}
}

func readUntilProbe(reader websocketMessageReader, probeJobID string) error {
	_, err := readUntilMessage(reader, probeJobID)
	return err
}

// registerClientOnHub registers a real ws.Client, starts its WritePump, and
// waits for an observed probe delivered through the running hub. Probe sends
// repeat because Hub.Run may select broadcast before registration when both
// channels are ready. Reads do not repeat after an error: one overall deadline
// bounds a single read loop, while a separate sender retries the harmless
// broadcast side until registration is acknowledged.
//
// The returned channel closes when WritePump exits. Callers must stop the hub,
// wait for this channel, then close the connection pair and HTTP server.
func registerClientOnHub(hub *ws.Hub, serverConn *websocket.Conn, clientConn *websocket.Conn) (<-chan struct{}, error) {
	const (
		probeJobID      = "__probe_ready__"
		probeInterval   = 20 * time.Millisecond
		overallDeadline = 2 * time.Second
	)

	client := ws.NewClient(serverConn)
	hub.Register(client)
	writePumpDone := make(chan struct{})
	go func() {
		defer close(writePumpDone)
		client.WritePump()
	}()

	if err := clientConn.SetReadDeadline(time.Now().Add(overallDeadline)); err != nil {
		return writePumpDone, fmt.Errorf("set probe read deadline: %w", err)
	}

	probe := &ws.ProgressMessage{JobID: probeJobID, Status: ws.ProgressStatusPending}
	stopProbe := make(chan struct{})
	probeSenderDone := make(chan struct{})
	probeErr := make(chan error, 1)
	go func() {
		defer close(probeSenderDone)
		ticker := time.NewTicker(probeInterval)
		defer ticker.Stop()
		for {
			if err := hub.BroadcastProgress(probe); err != nil {
				probeErr <- err
				return
			}
			select {
			case <-stopProbe:
				return
			case <-ticker.C:
			}
		}
	}()

	readErr := readUntilProbe(clientConn, probeJobID)
	close(stopProbe)
	<-probeSenderDone
	select {
	case err := <-probeErr:
		return writePumpDone, fmt.Errorf("probe broadcast failed: %w", err)
	default:
	}
	if readErr != nil {
		return writePumpDone, fmt.Errorf("hub registration probe failed: %w", readErr)
	}
	return writePumpDone, nil
}

type queuedMessageReader struct {
	messages [][]byte
	reads    int
}

func (r *queuedMessageReader) ReadMessage() (int, []byte, error) {
	if r.reads >= len(r.messages) {
		return 0, nil, fmt.Errorf("message queue exhausted")
	}
	message := r.messages[r.reads]
	r.reads++
	return websocket.TextMessage, message, nil
}

func TestReadUntilMessageMatchesExactJobID(t *testing.T) {
	reader := &queuedMessageReader{messages: [][]byte{
		[]byte(`{"job_id":"other-job","file_path":"/tmp/stub-job.mp4","message":"processing stub-job"}`),
		[]byte(`{"job_id":"stub-job-suffix","message":"not the requested job"}`),
		[]byte(`{"job_id":"stub-job","message":"expected"}`),
	}}

	data, err := readUntilMessage(reader, "stub-job")
	if err != nil {
		t.Fatalf("readUntilMessage() error = %v", err)
	}
	if string(data) != string(reader.messages[2]) {
		t.Fatalf("readUntilMessage() = %s, want exact JobID frame %s", data, reader.messages[2])
	}
	if reader.reads != 3 {
		t.Fatalf("ReadMessage called %d times, want 3", reader.reads)
	}
}

func TestReadUntilMessageRejectsMalformedFrame(t *testing.T) {
	tests := map[string][]byte{
		"invalid JSON":  []byte(`{"job_id":"stub-job"`),
		"missing JobID": []byte(`{"message":"stub-job"}`),
	}
	for name, frame := range tests {
		t.Run(name, func(t *testing.T) {
			reader := &queuedMessageReader{messages: [][]byte{
				frame,
				[]byte(`{"job_id":"stub-job"}`),
			}}

			_, err := readUntilMessage(reader, "stub-job")
			if err == nil || !strings.Contains(err.Error(), "decode websocket progress message") {
				t.Fatalf("readUntilMessage() error = %v, want malformed-frame decode error", err)
			}
			if reader.reads != 1 {
				t.Fatalf("ReadMessage called %d times after malformed frame, want 1", reader.reads)
			}
		})
	}
}

func TestReadUntilProbeRequiresExactJobID(t *testing.T) {
	reader := &queuedMessageReader{messages: [][]byte{
		[]byte(`{"job_id":"other-job","message":"__probe_ready__"}`),
	}}

	err := readUntilProbe(reader, "__probe_ready__")
	if err == nil || !strings.Contains(err.Error(), "message queue exhausted") {
		t.Fatalf("readUntilProbe() error = %v, want terminal queue error after unrelated frame", err)
	}
	if reader.reads != 1 {
		t.Fatalf("ReadMessage called %d times before terminal queue error, want 1", reader.reads)
	}
}

func TestNewTestWSConnDialFailureCleansUp(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	connections, err := newTestWSConnWithOptions(testWSConnOptions{
		dial: func(string, http.Header) (*websocket.Conn, *http.Response, error) {
			return nil, nil, fmt.Errorf("injected dial failure")
		},
	})
	if connections != nil {
		t.Fatalf("newTestWSConnWithOptions() connections = %v, want nil", connections)
	}
	if err == nil || !strings.Contains(err.Error(), "dial websocket: injected dial failure") {
		t.Fatalf("newTestWSConnWithOptions() error = %v, want dial failure", err)
	}
}

func TestNewTestWSConnUpgradeFailureCleansUp(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	connections, err := newTestWSConnWithOptions(testWSConnOptions{
		upgrade: func(http.ResponseWriter, *http.Request, http.Header) (*websocket.Conn, error) {
			return nil, fmt.Errorf("injected upgrade failure")
		},
	})
	if connections != nil {
		t.Fatalf("newTestWSConnWithOptions() connections = %v, want nil", connections)
	}
	if err == nil || !strings.Contains(err.Error(), "upgrade websocket: injected upgrade failure") {
		t.Fatalf("newTestWSConnWithOptions() error = %v, want upgrade failure", err)
	}
}

func TestNewTestWSConnAcceptTimeoutJoinsLateHandoff(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	resultGate := make(chan struct{})
	upgraded := make(chan *websocket.Conn, 1)
	shutdownStarted := make(chan struct{}, 1)
	handlerDone := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

	type constructorResult struct {
		connections *testWSConnections
		err         error
	}
	constructorDone := make(chan constructorResult, 1)
	go func() {
		connections, err := newTestWSConnWithOptions(testWSConnOptions{
			upgrade: func(w http.ResponseWriter, r *http.Request, h http.Header) (*websocket.Conn, error) {
				conn, err := upgrader.Upgrade(w, r, h)
				if err == nil {
					upgraded <- conn
				}
				return conn, err
			},
			resultGate:      resultGate,
			acceptTimeout:   10 * time.Millisecond,
			shutdownStarted: shutdownStarted,
			handlerDone:     handlerDone,
		})
		constructorDone <- constructorResult{connections: connections, err: err}
	}()

	serverConn := <-upgraded
	<-shutdownStarted
	select {
	case result := <-constructorDone:
		t.Fatalf("constructor returned before gated handler exited: connections=%v err=%v", result.connections, result.err)
	case <-time.After(50 * time.Millisecond):
	}

	close(resultGate)
	result := <-constructorDone
	if result.connections != nil {
		t.Fatalf("newTestWSConnWithOptions() connections = %v, want nil", result.connections)
	}
	if result.err == nil || !strings.Contains(result.err.Error(), "timeout waiting for server-side websocket connection") {
		t.Fatalf("newTestWSConnWithOptions() error = %v, want accept timeout", result.err)
	}
	select {
	case <-handlerDone:
	default:
		t.Fatal("Close returned before the upgraded handler completed")
	}
	if err := serverConn.WriteMessage(websocket.TextMessage, []byte("late")); err == nil {
		t.Fatal("late server connection remained open after Close returned")
	}
}

type terminalErrorMessageReader struct {
	reads int
}

func (r *terminalErrorMessageReader) ReadMessage() (int, []byte, error) {
	r.reads++
	if r.reads > 1 {
		panic("ReadMessage called after terminal error")
	}
	return 0, nil, fmt.Errorf("injected terminal read failure")
}

func TestReadUntilProbeStopsAfterReadFailure(t *testing.T) {
	reader := &terminalErrorMessageReader{}
	err := readUntilProbe(reader, "probe")
	if err == nil || !strings.Contains(err.Error(), "injected terminal read failure") {
		t.Fatalf("readUntilProbe() error = %v, want injected terminal read failure", err)
	}
	if reader.reads != 1 {
		t.Fatalf("ReadMessage called %d times after terminal failure, want 1", reader.reads)
	}
}
