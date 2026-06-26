package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/operationmode"
)

type JobStatus string

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

func (s JobStatus) MarshalJSON() ([]byte, error)  { return MarshalStringEnum(string(s)) }
func (s *JobStatus) UnmarshalJSON(b []byte) error { return UnmarshalStringEnum((*string)(s), b) }

func (s *JobStatus) Scan(value any) error        { return ScanStringEnum((*string)(s), value) }
func (s JobStatus) Value() (driver.Value, error) { return StringEnumValue(string(s)) }

type Job struct {
	ID                    string                      `json:"id" gorm:"primaryKey"`
	Status                JobStatus                   `json:"status" gorm:"index"`
	TotalFiles            int                         `json:"total_files"`
	Completed             int                         `json:"completed"`
	Failed                int                         `json:"failed"`
	Progress              float64                     `json:"progress"`
	Destination           string                      `json:"destination"`
	TempDir               string                      `json:"temp_dir" gorm:"default:'data/temp'"`
	OperationModeOverride operationmode.OperationMode `json:"operation_mode_override,omitempty"`
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

func (Job) TableName() string {
	return "jobs"
}

func (j *Job) ParseResults(v any) error {
	if j.Results == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(j.Results), v); err != nil {
		return fmt.Errorf("failed to parse results for job %s: %w", j.ID, err)
	}
	return nil
}

func (j *Job) ParseExcluded(v any) error {
	if j.Excluded == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(j.Excluded), v); err != nil {
		return fmt.Errorf("failed to parse excluded for job %s: %w", j.ID, err)
	}
	return nil
}

func (j *Job) ParseFiles(v any) error {
	if j.Files == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(j.Files), v); err != nil {
		return fmt.Errorf("failed to parse files for job %s: %w", j.ID, err)
	}
	return nil
}
