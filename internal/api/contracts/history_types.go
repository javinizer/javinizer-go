package contracts

import "github.com/javinizer/javinizer-go/internal/models"

// JobListItem represents a job in the history-oriented listing (HIST-01)
type JobListItem struct {
	ID             string           `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status         models.JobStatus `json:"status" example:"organized"`
	TotalFiles     int              `json:"total_files" example:"10"`
	Completed      int              `json:"completed" example:"9"`
	Failed         int              `json:"failed" example:"1"`
	OperationCount int64            `json:"operation_count" example:"10"`
	RevertedCount  int64            `json:"reverted_count,omitempty" example:"7"`
	Progress       float64          `json:"progress" example:"0.9"`
	Destination    string           `json:"destination" example:"/path/to/output"`
	StartedAt      string           `json:"started_at" example:"2026-04-12T10:00:00Z"`
	CompletedAt    *string          `json:"completed_at,omitempty" example:"2026-04-12T10:05:00Z"`
	OrganizedAt    *string          `json:"organized_at,omitempty" example:"2026-04-12T10:05:00Z"`
	RevertedAt     *string          `json:"reverted_at,omitempty" example:"2026-04-12T11:00:00Z"`
}

// JobListResponse is the response for listing jobs
type JobListResponse struct {
	Jobs []JobListItem `json:"jobs"`
}

// OperationItem represents a single BatchFileOperation in API responses (HIST-02)
type OperationItem struct {
	ID             uint                     `json:"id" example:"1"`
	MovieID        string                   `json:"movie_id" example:"ABC-123"`
	OriginalPath   string                   `json:"original_path" example:"/source/ABC-123.mp4"`
	NewPath        string                   `json:"new_path" example:"/dest/ABC-123 [Studio]/ABC-123.mp4"`
	OperationType  models.OperationTypeEnum `json:"operation_type" example:"move"`
	RevertStatus   models.RevertStatusEnum  `json:"revert_status" example:"pending"`
	RevertedAt     *string                  `json:"reverted_at,omitempty" example:"2026-04-12T11:00:00Z"`
	InPlaceRenamed bool                     `json:"in_place_renamed" example:"false"`
	CreatedAt      string                   `json:"created_at" example:"2026-04-12T10:05:00Z"`
}

// OperationListResponse is the response for listing operations for a job
type OperationListResponse struct {
	JobID      string           `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	JobStatus  models.JobStatus `json:"job_status" example:"organized"`
	Operations []OperationItem  `json:"operations"`
	Total      int64            `json:"total" example:"10"`
}

// RevertResultResponse represents the result of a revert operation
type RevertResultResponse struct {
	JobID     string            `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status    models.JobStatus  `json:"status" example:"reverted"`
	Total     int               `json:"total" example:"10"`
	Succeeded int               `json:"succeeded" example:"9"`
	Skipped   int               `json:"skipped" example:"1"`
	Failed    int               `json:"failed" example:"1"`
	Errors    []RevertFileError `json:"errors,omitempty"`
}

// RevertFileError represents a per-file result during revert (includes skipped and failed)
type RevertFileError struct {
	OperationID  uint                     `json:"operation_id" example:"5"`
	MovieID      string                   `json:"movie_id" example:"ABC-123"`
	OriginalPath string                   `json:"original_path" example:"/source/ABC-123.mp4"`
	NewPath      string                   `json:"new_path" example:"/dest/ABC-123 [Studio]/ABC-123.mp4"`
	Error        string                   `json:"error" example:"file not found"`
	Outcome      models.RevertOutcomeEnum `json:"outcome,omitempty" example:"skipped"`
	Reason       models.RevertReasonEnum  `json:"reason,omitempty" example:"anchor_missing"`
}

// RevertCheckResponse represents overlap detection for a batch revert (D-07)
type RevertCheckResponse struct {
	JobID              string        `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	OverlappingBatches []OverlapInfo `json:"overlapping_batches"`
}

// OverlapInfo represents a later batch with path overlaps (D-07)
type OverlapInfo struct {
	JobID          string `json:"job_id" example:"660e8400-e29b-41d4-a716-446655440001"`
	CreatedAt      string `json:"created_at" example:"2026-04-12T12:00:00Z"`
	OperationCount int    `json:"operation_count" example:"3"`
}
