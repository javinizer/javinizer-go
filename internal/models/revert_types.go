package models

import (
	"encoding/json"
	"strings"
)

// GeneratedFilesJSON is the JSON structure stored in BatchFileOperation.GeneratedFiles.
// Moved from both workflow/revert_log.go and history/reverter.go to
// eliminate duplicate type definitions with drift risk. The JSON schema is a
// persistence contract — one source of truth.
type GeneratedFilesJSON struct {
	Delete       []string           `json:"delete,omitempty"`       // Files to delete on revert (NFO, images, screenshots)
	MoveBack     []FileMove         `json:"move_back,omitempty"`    // Files to move back on revert (subtitles)
	Replacements []ReplacementEntry `json:"replacements,omitempty"` // Overwritten byte pairs journaled before the replace landed (P3)
	Roots        []string           `json:"roots,omitempty"`        // Destination roots seeded at Begin — sweeper discovery independent of any later journal (P3 R3-3)
}

// ReplacementEntry journals one destructive media overwrite: the destination's
// pre-existing bytes were moved aside to Backup under the per-destination lock
// BEFORE the new bytes installed (internal/downloader installOverwriting).
// DestSeq is the restart-persistent per-destination sequence assigned inside
// that lock — revert restores destinations in strict reverse DestSeq order so
// stacked or cross-operation chains unwind in true chronological reverse,
// independent of row ids, begin order, or job boundaries.
type ReplacementEntry struct {
	Destination string `json:"destination"` // Where the new bytes landed
	Backup      string `json:"backup"`      // Where the pre-existing bytes were set aside
	DestSeq     int64  `json:"dest_seq"`    // Per-destination monotonic sequence (1-based)
	// Installed flips true when the downloader confirms the replace landed.
	// An armed-but-unconfirmed entry + a missing destination is the ONLY state
	// in which the pre-install crash window is provable (P3 R4-3): a confirmed
	// entry with a missing destination instead means someone deleted the
	// media afterwards, and restoring it would resurrect deleted artwork.
	Installed bool `json:"installed,omitempty"`
}

// FileMove represents a file that was moved during organize and should be moved back on revert.
type FileMove struct {
	OriginalPath string `json:"original_path"` // Where the file was before organize
	NewPath      string `json:"new_path"`      // Where the file is after organize
}

// ParseGeneratedFiles decodes the persisted ledger blob. Empty content decodes
// to the zero value (legacy rows created before the journal existed);
// malformed JSON surfaces as an error so callers can choose tolerance.
func ParseGeneratedFiles(raw string) (GeneratedFilesJSON, error) {
	var gf GeneratedFilesJSON
	if strings.TrimSpace(raw) == "" {
		return gf, nil
	}
	if err := json.Unmarshal([]byte(raw), &gf); err != nil {
		return GeneratedFilesJSON{}, err
	}
	return gf, nil
}
