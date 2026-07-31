package models

import "time"

const (
	// ActressSyncJobPending ...
	ActressSyncJobPending = "pending"
	// ActressSyncJobRunning ...
	ActressSyncJobRunning = "running"
	// ActressSyncJobCompleted ...
	ActressSyncJobCompleted = "completed"
	// ActressSyncJobCancelled ...
	ActressSyncJobCancelled = "cancelled"

	// ActressSyncTaskPending ...
	ActressSyncTaskPending = "pending"
	// ActressSyncTaskRunning ...
	ActressSyncTaskRunning = "running"
	// ActressSyncTaskCompleted ...
	ActressSyncTaskCompleted = "completed"
	// ActressSyncTaskSkipped ...
	ActressSyncTaskSkipped = "skipped"
	// ActressSyncTaskConflict ...
	ActressSyncTaskConflict = "conflict"
	// ActressSyncTaskFailed ...
	ActressSyncTaskFailed = "failed"
	// ActressSyncTaskCancelled ...
	ActressSyncTaskCancelled = "cancelled"
)

// ActressSyncJob ...
type ActressSyncJob struct {
	ID              string     `json:"id" gorm:"primaryKey;size:36"`
	Status          string     `json:"status"`
	Scope           string     `json:"scope"`
	TotalTasks      int        `json:"total_tasks"`
	Completed       int        `json:"completed"`
	Updated         int        `json:"updated"`
	Warnings        int        `json:"warnings"`
	Skipped         int        `json:"skipped"`
	Conflicts       int        `json:"conflicts"`
	Failed          int        `json:"failed"`
	Cancelled       int        `json:"cancelled"`
	CancelRequested bool       `json:"cancel_requested"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// TableName ...
func (ActressSyncJob) TableName() string { return "actress_sync_jobs" }

// ActressSyncTask ...
type ActressSyncTask struct {
	ID             string     `json:"id" gorm:"primaryKey;size:36"`
	JobID          string     `json:"job_id"`
	ActressID      *uint      `json:"actress_id,omitempty"`
	Label          string     `json:"label"`
	DedupeKey      string     `json:"dedupe_key"`
	Status         string     `json:"status"`
	Stage          string     `json:"stage"`
	Outcome        string     `json:"outcome,omitempty"`
	Messages       []string   `json:"messages" gorm:"serializer:json"`
	UpdatedFields  []string   `json:"updated_fields" gorm:"serializer:json"`
	Warning        string     `json:"warning,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	LeaseOwner     string     `json:"-"`
	LeaseToken     string     `json:"-"`
	HeartbeatAt    *time.Time `json:"heartbeat_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	Attempts       int        `json:"attempts"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// TableName ...
func (ActressSyncTask) TableName() string { return "actress_sync_tasks" }
