package models

import (
	"encoding/hex"
	"strings"
)

// PrimaryUndo is the durable copy-first inverse of a cross-volume primary
// move. Every path is fixed before use. A digest authenticates content only;
// the strong object identities are required separately for ownership.
// Absent on legacy rows. Retained after completion so a failed status write
// can resume without the already-consumed library anchor.
type PrimaryUndo struct {
	Token            string              `json:"token"`
	State            string              `json:"state"`
	Source           string              `json:"source"`
	Destination      string              `json:"destination"`
	Stage            string              `json:"stage"`
	CleanupFrom      string              `json:"cleanup_from,omitempty"`
	Cleanup          string              `json:"cleanup,omitempty"`
	SourceIdentity   PrimaryUndoIdentity `json:"source_identity"`
	StageIdentity    PrimaryUndoIdentity `json:"stage_identity"`
	SupersededStages []PrimaryUndoStage  `json:"superseded_stages,omitempty"`
	SHA256           string              `json:"sha256,omitempty"`
}

// PrimaryUndoIdentity never falls back to size, time, or equal bytes when a
// native object identity is unavailable. Device/Inode are POSIX device/inode.
// On Windows Device and FileID128 carry the full FILE_ID_INFO volume serial
// and 128-bit file ID. Strong is explicit because zero kernel IDs are valid.
type PrimaryUndoIdentity struct {
	Strong    bool   `json:"strong"`
	Device    uint64 `json:"device"`
	Inode     uint64 `json:"inode"`
	FileID128 string `json:"file_id_128,omitempty"`
	Size      int64  `json:"size"`
	ModTime   int64  `json:"mod_time"`
	Mode      uint32 `json:"mode"`
}

// PrimaryUndoStage retains an admitted, superseded copy generation until its
// own journaled no-replace cleanup finishes. No retry opens it for writing.
type PrimaryUndoStage struct {
	Path        string              `json:"path"`
	Identity    PrimaryUndoIdentity `json:"identity"`
	CleanupFrom string              `json:"cleanup_from,omitempty"`
	Cleanup     string              `json:"cleanup,omitempty"`
}

// PrimaryUndoNeedsRetention is deliberately conservative: retention must never
// erase incomplete or malformed recovery ownership, even without a sweep hook.
func PrimaryUndoNeedsRetention(op BatchFileOperation) bool {
	ledger, err := ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return true
	}
	j := ledger.PrimaryUndo
	if j == nil {
		return false
	}
	token, tokenErr := hex.DecodeString(j.Token)
	digest, digestErr := hex.DecodeString(j.SHA256)
	return ledger.PrimaryResident || j.State != "done" || len(j.SupersededStages) != 0 ||
		tokenErr != nil || len(token) != 16 || digestErr != nil || len(digest) != 32 ||
		!j.SourceIdentity.Strong || !j.StageIdentity.Strong ||
		j.Source == "" || j.Destination == "" || j.Source == j.Destination ||
		j.Source != op.NewPath || j.Destination != op.OriginalPath || op.InPlaceRenamed || op.OperationType != OperationTypeMove ||
		!primaryUndoRetainedStage(j) || j.CleanupFrom != j.Source ||
		!primaryUndoRetainedTokenPath(j.Cleanup, j.Source+".undo-remove.")
}

func primaryUndoRetainedStage(j *PrimaryUndo) bool {
	prefix := j.Destination + ".undo." + j.Token
	return j.Stage == prefix || primaryUndoRetainedTokenPath(j.Stage, prefix+".")
}
func primaryUndoRetainedTokenPath(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	token, err := hex.DecodeString(strings.TrimPrefix(path, prefix))
	return err == nil && len(token) == 16
}
