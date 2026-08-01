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

// ActressSyncCreateRequest ...
type ActressSyncCreateRequest struct {
	Scope      string `json:"scope"`
	ActressIDs []uint `json:"actress_ids"`
}

// ActressSyncManager ...
type ActressSyncManager struct {
	deps                ActressSyncManagerDeps
	repo                *database.ActressSyncRepository
	owner               string
	mu                  sync.Mutex
	started             bool
	retryTimer          *time.Timer
	retryGeneration     uint64
	canonicalRetryUntil atomic.Int64
	retryDelay          time.Duration
	ctx                 context.Context
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
	wake                chan struct{}
	active              atomic.Int32
}

// NewActressSyncManager ...
func NewActressSyncManager(deps ActressSyncManagerDeps) *ActressSyncManager {
	m := &ActressSyncManager{deps: deps, owner: uuid.NewString(), wake: make(chan struct{}, 1), retryDelay: time.Second}
	if deps.DB != nil {
		m.repo = database.NewActressSyncRepository(deps.DB)
	}
	return m
}

// Start ...
func (m *ActressSyncManager) Start() {
	if m == nil || m.repo == nil {
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
		logging.Warnf("Actress sync startup failed, scheduling retry: %v", err)
		m.scheduleStartRetry()
		return
	}
	if len(jobs) == 0 {
		return
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.started = true
	if err := m.repo.RecoverExpiredLeases(time.Now().UTC()); err != nil {
		logging.Warnf("Actress sync recovery failed: %v", err)
	}
	m.wg.Add(1)
	go m.dispatch(m.ctx)
}

func (m *ActressSyncManager) scheduleStartRetry() {
	if m.retryTimer != nil {
		return
	}
	generation := m.retryGeneration
	m.retryTimer = time.AfterFunc(m.retryDelay, func() {
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

// Stop ...
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
	m.canonicalRetryUntil.Store(0)
	if !m.started {
		return
	}
	m.started = false
	m.cancel()
	m.wg.Wait()
	if err := m.repo.ReleaseOwnerLeases(m.owner); err != nil {
		logging.Warnf("Actress sync lease release failed: %v", err)
	}
	m.ctx = nil
	m.cancel = nil
}

func (m *ActressSyncManager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *ActressSyncManager) dispatch(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(time.Second)
	recovery := time.NewTicker(15 * time.Second)
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
		for !m.canonicalRetryPending() {
			cfg, registry := m.runtimeSnapshot()
			if int(m.active.Load()) >= m.maxWorkers(cfg) {
				break
			}
			timeout := m.taskTimeout(cfg)
			task, err := m.repo.ClaimNext(m.owner, time.Now().UTC().Add(timeout+30*time.Second))
			if err != nil {
				logging.Warnf("Actress sync claim failed: %v", err)
				break
			}
			if task == nil {
				break
			}
			m.active.Add(1)
			m.wg.Add(1)
			go m.runTaskWithContext(ctx, task, timeout, registry)
		}
	}
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
func (m *ActressSyncManager) CreateJob(ctx context.Context, req ActressSyncCreateRequest) (*models.ActressSyncJob, error) {
	if m == nil || m.repo == nil || m.deps.ActressRepo == nil {
		return nil, fmt.Errorf("actress sync manager is unavailable")
	}
	scope := strings.TrimSpace(req.Scope)
	if scope != "missing" && scope != "selected" {
		return nil, fmt.Errorf("scope must be missing or selected")
	}
	ids := uniqueActressIDs(req.ActressIDs)
	var candidateMap map[uint]*models.Actress
	if scope == "missing" {
		candidates, err := m.deps.ActressRepo.ListSyncCandidates(ctx)
		if err != nil {
			return nil, err
		}
		ids = ids[:0]
		candidateMap = make(map[uint]*models.Actress, len(candidates))
		for i := range candidates {
			ids = append(ids, candidates[i].ID)
			candidateMap[candidates[i].ID] = &candidates[i]
		}
	}
	if scope == "selected" && len(ids) == 0 {
		return nil, fmt.Errorf("actress_ids is required for selected scope")
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no actresses require metadata sync")
	}
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: scope, CreatedAt: now}
	tasks := make([]models.ActressSyncTask, 0, len(ids))
	for _, id := range ids {
		var actress *models.Actress
		var err error
		if candidateMap != nil {
			actress = candidateMap[id]
		}
		if actress == nil {
			actress, err = m.deps.ActressRepo.FindByID(ctx, id)
			if err != nil {
				return nil, err
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
	if err := m.repo.CreateJob(job, tasks); err != nil {
		return nil, err
	}
	m.Start()
	m.signal()
	return job, nil
}
func (m *ActressSyncManager) requeueCanonicalTask(task *models.ActressSyncTask, err error) bool {
	if !errors.Is(err, database.ErrActressSyncCanonicalTaskRunning) {
		return false
	}
	if requeueErr := m.repo.RequeueTask(task, task.LeaseToken); requeueErr != nil {
		logging.Warnf("Actress sync task requeue failed: %v", requeueErr)
	} else {
		m.scheduleCanonicalRetry()
	}
	return true
}

func (m *ActressSyncManager) scheduleCanonicalRetry() {
	retryUntil := time.Now().Add(m.retryDelay).UnixNano()
	for {
		current := m.canonicalRetryUntil.Load()
		if current >= retryUntil || m.canonicalRetryUntil.CompareAndSwap(current, retryUntil) {
			return
		}
	}
}

func (m *ActressSyncManager) canonicalRetryPending() bool {
	return time.Now().UnixNano() < m.canonicalRetryUntil.Load()
}

func (m *ActressSyncManager) runTaskWithContext(runCtx context.Context, task *models.ActressSyncTask, timeout time.Duration, registry *scraperutil.ScraperRegistry) {
	defer m.wg.Done()
	defer func() { m.active.Add(-1); m.signal() }()
	ctx, cancel := context.WithTimeout(runCtx, timeout)
	defer cancel()
	done := make(chan struct{})
	go m.heartbeat(ctx, task.ID, task.LeaseToken, timeout, done)
	defer close(done)
	defer func() {
		if value := recover(); value != nil {
			task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, "failed", fmt.Sprintf("panic: %v", value)
			_ = m.repo.CompleteTask(task, task.LeaseToken)
		}
	}()
	job, err := m.repo.FindJob(task.JobID)
	if err != nil {
		task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, "failed", err.Error()
		_ = m.repo.CompleteTask(task, task.LeaseToken)
		return
	}
	if task.ActressID == nil {
		task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, "failed", "actress sync task has no actress ID"
		_ = m.repo.CompleteTask(task, task.LeaseToken)
		return
	}
	result, err := SyncActressMetadata(ctx, *task.ActressID, m.deps.ActressRepo, m.deps.MovieRepo, registry, ActressSyncOptions{
		Revalidate:         job.Scope == "selected",
		PriorUpdatedFields: append([]string(nil), task.UpdatedFields...),

		MergeActressesWithSource: func(targetID, sourceID uint, expectedSource models.Actress) (*database.ActressMergeResult, error) {
			merged, mergeErr := m.deps.ActressRepo.MergeForSyncTaskWithSource(ctx, targetID, sourceID, nil, expectedSource, task.ID, task.LeaseToken)
			if mergeErr == nil {
				task.ActressID = &targetID
			}
			return merged, mergeErr
		},
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
	if err != nil {
		if runCtx.Err() != nil && errors.Is(err, context.Canceled) {
			return
		}
		if m.requeueCanonicalTask(task, err) {
			return
		}
		task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, "failed", err.Error()
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

func (m *ActressSyncManager) heartbeat(ctx context.Context, id, token string, timeout time.Duration, done <-chan struct{}) {
	interval := timeout / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := m.repo.Heartbeat(id, token, time.Now().UTC().Add(timeout+30*time.Second)); err != nil {
				logging.Warnf("Actress sync heartbeat failed: %v", err)
				return
			}
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
func (m *ActressSyncManager) ListTasks(id string) ([]models.ActressSyncTask, error) {
	if _, err := m.repo.FindJob(id); err != nil {
		return nil, err
	}
	return m.repo.ListTasks(id)
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
	m.signal()
	return nil
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
	m.runTaskWithContext(runCtx, task, timeout, registry)
}
