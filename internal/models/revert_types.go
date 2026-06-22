package models

// GeneratedFilesJSON is the JSON structure stored in BatchFileOperation.GeneratedFiles.
// Per ADR-0034: moved from both workflow/revert_log.go and history/reverter.go to
// eliminate duplicate type definitions with drift risk. The JSON schema is a
// persistence contract — one source of truth.
type GeneratedFilesJSON struct {
	Delete   []string   `json:"delete,omitempty"`    // Files to delete on revert (NFO, images, screenshots)
	MoveBack []FileMove `json:"move_back,omitempty"` // Files to move back on revert (subtitles)
}

// FileMove represents a file that was moved during organize and should be moved back on revert.
type FileMove struct {
	OriginalPath string `json:"original_path"` // Where the file was before organize
	NewPath      string `json:"new_path"`      // Where the file is after organize
}
