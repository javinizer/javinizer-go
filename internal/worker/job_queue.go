package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// JobStatus represents the status of a job
type JobStatus string

// Job State Machine:
//
// Normal flow:
//
//	Pending → Running → Completed → Organized
//
// State descriptions:
//   - Pending: Job created, waiting to start scraping
//   - Running: Scraping in progress (files being processed)
//   - Completed: Scraping finished, metadata available for review/editing
//   - Failed: Job failed during scraping (terminal state)
//   - Cancelled: Job was cancelled by user (terminal state)
//   - Organized: Files successfully organized (terminal state)
//   - Reverted: Files reverted to original state (terminal state)
//
// Organization retry flow:
//
//	Completed → Running (organize) → Completed (if failed > 0)
//	Completed → Running (organize) → Organized (if failed == 0)
//
// Revert flow:
//
//	Organized → Reverted
//
// Key rules:
//   - Only "Completed" jobs can be organized
//   - If organization has any failures, job stays "Completed" to enable retry
//   - If organization fully succeeds (failed == 0), job transitions to "Organized"
//   - "Organized" jobs cannot be organized again (terminal state)
//   - "Reverted" jobs are never deleted by cleanup (no time limit on revert)
//
// Terminal states (no further transitions):
//   - Failed, Cancelled, Organized, Reverted
const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
	JobStatusOrganized JobStatus = "organized"
	JobStatusReverted  JobStatus = "reverted"
)

// FileResult represents the result of processing a single file
type FileResult struct {
	FilePath       string            `json:"file_path"`
	MovieID        string            `json:"movie_id"`
	Revision       uint64            `json:"revision"`
	Status         JobStatus         `json:"status"`
	Error          string            `json:"error,omitempty"`
	PosterError    *string           `json:"poster_error,omitempty"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
	DataType       string            `json:"data_type,omitempty"`
	Data           interface{}       `json:"data,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	EndedAt        *time.Time        `json:"ended_at,omitempty"`
	IsMultiPart    bool              `json:"is_multi_part,omitempty"`
	PartNumber     int               `json:"part_number,omitempty"`
	PartSuffix     string            `json:"part_suffix,omitempty"`
}

const (
	DataTypeMovie = "movie"
)

// FileResultSlim is a lightweight FileResult that omits the Data field
// for efficient status polling without deep-copying movie objects.
type FileResultSlim struct {
	FilePath       string            `json:"file_path"`
	MovieID        string            `json:"movie_id"`
	Revision       uint64            `json:"revision"`
	Status         JobStatus         `json:"status"`
	Error          string            `json:"error,omitempty"`
	PosterError    *string           `json:"poster_error,omitempty"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
	DataType       string            `json:"data_type,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	EndedAt        *time.Time        `json:"ended_at,omitempty"`
	IsMultiPart    bool              `json:"is_multi_part,omitempty"`
	PartNumber     int               `json:"part_number,omitempty"`
	PartSuffix     string            `json:"part_suffix,omitempty"`
}

// BatchJobSlim is a lightweight BatchJob snapshot that uses FileResultSlim
// to avoid deep-copying movie Data on every poll.
type BatchJobSlim struct {
	ID                          string                     `json:"id"`
	Status                      JobStatus                  `json:"status"`
	TotalFiles                  int                        `json:"total_files"`
	Completed                   int                        `json:"completed"`
	Failed                      int                        `json:"failed"`
	Excluded                    map[string]bool            `json:"excluded"`
	Files                       []string                   `json:"files"`
	Results                     map[string]*FileResultSlim `json:"results"`
	FileMatchInfo               map[string]FileMatchInfo   `json:"file_match_info,omitempty"`
	Progress                    float64                    `json:"progress"`
	Destination                 string                     `json:"destination"`
	TempDir                     string                     `json:"temp_dir"`
	StartedAt                   time.Time                  `json:"started_at"`
	CompletedAt                 *time.Time                 `json:"completed_at,omitempty"`
	OrganizedAt                 *time.Time                 `json:"organized_at,omitempty"`
	RevertedAt                  *time.Time                 `json:"reverted_at,omitempty"`
	MoveToFolderOverride        *bool                      `json:"move_to_folder_override,omitempty"`
	RenameFolderInPlaceOverride *bool                      `json:"rename_folder_in_place_override,omitempty"`
	OperationModeOverride       string                     `json:"operation_mode_override,omitempty"`
	PersistError                string                     `json:"persist_error,omitempty"`
}

type fileResultAlias FileResult

func (fr *FileResult) MarshalJSON() ([]byte, error) {
	if fr.Data != nil {
		if _, ok := fr.Data.(*models.Movie); ok {
			fr.DataType = DataTypeMovie
		}
	}
	return json.Marshal(fileResultAlias(*fr))
}

func (fr *FileResult) UnmarshalJSON(data []byte) error {
	var alias fileResultAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*fr = FileResult(alias)

	if fr.DataType == DataTypeMovie && fr.Data != nil {
		var movie models.Movie
		rawData, err := json.Marshal(fr.Data)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(rawData, &movie); err != nil {
			return err
		}
		fr.Data = &movie
	}
	return nil
}

// BatchJob represents a batch processing job
type BatchJob struct {
	ID                          string                   `json:"id"`
	Status                      JobStatus                `json:"status"`
	TotalFiles                  int                      `json:"total_files"`
	Completed                   int                      `json:"completed"`
	Failed                      int                      `json:"failed"`
	Excluded                    map[string]bool          `json:"excluded"`
	Files                       []string                 `json:"files"`
	Results                     map[string]*FileResult   `json:"results"`
	FileMatchInfo               map[string]FileMatchInfo `json:"file_match_info,omitempty"`
	Progress                    float64                  `json:"progress"`
	Destination                 string                   `json:"destination"`
	TempDir                     string                   `json:"temp_dir"`
	StartedAt                   time.Time                `json:"started_at"`
	CompletedAt                 *time.Time               `json:"completed_at,omitempty"`
	OrganizedAt                 *time.Time               `json:"organized_at,omitempty"`
	RevertedAt                  *time.Time               `json:"reverted_at,omitempty"`
	MoveToFolderOverride        *bool                    `json:"move_to_folder_override,omitempty"`
	RenameFolderInPlaceOverride *bool                    `json:"rename_folder_in_place_override,omitempty"`
	OperationModeOverride       string                   `json:"operation_mode_override,omitempty"`
	PersistError                string                   `json:"persist_error,omitempty"`
	CancelFunc                  context.CancelFunc       `json:"-"`
	Done                        chan struct{}            `json:"-"`
	mu                          sync.RWMutex             `json:"-"`
	deleted                     bool                     `json:"-"` // Tombstone flag - prevents persist after deletion
	templateEngine              *template.Engine         `json:"-"` // Shared template engine (safe for concurrent use)
}

// Lock acquires the job's write lock for exclusive access
// Use this when performing mutations that require atomic state validation
func (job *BatchJob) Lock() {
	job.mu.Lock()
}

// Unlock releases the job's write lock
func (job *BatchJob) Unlock() {
	job.mu.Unlock()
}

// RLock acquires the job's read lock for shared access
func (job *BatchJob) RLock() {
	job.mu.RLock()
}

// RUnlock releases the job's read lock
func (job *BatchJob) RUnlock() {
	job.mu.RUnlock()
}

// IsDeleted returns the tombstone flag
// Caller must hold at least RLock to safely read this value
func (job *BatchJob) IsDeleted() bool {
	return job.deleted
}

// FileMatchInfo stores match metadata for a file (populated during discovery)
type FileMatchInfo struct {
	MovieID     string `json:"movie_id"`
	IsMultiPart bool   `json:"is_multi_part"`
	PartNumber  int    `json:"part_number"`
	PartSuffix  string `json:"part_suffix"`
}

// JobQueue manages batch jobs
type JobQueue struct {
	jobs           map[string]*BatchJob
	jobRepo        database.JobRepositoryInterface
	tempDir        string
	templateEngine *template.Engine
	mu             sync.RWMutex
	stopCleanup    chan struct{}
}

func NewJobQueue(jobRepo database.JobRepositoryInterface, tempDir string, engine *template.Engine) *JobQueue {
	if engine == nil {
		engine = template.NewEngine()
	}
	jq := &JobQueue{
		jobs:           make(map[string]*BatchJob),
		jobRepo:        jobRepo,
		tempDir:        tempDir,
		templateEngine: engine,
		stopCleanup:    make(chan struct{}),
	}

	jq.loadFromDatabase()
	jq.StartCleanup()

	return jq
}

// StartCleanup starts a background goroutine that cleans up old organized jobs every hour
func (jq *JobQueue) StartCleanup() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				jq.cleanupOldOrganizedJobs()
			case <-jq.stopCleanup:
				return
			}
		}
	}()
}

func (jq *JobQueue) StopCleanup() {
	close(jq.stopCleanup)
}

// cleanupOldOrganizedJobs is disabled per D-05/HIST-11:
// organized batch jobs must persist indefinitely for revert eligibility.
// Reverted jobs are also never deleted per D-06.
func (jq *JobQueue) cleanupOldOrganizedJobs() {
	// No-op: organized and reverted jobs must persist for revert eligibility
}

// loadFromDatabase loads existing jobs from the database on startup
func (jq *JobQueue) loadFromDatabase() {
	if jq.jobRepo == nil {
		return
	}

	jobs, err := jq.jobRepo.List()
	if err != nil {
		logging.Warnf("Failed to load jobs from database: %v", err)
		return
	}

	for i := range jobs {
		batchJob := jq.reconstructBatchJob(&jobs[i])
		if batchJob != nil {
			jq.jobs[batchJob.ID] = batchJob
		}
	}
}

// reconstructBatchJob reconstructs a BatchJob from a database Job model
func (jq *JobQueue) reconstructBatchJob(dbJob *models.Job) *BatchJob {
	batchJob := &BatchJob{
		ID:            dbJob.ID,
		Status:        JobStatus(dbJob.Status),
		TotalFiles:    dbJob.TotalFiles,
		Completed:     dbJob.Completed,
		Failed:        dbJob.Failed,
		Progress:      dbJob.Progress,
		Destination:   dbJob.Destination,
		TempDir:       dbJob.TempDir,
		StartedAt:     dbJob.StartedAt,
		CompletedAt:   dbJob.CompletedAt,
		OrganizedAt:   dbJob.OrganizedAt,
		RevertedAt:    dbJob.RevertedAt,
		Results:       make(map[string]*FileResult),
		Excluded:      make(map[string]bool),
		FileMatchInfo: make(map[string]FileMatchInfo),
		Done:          make(chan struct{}),
	}

	// Parse Files JSON
	if dbJob.Files != "" {
		if err := json.Unmarshal([]byte(dbJob.Files), &batchJob.Files); err != nil {
			logging.Warnf("Failed to parse files for job %s: %v", dbJob.ID, err)
		}
	}

	// Parse Results JSON
	if dbJob.Results != "" {
		if err := json.Unmarshal([]byte(dbJob.Results), &batchJob.Results); err != nil {
			logging.Warnf("Failed to parse results for job %s: %v", dbJob.ID, err)
		}
	}

	// Use the job's stored TempDir to check for temp posters
	if batchJob.TempDir != "" {
		posterDir := filepath.Join(batchJob.TempDir, "posters", dbJob.ID)
		for _, result := range batchJob.Results {
			if result.Data == nil {
				continue
			}
			movie, ok := result.Data.(*models.Movie)
			if !ok || movie == nil {
				continue
			}
			if movie.CroppedPosterURL == "" {
				continue
			}
			tempPosterPath := filepath.Join(posterDir, movie.ID+".jpg")
			if _, err := os.Stat(tempPosterPath); os.IsNotExist(err) {
				movie.CroppedPosterURL = ""
				logging.Debugf("[Job %s] Cleared missing temp poster URL for %s", dbJob.ID, movie.ID)
			}
		}
	}

	// Parse Excluded JSON
	if dbJob.Excluded != "" {
		if err := json.Unmarshal([]byte(dbJob.Excluded), &batchJob.Excluded); err != nil {
			logging.Warnf("Failed to parse excluded for job %s: %v", dbJob.ID, err)
		}
	}

	// Parse FileMatchInfo JSON
	if dbJob.FileMatchInfo != "" {
		if err := json.Unmarshal([]byte(dbJob.FileMatchInfo), &batchJob.FileMatchInfo); err != nil {
			logging.Warnf("Failed to parse file match info for job %s: %v", dbJob.ID, err)
		}
	}

	// Close Done channel for terminal states
	switch batchJob.Status {
	case JobStatusCompleted, JobStatusFailed, JobStatusCancelled, JobStatusOrganized, JobStatusReverted:
		close(batchJob.Done)
	}

	return batchJob
}

// persistToDatabase saves a BatchJob to the database
func (jq *JobQueue) persistToDatabase(job *BatchJob) {
	if jq.jobRepo == nil {
		return
	}

	job.mu.RLock()
	if job.deleted {
		job.mu.RUnlock()
		logging.Debugf("[Job %s] Skipping persist - job marked as deleted", job.ID)
		return
	}

	// Marshal fields to JSON
	filesJSON, err := json.Marshal(job.Files)
	if err != nil {
		job.mu.RUnlock()
		logging.Warnf("Failed to marshal files for job %s: %v", job.ID, err)
		return
	}

	resultsJSON, err := json.Marshal(job.Results)
	if err != nil {
		job.mu.RUnlock()
		logging.Warnf("Failed to marshal results for job %s: %v", job.ID, err)
		return
	}

	excludedJSON, err := json.Marshal(job.Excluded)
	if err != nil {
		job.mu.RUnlock()
		logging.Warnf("Failed to marshal excluded for job %s: %v", job.ID, err)
		return
	}

	fileMatchInfoJSON, err := json.Marshal(job.FileMatchInfo)
	if err != nil {
		job.mu.RUnlock()
		logging.Warnf("Failed to marshal file match info for job %s: %v", job.ID, err)
		return
	}

	// Read TempDir directly since we already hold RLock (GetTempDir would re-acquire)
	tempDir := job.TempDir

	dbJob := &models.Job{
		ID:            job.ID,
		Status:        string(job.Status),
		TotalFiles:    job.TotalFiles,
		Completed:     job.Completed,
		Failed:        job.Failed,
		Progress:      job.Progress,
		Destination:   job.Destination,
		TempDir:       tempDir,
		Files:         string(filesJSON),
		Results:       string(resultsJSON),
		Excluded:      string(excludedJSON),
		FileMatchInfo: string(fileMatchInfoJSON),
		StartedAt:     job.StartedAt,
		CompletedAt:   job.CompletedAt,
		OrganizedAt:   job.OrganizedAt,
		RevertedAt:    job.RevertedAt,
	}

	// Try to update first, if not found then create
	existing, err := jq.jobRepo.FindByID(job.ID)
	if err != nil || existing == nil {
		if err := jq.jobRepo.Create(dbJob); err != nil {
			logging.Warnf("Failed to create job %s in database: %v", job.ID, err)
			job.mu.RUnlock()
			job.SetPersistError(fmt.Sprintf("create failed: %v", err))
			return
		}
		job.mu.RUnlock()
		job.SetPersistError("")
		return
	}
	if err := jq.jobRepo.Update(dbJob); err != nil {
		logging.Warnf("Failed to update job %s in database: %v", job.ID, err)
		job.mu.RUnlock()
		job.SetPersistError(fmt.Sprintf("update failed: %v", err))
		return
	}
	job.mu.RUnlock()
	job.SetPersistError("")
}

// CreateJob creates a new batch job
func (jq *JobQueue) CreateJob(files []string) *BatchJob {
	job := &BatchJob{
		ID:             uuid.New().String(),
		Status:         JobStatusPending,
		TotalFiles:     len(files),
		Files:          files,
		Results:        make(map[string]*FileResult),
		FileMatchInfo:  make(map[string]FileMatchInfo),
		Excluded:       make(map[string]bool),
		Done:           make(chan struct{}),
		StartedAt:      time.Now(),
		TempDir:        jq.tempDir,
		templateEngine: jq.templateEngine,
	}

	jq.mu.Lock()
	jq.jobs[job.ID] = job
	jq.mu.Unlock()

	jq.persistToDatabase(job)

	return job
}

// GetJob retrieves a thread-safe copy of a job by ID
// Returns a deep copy to prevent external mutations of internal state
func (jq *JobQueue) GetJob(id string) (*BatchJob, bool) {
	jq.mu.RLock()
	job, ok := jq.jobs[id]
	jq.mu.RUnlock()

	if !ok {
		return nil, false
	}

	// Return a safe copy using GetStatus
	return job.GetStatus(), true
}

// GetJobPointer retrieves the actual job pointer for internal mutations
// WARNING: This exposes the internal job - use only when mutations are required
// Callers must respect the job's internal mutex (job.mu) when modifying state
func (jq *JobQueue) GetJobPointer(id string) (*BatchJob, bool) {
	jq.mu.RLock()
	job, ok := jq.jobs[id]
	jq.mu.RUnlock()
	return job, ok
}

// DeleteJob removes a job from the queue and cleans up associated temp files
// Cancels the job first and waits for it to fully finish before removing files
// tempDir is the base temp directory (e.g., "data/temp")
// Returns error if job not found, job is running, or database deletion fails
func (jq *JobQueue) DeleteJob(id string, tempDir string) error {
	jq.mu.RLock()
	job, ok := jq.jobs[id]
	jq.mu.RUnlock()

	if !ok {
		return fmt.Errorf("job %s not found", id)
	}

	snap := job.GetStatus()
	if snap.Status == JobStatusRunning {
		return fmt.Errorf("cannot delete running job")
	}

	if snap.Status == JobStatusPending {
		job.Cancel()
	}

	select {
	case <-job.Done:
	case <-time.After(5 * time.Second):
		logging.Warnf("DeleteJob: timed out waiting for job %s to finish, proceeding with cleanup", id)
	}

	job.mu.Lock()
	job.deleted = true
	job.mu.Unlock()

	jq.mu.Lock()
	defer jq.mu.Unlock()

	tempPosterDir := filepath.Join(tempDir, "posters", id)
	if err := os.RemoveAll(tempPosterDir); err != nil {
		logging.Warnf("Failed to clean up temp posters for job %s: %v", id, err)
	} else {
		logging.Debugf("[Job %s] Cleaned up temporary poster directory: %s", id, tempPosterDir)
	}

	if jq.jobRepo != nil {
		if err := jq.jobRepo.Delete(id); err != nil {
			logging.Warnf("Failed to delete job %s from database: %v", id, err)
			return fmt.Errorf("database deletion failed: %w", err)
		}
	}

	delete(jq.jobs, id)
	return nil
}

// PersistJob saves a job to the database
func (jq *JobQueue) PersistJob(job *BatchJob) {
	jq.persistToDatabase(job)
}

// ListJobs returns thread-safe copies of all jobs
// Returns deep copies to prevent external mutations of internal state
func (jq *JobQueue) ListJobs() []*BatchJob {
	jq.mu.RLock()
	// Create a snapshot of job pointers while holding the lock
	jobSnapshots := make([]*BatchJob, 0, len(jq.jobs))
	for _, job := range jq.jobs {
		jobSnapshots = append(jobSnapshots, job)
	}
	jq.mu.RUnlock()

	// Create safe copies of each job (releases lock before expensive copying)
	jobs := make([]*BatchJob, 0, len(jobSnapshots))
	for _, job := range jobSnapshots {
		jobs = append(jobs, job.GetStatus())
	}
	return jobs
}

// UpdateFileResult updates the result for a specific file in the job
func (job *BatchJob) UpdateFileResult(filePath string, result *FileResult) {
	job.mu.Lock()
	defer job.mu.Unlock()

	existing := job.Results[filePath]
	if existing != nil {
		switch existing.Status {
		case JobStatusCompleted:
			job.Completed--
		case JobStatusFailed:
			job.Failed--
		}
	}

	if existing != nil {
		result.Revision = existing.Revision + 1
	} else {
		result.Revision = 1
	}

	job.Results[filePath] = result

	switch result.Status {
	case JobStatusCompleted:
		job.Completed++
	case JobStatusFailed:
		job.Failed++
	}

	if job.TotalFiles == 0 {
		job.Progress = 100
	} else {
		job.Progress = float64(job.Completed+job.Failed) / float64(job.TotalFiles) * 100
	}
}

// AtomicUpdateFileResult performs an atomic read-modify-write on a FileResult
// The updateFn receives a deep copy of the current FileResult and must return the updated version
// This prevents lost-update races by ensuring all modifications happen under the job's lock
func (job *BatchJob) AtomicUpdateFileResult(filePath string, updateFn func(*FileResult) (*FileResult, error)) error {
	job.mu.Lock()
	defer job.mu.Unlock()

	current, exists := job.Results[filePath]
	if !exists || current == nil {
		return fmt.Errorf("file result not found: %s", filePath)
	}

	// Deep copy current to prevent updateFn from accidentally mutating shared state
	copied := *current
	if current.EndedAt != nil {
		t := *current.EndedAt
		copied.EndedAt = &t
	}
	if current.PosterError != nil {
		s := *current.PosterError
		copied.PosterError = &s
	}
	if current.FieldSources != nil {
		copied.FieldSources = make(map[string]string, len(current.FieldSources))
		for k, v := range current.FieldSources {
			copied.FieldSources[k] = v
		}
	}
	if current.ActressSources != nil {
		copied.ActressSources = make(map[string]string, len(current.ActressSources))
		for k, v := range current.ActressSources {
			copied.ActressSources[k] = v
		}
	}

	// Apply the update function
	updated, err := updateFn(&copied)
	if err != nil {
		return err
	}

	// Increment Revision for CAS
	updated.Revision = current.Revision + 1

	// Adjust counters: decrement old status
	switch current.Status {
	case JobStatusCompleted:
		job.Completed--
	case JobStatusFailed:
		job.Failed--
	}

	// Write back the updated result
	job.Results[filePath] = updated

	// Adjust counters: increment new status
	switch updated.Status {
	case JobStatusCompleted:
		job.Completed++
	case JobStatusFailed:
		job.Failed++
	}

	if job.TotalFiles == 0 {
		job.Progress = 100
	} else {
		job.Progress = float64(job.Completed+job.Failed) / float64(job.TotalFiles) * 100
	}

	return nil
}

// GetFileMatchInfo retrieves the FileMatchInfo for a file path (thread-safe)
// Returns the info and true if found, zero value and false if not found
func (job *BatchJob) GetFileMatchInfo(filePath string) (FileMatchInfo, bool) {
	job.mu.RLock()
	defer job.mu.RUnlock()
	info, ok := job.FileMatchInfo[filePath]
	return info, ok
}

// SetFileMatchInfo stores the FileMatchInfo for a file path (thread-safe)
func (job *BatchJob) SetFileMatchInfo(filePath string, info FileMatchInfo) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.FileMatchInfo[filePath] = info
}

// MarkStarted marks the job as started
func (job *BatchJob) MarkStarted() {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Status = JobStatusRunning
	job.StartedAt = time.Now()
	job.CompletedAt = nil
	job.OrganizedAt = nil
	// Create a new Done channel for this run
	// This allows re-using jobs for retry workflows
	job.Done = make(chan struct{})
}

// MarkCompleted marks the job as completed
func (job *BatchJob) MarkCompleted() {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Status = JobStatusCompleted
	now := time.Now()
	job.CompletedAt = &now
	job.Progress = 100
	// Close Done channel to signal completion (idempotent)
	select {
	case <-job.Done:
		// already closed
	default:
		close(job.Done)
	}
}

// MarkFailed marks the job as failed
func (job *BatchJob) MarkFailed() {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Status = JobStatusFailed
	now := time.Now()
	job.CompletedAt = &now
	// Close Done channel to signal completion (idempotent)
	select {
	case <-job.Done:
		// already closed
	default:
		close(job.Done)
	}
}

// MarkCancelled marks the job as cancelled
func (job *BatchJob) MarkCancelled() {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Status = JobStatusCancelled
	now := time.Now()
	job.CompletedAt = &now
	// Close Done channel to signal completion (idempotent)
	select {
	case <-job.Done:
		// already closed
	default:
		close(job.Done)
	}
}

// MarkOrganized marks the job as organized
func (job *BatchJob) MarkOrganized() {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Status = JobStatusOrganized
	now := time.Now()
	job.OrganizedAt = &now
	select {
	case <-job.Done:
	default:
		close(job.Done)
	}
}

// MarkReverted marks the job as reverted
func (job *BatchJob) MarkReverted() {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Status = JobStatusReverted
	now := time.Now()
	job.RevertedAt = &now
	select {
	case <-job.Done:
	default:
		close(job.Done)
	}
}

// SetCancelFunc sets the cancel function for the job (thread-safe)
func (job *BatchJob) SetCancelFunc(cancelFunc context.CancelFunc) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.CancelFunc = cancelFunc
}

// Cancel cancels the job
func (job *BatchJob) Cancel() {
	job.mu.Lock()
	cancelFunc := job.CancelFunc
	job.mu.Unlock()

	if cancelFunc != nil {
		cancelFunc()
	}
	job.MarkCancelled()
}

// GetProgress returns the current progress percentage (thread-safe).
// This is a lightweight accessor that avoids copying the entire job state.
func (job *BatchJob) GetProgress() float64 {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.Progress
}

// ExcludeFile marks a file as excluded from organization (thread-safe)
func (job *BatchJob) ExcludeFile(filePath string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Excluded[filePath] = true
}

// IsExcluded checks if a file is excluded from organization (thread-safe)
func (job *BatchJob) IsExcluded(filePath string) bool {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.Excluded[filePath]
}

// GetStatus returns a thread-safe copy of the job status
func (job *BatchJob) GetStatus() *BatchJob {
	job.mu.RLock()
	defer job.mu.RUnlock()

	// Create a deep copy to avoid race conditions
	// Shallow copy would expose internal pointers and allow concurrent mutations
	results := make(map[string]*FileResult, len(job.Results))
	for k, v := range job.Results {
		if v == nil {
			continue
		}
		// Deep copy the FileResult struct
		copyResult := *v

		// Deep copy pointer fields to prevent shared state
		if v.EndedAt != nil {
			t := *v.EndedAt
			copyResult.EndedAt = &t
		}
		if v.PosterError != nil {
			s := *v.PosterError
			copyResult.PosterError = &s
		}
		if v.FieldSources != nil {
			copyResult.FieldSources = make(map[string]string, len(v.FieldSources))
			for sourceField, sourceName := range v.FieldSources {
				copyResult.FieldSources[sourceField] = sourceName
			}
		}
		if v.ActressSources != nil {
			copyResult.ActressSources = make(map[string]string, len(v.ActressSources))
			for actressKey, sourceName := range v.ActressSources {
				copyResult.ActressSources[actressKey] = sourceName
			}
		}

		// Deep copy the Data payload if it's a *models.Movie to prevent shared mutable state
		if v.Data != nil {
			if m, ok := v.Data.(*models.Movie); ok {
				mCopy := *m

				// Deep copy nested slices to prevent concurrent modifications
				if m.Actresses != nil {
					mCopy.Actresses = make([]models.Actress, len(m.Actresses))
					copy(mCopy.Actresses, m.Actresses)
				}
				if m.Genres != nil {
					mCopy.Genres = make([]models.Genre, len(m.Genres))
					copy(mCopy.Genres, m.Genres)
				}
				if m.Screenshots != nil {
					mCopy.Screenshots = make([]string, len(m.Screenshots))
					copy(mCopy.Screenshots, m.Screenshots)
				}
				if m.Translations != nil {
					mCopy.Translations = make([]models.MovieTranslation, len(m.Translations))
					copy(mCopy.Translations, m.Translations)
				}

				copyResult.Data = &mCopy
			}
			// For unknown types, keep the original pointer (can't easily deep-copy arbitrary types)
		}

		results[k] = &copyResult
	}

	// Deep copy the Files slice to avoid exposing the internal slice
	files := make([]string, len(job.Files))
	copy(files, job.Files)

	// Deep copy the Excluded map
	excluded := make(map[string]bool, len(job.Excluded))
	for k, v := range job.Excluded {
		excluded[k] = v
	}

	// Deep copy the FileMatchInfo map
	fileMatchInfo := make(map[string]FileMatchInfo, len(job.FileMatchInfo))
	for k, v := range job.FileMatchInfo {
		fileMatchInfo[k] = v
	}

	completedAt := job.CompletedAt
	if completedAt != nil {
		t := *completedAt
		completedAt = &t
	}

	organizedAt := job.OrganizedAt
	if organizedAt != nil {
		t := *organizedAt
		organizedAt = &t
	}

	revertedAt := job.RevertedAt
	if revertedAt != nil {
		t := *revertedAt
		revertedAt = &t
	}

	return &BatchJob{
		ID:                          job.ID,
		Status:                      job.Status,
		TotalFiles:                  job.TotalFiles,
		Completed:                   job.Completed,
		Failed:                      job.Failed,
		Excluded:                    excluded,
		Files:                       files,
		Results:                     results,
		FileMatchInfo:               fileMatchInfo,
		Progress:                    job.Progress,
		Destination:                 job.Destination,
		TempDir:                     job.TempDir,
		StartedAt:                   job.StartedAt,
		CompletedAt:                 completedAt,
		OrganizedAt:                 organizedAt,
		RevertedAt:                  revertedAt,
		MoveToFolderOverride:        job.MoveToFolderOverride,
		RenameFolderInPlaceOverride: job.RenameFolderInPlaceOverride,
		OperationModeOverride:       job.OperationModeOverride,
		PersistError:                job.PersistError,
	}
}

// GetStatusSlim returns a lightweight snapshot of the job status without movie Data.
// This is the recommended method for polling endpoints that only need status/progress.
func (job *BatchJob) GetStatusSlim() *BatchJobSlim {
	job.mu.RLock()
	defer job.mu.RUnlock()

	results := make(map[string]*FileResultSlim, len(job.Results))
	for k, v := range job.Results {
		if v == nil {
			continue
		}
		slim := &FileResultSlim{
			FilePath:    v.FilePath,
			MovieID:     v.MovieID,
			Revision:    v.Revision,
			Status:      v.Status,
			Error:       v.Error,
			DataType:    v.DataType,
			StartedAt:   v.StartedAt,
			IsMultiPart: v.IsMultiPart,
			PartNumber:  v.PartNumber,
			PartSuffix:  v.PartSuffix,
		}
		if v.FieldSources != nil {
			slim.FieldSources = make(map[string]string, len(v.FieldSources))
			for fk, fv := range v.FieldSources {
				slim.FieldSources[fk] = fv
			}
		}
		if v.ActressSources != nil {
			slim.ActressSources = make(map[string]string, len(v.ActressSources))
			for ak, av := range v.ActressSources {
				slim.ActressSources[ak] = av
			}
		}
		if v.PosterError != nil {
			s := *v.PosterError
			slim.PosterError = &s
		}
		if v.EndedAt != nil {
			t := *v.EndedAt
			slim.EndedAt = &t
		}
		results[k] = slim
	}

	files := make([]string, len(job.Files))
	copy(files, job.Files)

	excluded := make(map[string]bool, len(job.Excluded))
	for k, v := range job.Excluded {
		excluded[k] = v
	}

	fileMatchInfo := make(map[string]FileMatchInfo, len(job.FileMatchInfo))
	for k, v := range job.FileMatchInfo {
		fileMatchInfo[k] = v
	}

	completedAt := job.CompletedAt
	if completedAt != nil {
		t := *completedAt
		completedAt = &t
	}

	organizedAt := job.OrganizedAt
	if organizedAt != nil {
		t := *organizedAt
		organizedAt = &t
	}

	revertedAt := job.RevertedAt
	if revertedAt != nil {
		t := *revertedAt
		revertedAt = &t
	}

	return &BatchJobSlim{
		ID:                          job.ID,
		Status:                      job.Status,
		TotalFiles:                  job.TotalFiles,
		Completed:                   job.Completed,
		Failed:                      job.Failed,
		Excluded:                    excluded,
		Files:                       files,
		Results:                     results,
		FileMatchInfo:               fileMatchInfo,
		Progress:                    job.Progress,
		Destination:                 job.Destination,
		TempDir:                     job.TempDir,
		StartedAt:                   job.StartedAt,
		CompletedAt:                 completedAt,
		OrganizedAt:                 organizedAt,
		RevertedAt:                  revertedAt,
		MoveToFolderOverride:        job.MoveToFolderOverride,
		RenameFolderInPlaceOverride: job.RenameFolderInPlaceOverride,
		OperationModeOverride:       job.OperationModeOverride,
		PersistError:                job.PersistError,
	}
}

// GetTempDir returns the job's temporary directory path in a thread-safe manner.
// This is the recommended way to access TempDir for concurrent safety.
func (job *BatchJob) GetTempDir() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.TempDir
}

// GetOperationModeOverride returns the operation mode override (thread-safe)
func (job *BatchJob) GetOperationModeOverride() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.OperationModeOverride
}

// SetOperationModeOverride sets the operation mode override (thread-safe)
func (job *BatchJob) SetOperationModeOverride(mode string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.OperationModeOverride = mode
}

// GetMoveToFolderOverride returns the move-to-folder override (thread-safe)
func (job *BatchJob) GetMoveToFolderOverride() *bool {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.MoveToFolderOverride == nil {
		return nil
	}
	v := *job.MoveToFolderOverride
	return &v
}

// SetMoveToFolderOverride sets the move-to-folder override (thread-safe)
func (job *BatchJob) SetMoveToFolderOverride(val *bool) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.MoveToFolderOverride = val
}

// GetRenameFolderInPlaceOverride returns the rename-in-place override (thread-safe)
func (job *BatchJob) GetRenameFolderInPlaceOverride() *bool {
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.RenameFolderInPlaceOverride == nil {
		return nil
	}
	v := *job.RenameFolderInPlaceOverride
	return &v
}

// SetRenameFolderInPlaceOverride sets the rename-in-place override (thread-safe)
func (job *BatchJob) SetRenameFolderInPlaceOverride(val *bool) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.RenameFolderInPlaceOverride = val
}

// GetDestination returns the destination path (thread-safe)
func (job *BatchJob) GetDestination() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.Destination
}

// SetDestination sets the destination path (thread-safe)
func (job *BatchJob) SetDestination(dest string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Destination = dest
}

// GetFiles returns a copy of the files list (thread-safe)
func (job *BatchJob) GetFiles() []string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	files := make([]string, len(job.Files))
	copy(files, job.Files)
	return files
}

// GetCompleted returns the completed count (thread-safe)
func (job *BatchJob) GetCompleted() int {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.Completed
}

// GetFailed returns the failed count (thread-safe)
func (job *BatchJob) GetFailed() int {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.Failed
}

// GetTotalFiles returns the total files count (thread-safe)
func (job *BatchJob) GetTotalFiles() int {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.TotalFiles
}

func (job *BatchJob) GetID() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.ID
}

func (job *BatchJob) GetPersistError() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.PersistError
}

func (job *BatchJob) SetPersistError(msg string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.PersistError = msg
}

func (job *BatchJob) GetJobStatus() JobStatus {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.Status
}

// TemplateEngine returns the shared template engine (thread-safe, read-only after construction)
// Lazily initializes if nil (for tests that create BatchJob directly)
func (job *BatchJob) TemplateEngine() *template.Engine {
	job.mu.RLock()
	eng := job.templateEngine
	job.mu.RUnlock()
	if eng != nil {
		return eng
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.templateEngine == nil {
		job.templateEngine = template.NewEngine()
	}
	return job.templateEngine
}
