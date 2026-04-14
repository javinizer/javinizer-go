package models

import "time"

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

type Job struct {
	ID            string     `json:"id" gorm:"primaryKey"`
	Status        string     `json:"status" gorm:"index"`
	TotalFiles    int        `json:"total_files"`
	Completed     int        `json:"completed"`
	Failed        int        `json:"failed"`
	Progress      float64    `json:"progress"`
	Destination   string     `json:"destination"`
	TempDir       string     `json:"temp_dir" gorm:"default:'data/temp'"`
	Files         string     `json:"files" gorm:"type:text"`
	Results       string     `json:"results" gorm:"type:text"`
	Excluded      string     `json:"excluded" gorm:"type:text"`
	FileMatchInfo string     `json:"file_match_info" gorm:"type:text"`
	StartedAt     time.Time  `json:"started_at" gorm:"index"`
	CompletedAt   *time.Time `json:"completed_at"`
	OrganizedAt   *time.Time `json:"organized_at"`
	RevertedAt    *time.Time `json:"reverted_at"`
}

func (Job) TableName() string {
	return "jobs"
}
