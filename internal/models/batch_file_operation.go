package models

import (
	"database/sql/driver"
	"time"
)

// ---------------------------------------------------------------------------
// Typed enums — type X string for GORM compatibility with existing string columns
// ---------------------------------------------------------------------------

// OperationTypeEnum represents the type of file operation performed.
type OperationTypeEnum string

// OperationType values are the supported file operation kinds for a batch entry.
const (
	OperationTypeMove     OperationTypeEnum = "move"
	OperationTypeCopy     OperationTypeEnum = "copy"
	OperationTypeHardlink OperationTypeEnum = "hardlink"
	OperationTypeSymlink  OperationTypeEnum = "symlink"
	OperationTypeUpdate   OperationTypeEnum = "update" // update-mode organize (NFO overwrite, no file move) per HIST-05
)

func (e OperationTypeEnum) String() string { return string(e) }

// MarshalJSON implements json.Marshaler for OperationTypeEnum.
func (e OperationTypeEnum) MarshalJSON() ([]byte, error) { return MarshalStringEnum(string(e)) }

// UnmarshalJSON implements json.Unmarshaler for OperationTypeEnum.
func (e *OperationTypeEnum) UnmarshalJSON(b []byte) error {
	return UnmarshalStringEnum((*string)(e), b)
}

// Scan implements sql.Scanner for OperationTypeEnum.
func (e *OperationTypeEnum) Scan(value any) error { return ScanStringEnum((*string)(e), value) }

// Value implements driver.Valuer for OperationTypeEnum.
func (e OperationTypeEnum) Value() (driver.Value, error) { return StringEnumValue(string(e)) }

// RevertStatusEnum represents the revert status of a batch file operation.
type RevertStatusEnum string

// RevertStatus values are the revert states of a batch file operation.
const (
	RevertStatusApplied  RevertStatusEnum = "applied" // Renamed from "pending" — D-01
	RevertStatusReverted RevertStatusEnum = "reverted"
	RevertStatusFailed   RevertStatusEnum = "failed"
	// RevertStatusNoOp is the completed-noop terminal state (codex P2, PR
	// #241 F2; generalized batch-2 F1/F2): the operation's apply mutated
	// NOTHING on the filesystem — an authorized intra-batch duplicate skip
	// (organizer's DuplicateSkipped) whose OrganizeResult.NewPath names the
	// batch winner's shared destination for display only, or a pre-publication
	// organize terminal (plan rejection/conflict, context abort, or a strategy
	// execute failure before any publish — e.g. ForceUpdate with a vanished
	// source) whose intent paths likewise belong to nobody's revert. Marking
	// the row noop instead of leaving it applied/failed-with-empty-NewPath
	// keeps the reverter from probing a "" anchor forever (anchor_missing) and
	// lets the batch report fully reverted once its real rows unwind;
	// nothing-to-revert rows are excluded from revert selection exactly like
	// reverted rows.
	RevertStatusNoOp RevertStatusEnum = "noop"
)

func (e RevertStatusEnum) String() string { return string(e) }

// MarshalJSON implements json.Marshaler for RevertStatusEnum.
func (e RevertStatusEnum) MarshalJSON() ([]byte, error) { return MarshalStringEnum(string(e)) }

// UnmarshalJSON implements json.Unmarshaler for RevertStatusEnum.
func (e *RevertStatusEnum) UnmarshalJSON(b []byte) error { return UnmarshalStringEnum((*string)(e), b) }

// Scan implements sql.Scanner for RevertStatusEnum.
func (e *RevertStatusEnum) Scan(value any) error { return ScanStringEnum((*string)(e), value) }

// Value implements driver.Valuer for RevertStatusEnum.
func (e RevertStatusEnum) Value() (driver.Value, error) { return StringEnumValue(string(e)) }

// RevertOutcomeEnum represents the per-operation result of a revert attempt.
type RevertOutcomeEnum string

// RevertOutcome values are the per-operation results of a revert attempt.
const (
	RevertOutcomeReverted RevertOutcomeEnum = "reverted" // Successfully reverted
	RevertOutcomeSkipped  RevertOutcomeEnum = "skipped"  // Skipped (e.g., anchor missing)
	RevertOutcomeFailed   RevertOutcomeEnum = "failed"   // Failed to revert
)

func (e RevertOutcomeEnum) String() string { return string(e) }

// MarshalJSON implements json.Marshaler for RevertOutcomeEnum.
func (e RevertOutcomeEnum) MarshalJSON() ([]byte, error) { return MarshalStringEnum(string(e)) }

// UnmarshalJSON implements json.Unmarshaler for RevertOutcomeEnum.
func (e *RevertOutcomeEnum) UnmarshalJSON(b []byte) error {
	return UnmarshalStringEnum((*string)(e), b)
}

// Scan implements sql.Scanner for RevertOutcomeEnum.
func (e *RevertOutcomeEnum) Scan(value any) error { return ScanStringEnum((*string)(e), value) }

// Value implements driver.Valuer for RevertOutcomeEnum.
func (e RevertOutcomeEnum) Value() (driver.Value, error) { return StringEnumValue(string(e)) }

// RevertReasonEnum represents the reason a revert had a specific outcome.
type RevertReasonEnum string

// RevertReason values are the reasons a revert produced a specific outcome.
const (
	RevertReasonAnchorMissing          RevertReasonEnum = "anchor_missing"           // Video file missing at expected path
	RevertReasonDestinationConflict    RevertReasonEnum = "destination_conflict"     // Original path already occupied
	RevertReasonAccessDenied           RevertReasonEnum = "access_denied"            // Permission error during revert
	RevertReasonUnexpectedPathState    RevertReasonEnum = "unexpected_path_state"    // File in unexpected state
	RevertReasonNFORestoreFailed       RevertReasonEnum = "nfo_restore_failed"       // NFO write failed
	RevertReasonGeneratedCleanupFailed RevertReasonEnum = "generated_cleanup_failed" // Generated file cleanup failed
)

func (e RevertReasonEnum) String() string { return string(e) }

// MarshalJSON implements json.Marshaler for RevertReasonEnum.
func (e RevertReasonEnum) MarshalJSON() ([]byte, error) { return MarshalStringEnum(string(e)) }

// UnmarshalJSON implements json.Unmarshaler for RevertReasonEnum.
func (e *RevertReasonEnum) UnmarshalJSON(b []byte) error { return UnmarshalStringEnum((*string)(e), b) }

// Scan implements sql.Scanner for RevertReasonEnum.
func (e *RevertReasonEnum) Scan(value any) error { return ScanStringEnum((*string)(e), value) }

// Value implements driver.Valuer for RevertReasonEnum.
func (e RevertReasonEnum) Value() (driver.Value, error) { return StringEnumValue(string(e)) }

// BatchFileOperation represents per-file organize details for revert support
type BatchFileOperation struct {
	ID              uint              `json:"id" gorm:"primaryKey"`
	BatchJobID      string            `json:"batch_job_id" gorm:"not null;index:idx_bfo_batch_job_id;index:idx_bfo_batch_job_revert_status,priority:1"`
	MovieID         string            `json:"movie_id"`
	OriginalPath    string            `json:"original_path" gorm:"not null"`
	NewPath         string            `json:"new_path" gorm:"not null"`
	OperationType   OperationTypeEnum `json:"operation_type" gorm:"not null;default:move"`
	NFOSnapshot     string            `json:"nfo_snapshot" gorm:"type:text"`
	NFOPath         string            `json:"nfo_path" gorm:"type:text"`
	GeneratedFiles  string            `json:"generated_files" gorm:"type:text"`
	RevertStatus    RevertStatusEnum  `json:"revert_status" gorm:"not null;default:applied;index:idx_bfo_batch_job_revert_status,priority:2"`
	RevertedAt      *time.Time        `json:"reverted_at"`
	InPlaceRenamed  bool              `json:"in_place_renamed" gorm:"not null;default:false"`
	OriginalDirPath string            `json:"original_dir_path"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// TableName specifies the table name for BatchFileOperation
func (BatchFileOperation) TableName() string {
	return "batch_file_operations"
}
