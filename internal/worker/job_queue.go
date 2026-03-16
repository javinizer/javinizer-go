package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// JobStatus represents the status of a job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// FileResult represents the result of processing a single file
type FileResult struct {
	FilePath       string            `json:"file_path"`
	MovieID        string            `json:"movie_id"`
	Status         JobStatus         `json:"status"`
	Error          string            `json:"error,omitempty"`
	PosterError    *string           `json:"poster_error,omitempty"`    // Optional error from poster generation
	FieldSources   map[string]string `json:"field_sources,omitempty"`   // Field -> scraper/NFO source
	ActressSources map[string]string `json:"actress_sources,omitempty"` // Actress-key -> scraper/NFO source
	Data           interface{}       `json:"data,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	EndedAt        *time.Time        `json:"ended_at,omitempty"`
	IsMultiPart    bool              `json:"is_multi_part,omitempty"`
	PartNumber     int               `json:"part_number,omitempty"`
	PartSuffix     string            `json:"part_suffix,omitempty"`
}

// BatchJob represents a batch processing job
type BatchJob struct {
	ID          string                 `json:"id"`
	Status      JobStatus              `json:"status"`
	TotalFiles  int                    `json:"total_files"`
	Completed   int                    `json:"completed"`
	Failed      int                    `json:"failed"`
	Excluded    map[string]bool        `json:"excluded"` // Files excluded from organization (keyed by file path)
	Files       []string               `json:"files"`
	Results     map[string]*FileResult `json:"results"` // keyed by file path
	Progress    float64                `json:"progress"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	CancelFunc  context.CancelFunc     `json:"-"`
	Done        chan struct{}          `json:"-"` // closed when job fully finishes
	mu          sync.RWMutex           `json:"-"`
}

// JobQueue manages batch jobs
type JobQueue struct {
	jobs map[string]*BatchJob
	mu   sync.RWMutex
}

// NewJobQueue creates a new job queue
func NewJobQueue() *JobQueue {
	return &JobQueue{
		jobs: make(map[string]*BatchJob),
	}
}

// CreateJob creates a new batch job
func (jq *JobQueue) CreateJob(files []string) *BatchJob {
	job := &BatchJob{
		ID:         uuid.New().String(),
		Status:     JobStatusPending,
		TotalFiles: len(files),
		Files:      files,
		Results:    make(map[string]*FileResult),
		Excluded:   make(map[string]bool),
		Done:       make(chan struct{}),
		StartedAt:  time.Now(),
	}

	jq.mu.Lock()
	jq.jobs[job.ID] = job
	jq.mu.Unlock()

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
func (jq *JobQueue) DeleteJob(id string) {
	// Get job without holding queue lock to avoid lock ordering issues
	jq.mu.RLock()
	job, ok := jq.jobs[id]
	jq.mu.RUnlock()

	if ok {
		snap := job.GetStatus()
		if snap.Status == JobStatusRunning || snap.Status == JobStatusPending {
			job.Cancel()
		}

		// Wait for job to fully finish using Done channel
		select {
		case <-job.Done:
			// Job finished, safe to cleanup
		case <-time.After(5 * time.Second):
			logging.Warnf("DeleteJob: timed out waiting for job %s to finish, proceeding with cleanup", id)
		}
	}

	// Now safe to clean up filesystem and remove from map
	jq.mu.Lock()
	defer jq.mu.Unlock()

	// Clean up temp posters for this job (data/temp/posters/{jobID}/)
	tempPosterDir := filepath.Join("data", "temp", "posters", id)
	if err := os.RemoveAll(tempPosterDir); err != nil {
		logging.Warnf("Failed to clean up temp posters for job %s: %v", id, err)
	}

	delete(jq.jobs, id)
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

	job.Results[filePath] = result

	// Update counters
	completed := 0
	failed := 0
	for _, r := range job.Results {
		switch r.Status {
		case JobStatusCompleted:
			completed++
		case JobStatusFailed:
			failed++
		}
	}
	job.Completed = completed
	job.Failed = failed

	// Guard against division by zero
	if job.TotalFiles == 0 {
		job.Progress = 100 // Empty job is considered complete
	} else {
		job.Progress = float64(completed+failed) / float64(job.TotalFiles) * 100
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

	// Write back the updated result
	job.Results[filePath] = updated

	// Recalculate counters (same logic as UpdateFileResult)
	completed := 0
	failed := 0
	for _, r := range job.Results {
		switch r.Status {
		case JobStatusCompleted:
			completed++
		case JobStatusFailed:
			failed++
		}
	}
	job.Completed = completed
	job.Failed = failed

	if job.TotalFiles == 0 {
		job.Progress = 100
	} else {
		job.Progress = float64(completed+failed) / float64(job.TotalFiles) * 100
	}

	return nil
}

// MarkStarted marks the job as started
func (job *BatchJob) MarkStarted() {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Status = JobStatusRunning
	job.StartedAt = time.Now()
	job.CompletedAt = nil
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
			results[k] = nil
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

	completedAt := job.CompletedAt
	if completedAt != nil {
		t := *completedAt
		completedAt = &t
	}

	return &BatchJob{
		ID:          job.ID,
		Status:      job.Status,
		TotalFiles:  job.TotalFiles,
		Completed:   job.Completed,
		Failed:      job.Failed,
		Excluded:    excluded,
		Files:       files,
		Results:     results,
		Progress:    job.Progress,
		StartedAt:   job.StartedAt,
		CompletedAt: completedAt,
	}
}
