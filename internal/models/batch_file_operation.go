package models

import "time"

// Revert status constants for BatchFileOperation
const (
	RevertStatusApplied  = "applied" // Renamed from "pending" — D-01
	RevertStatusReverted = "reverted"
	RevertStatusFailed   = "failed"
)

// RevertOutcome constants for per-operation result tracking — D-06
const (
	RevertOutcomeReverted = "reverted" // Successfully reverted
	RevertOutcomeSkipped  = "skipped"  // Skipped (e.g., anchor missing)
	RevertOutcomeFailed   = "failed"   // Failed to revert
)

// RevertReason constants for why a revert had a specific outcome — D-06
const (
	RevertReasonAnchorMissing          = "anchor_missing"           // Video file missing at expected path
	RevertReasonDestinationConflict    = "destination_conflict"     // Original path already occupied
	RevertReasonAccessDenied           = "access_denied"            // Permission error during revert
	RevertReasonUnexpectedPathState    = "unexpected_path_state"    // File in unexpected state
	RevertReasonNFORestoreFailed       = "nfo_restore_failed"       // NFO write failed
	RevertReasonGeneratedCleanupFailed = "generated_cleanup_failed" // Generated file cleanup failed
)

// Operation type constants for BatchFileOperation
const (
	OperationTypeMove     = "move"
	OperationTypeCopy     = "copy"
	OperationTypeHardlink = "hardlink"
	OperationTypeSymlink  = "symlink"
	OperationTypeUpdate   = "update" // update-mode organize (NFO overwrite, no file move) per HIST-05
)

// BatchFileOperation represents per-file organize details for revert support
type BatchFileOperation struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	BatchJobID      string     `json:"batch_job_id" gorm:"not null;index:idx_bfo_batch_job_id;index:idx_bfo_batch_job_revert_status,priority:1"`
	MovieID         string     `json:"movie_id"`
	OriginalPath    string     `json:"original_path" gorm:"not null"`
	NewPath         string     `json:"new_path" gorm:"not null"`
	OperationType   string     `json:"operation_type" gorm:"not null;default:move"`
	NFOSnapshot     string     `json:"nfo_snapshot" gorm:"type:text"`
	NFOPath         string     `json:"nfo_path" gorm:"type:text"`
	GeneratedFiles  string     `json:"generated_files" gorm:"type:text"`
	RevertStatus    string     `json:"revert_status" gorm:"not null;default:applied;index:idx_bfo_batch_job_revert_status,priority:2"`
	RevertedAt      *time.Time `json:"reverted_at"`
	InPlaceRenamed  bool       `json:"in_place_renamed" gorm:"not null;default:false"`
	OriginalDirPath string     `json:"original_dir_path"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// TableName specifies the table name for BatchFileOperation
func (BatchFileOperation) TableName() string {
	return "batch_file_operations"
}
