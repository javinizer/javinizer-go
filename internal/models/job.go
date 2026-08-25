package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// JobStatus is the lifecycle state of a background job.
type JobStatus string

// JobStatus values are the possible lifecycle states of a background job.
const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
	JobStatusOrganized JobStatus = "organized"
	JobStatusReverted  JobStatus = "reverted"
)

func (s JobStatus) String() string { return string(s) }

// MarshalJSON implements json.Marshaler for JobStatus.
func (s JobStatus) MarshalJSON() ([]byte, error) { return MarshalStringEnum(string(s)) }

// UnmarshalJSON implements json.Unmarshaler for JobStatus.
func (s *JobStatus) UnmarshalJSON(b []byte) error { return UnmarshalStringEnum((*string)(s), b) }

// Scan implements sql.Scanner for JobStatus, storing the column value as a string.
func (s *JobStatus) Scan(value any) error { return ScanStringEnum((*string)(s), value) }

// Value implements driver.Valuer for JobStatus, returning the status as a string.
func (s JobStatus) Value() (driver.Value, error) { return StringEnumValue(string(s)) }

// Job represents a background processing job persisted in the database.
type Job struct {
	ID                    string                      `json:"id" gorm:"primaryKey"`
	PruneVersion          uint64                      `json:"-" gorm:"not null;default:0"`
	EnvelopeGeneration    uint64                      `json:"-" gorm:"not null;default:0"`
	Status                JobStatus                   `json:"status" gorm:"index"`
	TotalFiles            int                         `json:"total_files"`
	Completed             int                         `json:"completed"`
	Failed                int                         `json:"failed"`
	Progress              float64                     `json:"progress"`
	Destination           string                      `json:"destination"`
	TempDir               string                      `json:"temp_dir" gorm:"default:'data/temp'"`
	OperationModeOverride operationmode.OperationMode `json:"operation_mode_override,omitempty"`
	ApplyPlan             *string                     `json:"apply_plan,omitempty" gorm:"type:text"`
	Files                 string                      `json:"files" gorm:"type:text"`
	Results               string                      `json:"results" gorm:"type:text"`
	Excluded              string                      `json:"excluded" gorm:"type:text"`
	FileMatchInfo         string                      `json:"file_match_info" gorm:"type:text"`
	StartedAt             time.Time                   `json:"started_at" gorm:"index"`
	CompletedAt           *time.Time                  `json:"completed_at,omitempty"`
	OrganizedAt           *time.Time                  `json:"organized_at,omitempty"`
	RevertedAt            *time.Time                  `json:"reverted_at,omitempty"`
	Update                bool                        `json:"update" gorm:"column:update;default:false"`
}

// TableName overrides the default GORM table name for Job, returning "jobs".
func (Job) TableName() string {
	return "jobs"
}

// ParseResults unmarshals the job's JSON-encoded Results field into v.
func (j *Job) ParseResults(v any) error {
	if j.Results == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(j.Results), v); err != nil {
		return fmt.Errorf("failed to parse results for job %s: %w", j.ID, err)
	}
	return nil
}

// ParseExcluded unmarshals the job's JSON-encoded Excluded field into v.
func (j *Job) ParseExcluded(v any) error {
	if j.Excluded == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(j.Excluded), v); err != nil {
		return fmt.Errorf("failed to parse excluded for job %s: %w", j.ID, err)
	}
	return nil
}

// ParseFiles unmarshals the job's JSON-encoded Files field into v.
func (j *Job) ParseFiles(v any) error {
	if j.Files == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(j.Files), v); err != nil {
		return fmt.Errorf("failed to parse files for job %s: %w", j.ID, err)
	}
	return nil
}
