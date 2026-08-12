package contracts

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ActressMergePreviewRequest represents a merge preview request for two actresses.
type ActressMergePreviewRequest struct {
	TargetID uint `json:"target_id" binding:"required" example:"12"`
	SourceID uint `json:"source_id" binding:"required" example:"34"`
}

// ActressMergeConflict represents a conflicting field between target and source actress.
type ActressMergeConflict struct {
	Field             string `json:"field" example:"japanese_name"`
	TargetValue       any    `json:"target_value,omitempty"`
	SourceValue       any    `json:"source_value,omitempty"`
	DefaultResolution string `json:"default_resolution" example:"target"`
}

// ActressMergePreviewResponse represents a preview of an actress merge operation.
type ActressMergePreviewResponse struct {
	Target             models.Actress         `json:"target"`
	Source             models.Actress         `json:"source"`
	ProposedMerged     models.Actress         `json:"proposed_merged"`
	Conflicts          []ActressMergeConflict `json:"conflicts"`
	DefaultResolutions map[string]string      `json:"default_resolutions"`
	TargetUpdatedAt    time.Time              `json:"target_updated_at"`
	SourceUpdatedAt    time.Time              `json:"source_updated_at"`
}

// ActressMergeRequest represents a merge request with conflict resolutions.
type ActressMergeRequest struct {
	TargetID    uint              `json:"target_id" binding:"required" example:"12"`
	SourceID    uint              `json:"source_id" binding:"required" example:"34"`
	Resolutions map[string]string `json:"resolutions,omitempty" example:"japanese_name:source,dmm_id:target"`
	// TargetUpdatedAt/SourceUpdatedAt fence the merge against concurrent edits
	// observed in a merge preview. They are optional for backward compatibility:
	// omitting them performs an unfenced merge (pre-versioning behavior).
	TargetUpdatedAt time.Time `json:"target_updated_at,omitempty"`
	SourceUpdatedAt time.Time `json:"source_updated_at,omitempty"`
}

// ActressMergeResponse represents a completed actress merge operation.
type ActressMergeResponse struct {
	MergedActress     models.Actress `json:"merged_actress"`
	MergedFromID      uint           `json:"merged_from_id" example:"34"`
	UpdatedMovies     int            `json:"updated_movies" example:"27"`
	ConflictsResolved int            `json:"conflicts_resolved" example:"3"`
	AliasesAdded      int            `json:"aliases_added" example:"5"`
}
