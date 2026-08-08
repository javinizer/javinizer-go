package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// ActressSyncManagerDeps ...
type ActressSyncManagerDeps struct {
	DB          *database.DB
	ActressRepo *database.ActressRepository
	MovieRepo   *database.MovieRepository
	Snapshot    func() (*config.Config, *scraperutil.ScraperRegistry)
}

// ErrActressSyncManagerUnavailable is the ONLY readiness sentinel: phase 4
// maps it to HTTP 503 via errors.Is; a 202 must be impossible while
// unavailable (R1/R2).
var ErrActressSyncManagerUnavailable = errors.New("actress sync manager is unavailable")

// actressSyncOutcomeFailed is the terminal task outcome for task failures.
const actressSyncOutcomeFailed = "failed"

// NoCandidatesError reports a create where every candidate was already
// merged away (or none existed): SkippedIDs carries the dropped selections.
// It wraps database.ErrActressSyncNoCandidates for errors.Is callers (R3).
type NoCandidatesError struct{ SkippedIDs []uint }

// Error implements error.
func (e *NoCandidatesError) Error() string {
	if len(e.SkippedIDs) == 0 {
		return "no actresses require metadata sync"
	}
	return fmt.Sprintf("no actresses require metadata sync (%d already merged away)", len(e.SkippedIDs))
}

// Is reports a match for database.ErrActressSyncNoCandidates.
func (e *NoCandidatesError) Is(target error) bool {
	return target == database.ErrActressSyncNoCandidates
}

// Unwrap exposes the ErrActressSyncNoCandidates sentinel to errors.Is/As.
func (e *NoCandidatesError) Unwrap() error { return database.ErrActressSyncNoCandidates }

// ActressSyncCreateRequest ...
type ActressSyncCreateRequest struct {
	Scope      string `json:"scope"`
	ActressIDs []uint `json:"actress_ids"`
}

// trackedSyncTask records an in-flight sync task so per-job cancellation can
// abort its context before the lease expires. cancelled marks tasks aborted
// by CancelJob, so their completion settles as cancelled regardless of what
// outcome the sync path reported.
type trackedSyncTask struct {
	jobID     string
	cancel    context.CancelFunc
	cancelled bool
	// run identifies this particular registration: a requeue can register a
	// retry under the same task ID before the original goroutine exits, and
	// only the owning registration may remove (never cancel) that entry.
	run uint64
}

// ActressSyncManager ...
type ActressSyncManager struct {
	deps              ActressSyncManagerDeps
	repo              *database.ActressSyncRepository
	owner             string
	mu                sync.Mutex
	started           bool
	retryTimer        *time.Timer
	retryGeneration   uint64
	startFailStreak   int
	claimFailStreak   int
	claimBackoffUntil time.Time
	// retryDelay is the CON-05 backoff base (1s in production; tests set it
	// short). The streak exponent grows from it up to 60s.
	retryDelay time.Duration
	// Shutdown latches permanently (CON-08): after runtime shutdown, held
	// manager references refuse Start/CreateJob. Hot-reload restarts use Stop,
	// which does NOT latch, so the config-reload restart path keeps working.
	permanentlyStopped atomic.Bool
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	wake               chan struct{}
	active             atomic.Int32
	recoveryInterval   time.Duration // test-tunable recovery ticker period
	// taskMu guards runningTasks; it is separate from mu because Stop holds
	// mu across wg.Wait while task goroutines still need to unregister.
	taskMu     sync.Mutex
	taskRunSeq uint64
	// runningTasks maps a task ID to ALL in-flight runs for that task: a
	// merge can requeue a task under its old ID while the original run is
	// still executing, and cancelling the job must abort every one of them.
	runningTasks map[string][]trackedSyncTask
}

// NewActressSyncManager ...
func NewActressSyncManager(deps ActressSyncManagerDeps) *ActressSyncManager {
	m := &ActressSyncManager{
		deps:             deps,
		owner:            uuid.NewString(),
		retryDelay:       time.Second,
		wake:             make(chan struct{}, 1),
		recoveryInterval: 15 * time.Second,
		runningTasks:     make(map[string][]trackedSyncTask),
	}
	if deps.DB != nil {
		m.repo = database.NewActressSyncRepository(deps.DB)
	}
	return m
}

// Start ...
func (m *ActressSyncManager) Start() {
	if m == nil || m.repo == nil || m.permanentlyStopped.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startLocked()
}

func (m *ActressSyncManager) startLocked() {
	if m.started {
		return
	}
	jobs, err := m.repo.ListActiveJobs()
	if err != nil {
		m.startFailStreak++
		// CON-05: exponential backoff (1s..60s), log only on powers of two.
		if m.startFailStreak <= 2 || m.startFailStreak&(m.startFailStreak-1) == 0 {
			logging.Warnf("Actress sync startup failed (attempt %d), retrying in %s: %v", m.startFailStreak, m.startRetryDelay(), err)
		}
		m.scheduleStartRetry()
		return
	}
	m.startFailStreak = 0
	if len(jobs) == 0 {
		return
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.started = true
	if err := m.repo.RecoverExpiredLeases(time.Now().UTC()); err != nil {
		logging.Warnf("Actress sync recovery failed: %v", err)
	}
	m.wg.Add(1)
	go m.runDispatch(m.ctx)
}

func (m *ActressSyncManager) scheduleStartRetry() {
	if m.retryTimer != nil {
		return
	}
	generation := m.retryGeneration
	m.retryTimer = time.AfterFunc(m.startRetryDelay(), func() {
		m.mu.Lock()
		if generation != m.retryGeneration {
			m.mu.Unlock()
			return
		}
		m.retryTimer = nil
		m.startLocked()
		m.mu.Unlock()
	})
}

// Stop cancels the dispatch context,
// then waits for in-flight tasks with a bounded grace (CON-07): past the
// grace, leases are ABANDONED — the safety net is next-boot
// RecoverExpiredLeases, not an unbounded shutdown hang.
func (m *ActressSyncManager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retryGeneration++
	if m.retryTimer != nil {
		m.retryTimer.Stop()
		m.retryTimer = nil
	}
	if !m.started {
		return
	}
	m.started = false
	m.cancel()
	// Stop must never hang shutdown on a wedged task: task timeout + grace.
	taskGrace := 70 * time.Second // default taskTimeout (60s) + margin
	if cfg, _ := m.runtimeSnapshot(); cfg != nil && cfg.Scrapers.RequestTimeoutSeconds > 0 {
		taskGrace = time.Duration(cfg.Scrapers.RequestTimeoutSeconds+10) * time.Second
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(taskGrace):
		logging.Warnf("Actress sync stop grace expired; abandoning %d lease(s) to next-boot recovery", m.active.Load())
	}
	if err := m.repo.ReleaseOwnerLeases(m.owner); err != nil {
		logging.Warnf("Actress sync lease release failed: %v", err)
	}
	m.ctx = nil
	m.cancel = nil
}

// Shutdown latches the manager permanently and stops it: after this, no Start
// or CreateJob succeeds — held references must not resurrect the engine
// post-shutdown (CON-08). api/core's runtime stop uses this, hot-reload uses
// plain Stop. Idempotent.
func (m *ActressSyncManager) Shutdown() {
	if m == nil {
		return
	}
	m.permanentlyStopped.Store(true)
	m.Stop()
}

func (m *ActressSyncManager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// runDispatch owns the dispatch goroutine lifetime (wg.Done) and restarts
// the loop after a panic with a short backoff (CON-06): a wedged dispatch
// must never silently kill the engine.
func (m *ActressSyncManager) runDispatch(ctx context.Context) {
	defer m.wg.Done() // one Add per runDispatch; dispatchLoop never Done()s
	for {
		crashed := func() (panicked bool) {
			defer func() {
				if value := recover(); value != nil {
					logging.Errorf("Actress sync dispatch panicked, restarting: %v", value)
					panicked = true
				}
			}()
			m.dispatchLoop(ctx)
			return false
		}()
		if !crashed || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// dispatchLoop is pure: lifecycle (wg) belongs to the caller, so a
// panic-recovered restart (runDispatch) cannot desync the WaitGroup.
func (m *ActressSyncManager) dispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	recovery := time.NewTicker(m.recoveryInterval) // CON-01: configurable, not hard-coded
	defer ticker.Stop()
	defer recovery.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		case <-recovery.C:
			_ = m.repo.RecoverExpiredLeases(time.Now().UTC())
		}
		// A wake racing Stop must not claim more tasks after shutdown.
		for ctx.Err() == nil {
			cfg, registry := m.runtimeSnapshot()
			if int(m.active.Load()) >= m.maxWorkers(cfg) {
				break
			}
			timeout := m.taskTimeout(cfg)
			task, err := m.claimAndTrack(ctx, timeout)
			if err != nil {
				// CON-05: exponential claim backoff (1s..60s) instead of a
				// 1-per-second hot error loop; rate-limited logging.
				m.claimFailStreak++
				d := m.backoffDelay(m.claimFailStreak)
				m.taskMu.Lock()
				m.claimBackoffUntil = time.Now().Add(d)
				m.taskMu.Unlock()
				if m.claimFailStreak <= 2 || m.claimFailStreak&(m.claimFailStreak-1) == 0 {
					logging.Warnf("Actress sync claim failed (attempt %d), backing off %s: %v", m.claimFailStreak, d, err)
				}
				break
			}
			if task == nil {
				m.claimFailStreak = 0
				break
			}
			m.claimFailStreak = 0
			m.active.Add(1)
			m.wg.Add(1)
			go func() {
				defer func() {
					task.cancel()
					m.untrackTask(task.task.ID, task.run)
				}()
				m.runTaskWithContext(task.ctx, task.task, timeout, cfg, registry)
			}()
		}
	}
}

// claimedTask bundles a freshly claimed task with its per-run context and
// registration sequence for the launcher goroutine.
type claimedTask struct {
	task   *models.ActressSyncTask
	ctx    context.Context
	cancel context.CancelFunc
	run    uint64
}

// claimAndTrack claims a task and registers it for cancellation under a
// single taskMu hold (CON-06: always defer-unlocked): CancelJob commits the
// DB flag before sweeping, so a task claimed just before a cancel commit is
// seen by the sweep, and ClaimNext filters jobs whose cancel already
// committed. No task can slip between.
func (m *ActressSyncManager) claimAndTrack(ctx context.Context, timeout time.Duration) (*claimedTask, error) {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()
	if !m.claimBackoffUntil.IsZero() && time.Now().Before(m.claimBackoffUntil) {
		return nil, nil // CON-05 backoff window still open
	}
	task, err := m.repo.ClaimNext(m.owner, time.Now().UTC().Add(timeout+30*time.Second))
	if err != nil || task == nil {
		return nil, err
	}
	taskCtx, taskCancel := context.WithCancel(ctx)
	run := m.taskRunSeq + 1
	m.taskRunSeq = run
	if m.runningTasks == nil {
		m.runningTasks = make(map[string][]trackedSyncTask)
	}
	m.runningTasks[task.ID] = append(m.runningTasks[task.ID], trackedSyncTask{jobID: task.JobID, cancel: taskCancel, run: run})
	return &claimedTask{task: task, ctx: taskCtx, cancel: taskCancel, run: run}, nil
}

// startRetryDelay computes the exponential start-retry backoff (CON-05).
func (m *ActressSyncManager) startRetryDelay() time.Duration {
	return m.backoffDelay(m.startFailStreak)
}

// backoffDelay maps a consecutive-failure streak to exponential backoff from
// the manager base (retryDelay, default 1s) up to the 60s cap. Multiplication
// with early cap, never a raw shift: time.Duration<<63 can yield 0 and would
// defeat the backoff exactly during long outages.
func (m *ActressSyncManager) backoffDelay(streak int) time.Duration {
	if streak < 1 {
		streak = 1
	}
	base := m.retryDelay
	if base <= 0 {
		base = time.Second
	}
	d := base
	for i := 1; i < streak; i++ {
		d *= 2
		if d >= 60*time.Second {
			return 60 * time.Second
		}
	}
	return d
}

func (m *ActressSyncManager) runtimeSnapshot() (*config.Config, *scraperutil.ScraperRegistry) {
	if m.deps.Snapshot == nil {
		return nil, nil
	}
	return m.deps.Snapshot()
}

func (m *ActressSyncManager) maxWorkers(cfg *config.Config) int {
	if cfg != nil && cfg.Performance.MaxWorkers > 0 {
		return cfg.Performance.MaxWorkers
	}
	return 5
}

func (m *ActressSyncManager) taskTimeout(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.Scrapers.RequestTimeoutSeconds > 0 {
		return time.Duration(cfg.Scrapers.RequestTimeoutSeconds) * time.Second
	}
	return 60 * time.Second
}

// CreateJob ...
func (m *ActressSyncManager) CreateJob(ctx context.Context, req ActressSyncCreateRequest) (*models.ActressSyncJob, []uint, error) {
	if m == nil || m.repo == nil || m.deps.ActressRepo == nil {
		return nil, nil, ErrActressSyncManagerUnavailable
	}
	if m.permanentlyStopped.Load() {
		return nil, nil, ErrActressSyncManagerUnavailable
	}
	scope := strings.TrimSpace(req.Scope)
	if scope != "missing" && scope != "selected" {
		return nil, nil, fmt.Errorf("scope must be missing or selected")
	}
	ids := uniqueActressIDs(req.ActressIDs)
	var candidateMap map[uint]*models.Actress
	if scope == "missing" {
		candidates, err := m.deps.ActressRepo.ListSyncCandidates(ctx)
		if err != nil {
			return nil, nil, err
		}
		ids = ids[:0]
		candidateMap = make(map[uint]*models.Actress, len(candidates))
		for i := range candidates {
			ids = append(ids, candidates[i].ID)
			candidateMap[candidates[i].ID] = &candidates[i]
		}
	}
	if scope == "selected" && len(ids) == 0 {
		return nil, nil, fmt.Errorf("actress_ids is required for selected scope")
	}
	if len(ids) == 0 {
		return nil, nil, &NoCandidatesError{}
	}
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: scope, CreatedAt: now}
	tasks := make([]models.ActressSyncTask, 0, len(ids))
	skippedIDs := make([]uint, 0)
	for _, id := range ids {
		var actress *models.Actress
		var err error
		if candidateMap != nil {
			actress = candidateMap[id]
		}
		if actress == nil {
			actress, err = m.deps.ActressRepo.FindByID(ctx, id)
			if err != nil {
				// Stale client selections appear after sync merges delete
				// duplicates; skip them instead of rejecting the whole job.
				if database.IsNotFound(err) {
					skippedIDs = append(skippedIDs, id)
					continue
				}
				return nil, skippedIDs, err
			}
		}
		actressID := id
		label := strings.TrimSpace(actress.FullName())
		if label == "" {
			label = fmt.Sprintf("#%d", id)
		}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actressID, Label: label, DedupeKey: fmt.Sprintf("actress:%d", id), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		tasks = append(tasks, task)
	}
	if len(skippedIDs) > 0 {
		logging.Infof("Actress sync: skipped %d actress(es) already merged away by an earlier sync", len(skippedIDs))
	}
	if len(tasks) == 0 {
		return nil, skippedIDs, &NoCandidatesError{SkippedIDs: skippedIDs}
	}
	if err := m.repo.CreateJob(job, tasks); err != nil {
		return nil, skippedIDs, err
	}
	m.Start()
	m.signal()
	return job, skippedIDs, nil
}

// CON-04: no process-wide retry gate — the repo's ClaimNext NOT-EXISTS
// deferral already prevents re-claiming while the canonical task runs. The
// attempt is handed back (never consumed: the task did no work).
func (m *ActressSyncManager) requeueCanonicalTask(task *models.ActressSyncTask, err error) bool {
	if !errors.Is(err, database.ErrActressSyncCanonicalTaskRunning) {
		return false
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, requeueErr := m.repo.RequeueTask(writeCtx, task.ID, task.LeaseToken, database.ActressSyncRequeueOptions{}); requeueErr != nil {
		if !errors.Is(requeueErr, database.ErrActressSyncLeaseLost) {
			logging.Warnf("Actress sync task requeue failed: %v", requeueErr)
		}
	}
	return true
}

// requeueStaleTask implements CON-03/R1: identity-changed / stale-plan errors
// are benign concurrent edits — requeue WITHOUT consuming the attempt; the
// persisted stale counter (repo-capped at 3) terminal-fails beyond it.
func (m *ActressSyncManager) requeueStaleTask(task *models.ActressSyncTask, err error) bool {
	if !errors.Is(err, database.ErrActressSyncIdentityChanged) && !errors.Is(err, database.ErrActressMergeStalePlan) {
		return false
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	count, requeueErr := m.repo.RequeueTask(writeCtx, task.ID, task.LeaseToken, database.ActressSyncRequeueOptions{StaleRetry: true})
	if requeueErr != nil {
		if !errors.Is(requeueErr, database.ErrActressSyncLeaseLost) {
			logging.Warnf("Actress sync stale requeue failed: %v", requeueErr)
		}
		return false
	}
	logging.Infof("Actress sync task %s stale-requeued (count %d)", task.ID, count)
	return true
}

func mergeActressesWithSourceCallback(ctx context.Context, repo *database.ActressRepository, task *models.ActressSyncTask) func(uint, uint, models.Actress) (*database.ActressMergeResult, error) {
	return func(targetID, sourceID uint, expectedSource models.Actress) (*database.ActressMergeResult, error) {
		return mergeActressesWithSourceForTask(ctx, repo, task, targetID, sourceID, expectedSource)
	}
}

func mergeActressesWithSourceForTask(ctx context.Context, repo *database.ActressRepository, task *models.ActressSyncTask, targetID, sourceID uint, expectedSource models.Actress) (*database.ActressMergeResult, error) {
	merged, mergeErr := repo.MergeForSyncTaskWithSource(ctx, targetID, sourceID, nil, expectedSource, task.ID, task.LeaseToken)
	if mergeErr == nil {
		task.ActressID = &targetID
	}
	return merged, mergeErr
}

func (m *ActressSyncManager) runTaskWithContext(runCtx context.Context, task *models.ActressSyncTask, timeout time.Duration, cfg *config.Config, registry *scraperutil.ScraperRegistry) {
	defer m.wg.Done()
	defer func() { m.active.Add(-1); m.signal() }()
	ctx, cancel := context.WithTimeout(runCtx, timeout)
	defer cancel()
	done := make(chan struct{})
	go m.heartbeat(ctx, task.ID, task.LeaseToken, timeout, done, cancel)
	defer close(done)
	defer func() {
		if value := recover(); value != nil {
			task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, actressSyncOutcomeFailed, fmt.Sprintf("panic: %v", value)
			_ = m.repo.CompleteTask(task, task.LeaseToken)
		}
	}()
	job, err := m.repo.FindJob(task.JobID)
	if err != nil {
		task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, actressSyncOutcomeFailed, err.Error()
		_ = m.repo.CompleteTask(task, task.LeaseToken)
		return
	}
	if task.ActressID == nil {
		task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, actressSyncOutcomeFailed, "actress sync task has no actress ID"
		_ = m.repo.CompleteTask(task, task.LeaseToken)
		return
	}
	scrapeActress := true
	var scrapersPriority, actressFieldPriority []string
	if cfg != nil {
		scrapeActress = cfg.Scrapers.ScrapeActress
		scrapersPriority = append(scrapersPriority, cfg.Scrapers.Priority...)
		actressFieldPriority = append(actressFieldPriority, cfg.Metadata.Priority.Fields["actress"]...)
	}
	result, err := SyncActressMetadata(ctx, *task.ActressID, m.deps.ActressRepo, m.deps.MovieRepo, registry, ActressSyncOptions{
		Revalidate:           job.Scope == "selected",
		PriorUpdatedFields:   append([]string(nil), task.UpdatedFields...),
		ScrapeActress:        &scrapeActress,
		ScrapersPriority:     scrapersPriority,
		ActressFieldPriority: actressFieldPriority,

		MergeActressesWithSource: mergeActressesWithSourceCallback(ctx, m.deps.ActressRepo, task),
		MergeActressesWithTargetSource: func(targetID, sourceID uint, expectedTarget, expectedSource models.Actress) (*database.ActressMergeResult, error) {
			merged, mergeErr := m.deps.ActressRepo.MergeForSyncTaskWithTargetAndSource(ctx, targetID, sourceID, nil, expectedTarget, expectedSource, task.ID, task.LeaseToken)
			if mergeErr == nil {
				task.ActressID = &targetID
			}
			return merged, mergeErr
		},
		MergeCachedIdentityWithSource: func(targetID, sourceID uint, expectedDMMID int, expectedSource models.Actress) (*database.ActressMergeResult, error) {
			merged, mergeErr := m.deps.ActressRepo.MergeCachedIdentityForSyncTaskWithSource(ctx, targetID, sourceID, expectedDMMID, expectedSource, task.ID, task.LeaseToken)
			if mergeErr == nil {
				task.ActressID = &targetID
			}
			return merged, mergeErr
		},

		AssignDMMIDWithSource: func(id uint, dmmID int, expectedSource models.Actress) (bool, error) {
			return m.deps.ActressRepo.AssignDMMIDIfMissingForSyncTaskWithSource(ctx, id, dmmID, expectedSource, task.ID, task.LeaseToken)
		},
		FillMetadata: func(id uint, dmmID int, info models.ActressInfo) ([]string, error) {
			return m.deps.ActressRepo.FillBlankMetadataForSyncTask(ctx, id, dmmID, info, task.ID, task.LeaseToken)
		},
		ReplaceThumbnail: func(id uint, dmmID int, expected, replacement string) (bool, error) {
			return m.deps.ActressRepo.ReplaceThumbnailForSyncTask(ctx, id, dmmID, expected, replacement, task.ID, task.LeaseToken)
		},
		ValidateThumbnail: imageutil.ValidateRemoteImage,
		LookupCache: func(dmmID int, japaneseName, firstName, lastName string) (models.ActressInfo, bool) {
			record, ok := actresscache.Lookup(dmmID, japaneseName, firstName, lastName)
			if !ok {
				return models.ActressInfo{}, false
			}
			return models.ActressInfo{DMMID: record.DMMID, FirstName: record.FirstName, LastName: record.LastName, JapaneseName: record.JapaneseName, ThumbURL: record.ThumbURL, Aliases: record.Aliases}, true
		},
	})
	// Task cancellation and timeout supersede whatever the sync path
	// reported: scrapers may swallow context errors and return a benign
	// outcome (e.g. "missing_dmm_id") that would otherwise be persisted as
	// skipped, hiding the interruption.
	if ctxErr := ctx.Err(); ctxErr != nil {
		switch {
		case m.isTaskCancelled(task.ID):
			task.Status, task.Outcome = models.ActressSyncTaskCancelled, "cancelled"
			task.ErrorMessage = ""
			if completeErr := m.repo.CompleteTask(task, task.LeaseToken); completeErr != nil {
				logging.Warnf("Actress sync settle completion failed: %v", completeErr)
			}
			return
		case errors.Is(ctxErr, context.DeadlineExceeded):
			task.Status, task.Outcome = models.ActressSyncTaskFailed, actressSyncOutcomeFailed
			task.ErrorMessage = fmt.Sprintf("actress sync timed out after %s", timeout)
			if completeErr := m.repo.CompleteTask(task, task.LeaseToken); completeErr != nil {
				logging.Warnf("Actress sync settle completion failed: %v", completeErr)
			}
			return
		default:
			// Manager shutdown: never persist an outcome here — cancellation
			// may have been swallowed into a benign result (e.g. skipped with
			// "missing_dmm_id"). Keep the lease so startup recovery requeues
			// the task and it runs to a truthful outcome after restart.
			return
		}
	}
	if err != nil {
		// CON-04 then CON-03: canonical-running gets a free hand-back;
		// identity-change / stale-plan are stale retries (persisted counter,
		// repo terminal-fails past 3) — neither burns the attempt.
		if m.requeueCanonicalTask(task, err) || m.requeueStaleTask(task, err) {
			return
		}
		task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, actressSyncOutcomeFailed, err.Error()
	} else {
		task.Messages, task.UpdatedFields, task.Warning = result.Messages, result.UpdatedFields, result.Warning
		switch {
		case result.Conflict:
			task.Status, task.Outcome = models.ActressSyncTaskConflict, "conflict"
		case len(result.UpdatedFields) > 0 && result.Warning != "":
			task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated_with_warning"
		case len(result.UpdatedFields) > 0:
			task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated"
		case result.Verified:
			task.Status, task.Outcome = models.ActressSyncTaskCompleted, "verified"
		default:
			task.Status, task.Outcome = models.ActressSyncTaskSkipped, "skipped"
		}
	}
	if err := m.repo.CompleteTask(task, task.LeaseToken); err != nil {
		logging.Warnf("Actress sync completion failed: %v", err)
	}
}

// heartbeat extends the task lease on a third-of-timeout cadence. CON-02:
// transient failures retry with capped backoff until the task finishes or
// its context dies; ErrActressSyncLeaseLost aborts the task's own context so
// a task that lost its lease can never run on headless (double-processing).
func (m *ActressSyncManager) heartbeat(ctx context.Context, id, token string, timeout time.Duration, done <-chan struct{}, abort context.CancelFunc) {
	interval := timeout / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
		}
		err := m.repo.Heartbeat(id, token, time.Now().UTC().Add(timeout+30*time.Second))
		if err == nil {
			backoff = time.Second
			continue
		}
		if errors.Is(err, database.ErrActressSyncLeaseLost) {
			logging.Warnf("Actress sync heartbeat lost lease for task %s; aborting task context", id)
			abort()
			return
		}
		logging.Warnf("Actress sync heartbeat failed, retrying in %s: %v", backoff, err)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-done:
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// GetJob ...
func (m *ActressSyncManager) GetJob(id string) (*models.ActressSyncJob, error) {
	return m.repo.FindJob(id)
}

// ListActiveJobs ...
func (m *ActressSyncManager) ListActiveJobs() ([]models.ActressSyncJob, error) {
	return m.repo.ListActiveJobs()
}

// ListTasks ...
func (m *ActressSyncManager) ListTasks(id string, limit int) ([]models.ActressSyncTask, error) {
	if _, err := m.repo.FindJob(id); err != nil {
		return nil, err
	}
	return m.repo.ListTasks(id, limit)
}

// CountTasks returns the unbounded task count for the given view.
func (m *ActressSyncManager) CountTasks(id, view string) (int64, error) {
	if _, err := m.repo.FindJob(id); err != nil {
		return 0, err
	}
	return m.repo.CountTasks(id, view)
}

// ListRunningTasks returns currently running tasks for a sync job.
func (m *ActressSyncManager) ListRunningTasks(id string) ([]models.ActressSyncTask, error) {
	if _, err := m.repo.FindJob(id); err != nil {
		return nil, err
	}
	return m.repo.ListRunningTasks(id)
}

// ListDiagnosticTasks returns a bounded terminal-task diagnostic history for a sync job.
func (m *ActressSyncManager) ListDiagnosticTasks(id string, limit int) ([]models.ActressSyncTask, error) {
	if _, err := m.repo.FindJob(id); err != nil {
		return nil, err
	}
	return m.repo.ListDiagnosticTasks(id, limit)
}

// CancelJob ...
func (m *ActressSyncManager) CancelJob(id string) error {
	if err := m.repo.CancelJob(id); err != nil {
		return err
	}
	m.taskMu.Lock()
	for taskID, runs := range m.runningTasks {
		for i := range runs {
			if runs[i].jobID == id {
				runs[i].cancelled = true
				runs[i].cancel()
			}
		}
		m.runningTasks[taskID] = runs
	}
	m.taskMu.Unlock()
	m.signal()
	return nil
}

// untrackTask drops a finished run from the cancellation registry. run must
// match the current registration: a retried task may already have replaced
// the entry, and removing it would cancel the retry's context.
func (m *ActressSyncManager) untrackTask(id string, run uint64) {
	m.taskMu.Lock()
	runs := m.runningTasks[id]
	kept := runs[:0]
	for _, entry := range runs {
		if entry.run != run {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		delete(m.runningTasks, id)
	} else {
		m.runningTasks[id] = kept
	}
	m.taskMu.Unlock()
}

// isTaskCancelled reports whether a running task was aborted by a job
// cancellation through this manager instance.
func (m *ActressSyncManager) isTaskCancelled(taskID string) bool {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()
	for _, entry := range m.runningTasks[taskID] {
		if entry.cancelled {
			return true
		}
	}
	return false
}

// uniqueActressIDs ...
func uniqueActressIDs(ids []uint) []uint {
	seen := map[uint]struct{}{}
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

//nolint:unused // used by tests
func (m *ActressSyncManager) runTask(task *models.ActressSyncTask, timeout time.Duration, registry *scraperutil.ScraperRegistry) {
	m.mu.Lock()
	runCtx := m.ctx
	m.mu.Unlock()
	if runCtx == nil {
		runCtx = context.Background()
	}
	cfg, _ := m.runtimeSnapshot()
	m.runTaskWithContext(runCtx, task, timeout, cfg, registry)
}
