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
	// RestorePending marks a history/sweep restore whose destination bytes are
	// in place but whose backup cleanup could not yet complete. It is separate
	// from Installed so downloader crash-window semantics remain unchanged.
	RestorePending bool `json:"restore_pending,omitempty"`
	// RestorePendingKind discriminates WHICH retry semantics RestorePending
	// authorizes (wave-19, codex P2 PR#215). "" is the legacy clean kind: the
	// backup name still holds THIS operation's own set-aside bytes, so the
	// pending retry removes that path and consumes the entry. "rearm_refused"
	// records a refused no-replace re-arm: the backup name is foreign-occupied
	// or absent (unowned either way), the destination bytes are certified in
	// place, and the pending retry consumes the entry WITHOUT any backup-path
	// operation — a path existence check would fail forever on the absent
	// name, and a removal would delete a foreign file.
	RestorePendingKind string `json:"restore_pending_kind,omitempty"`
	// BackupSize/BackupModUnix bind later backup-path removals to the OWNED
	// object (wave-25, codex P3 PR#215): the downloader stamps the set-aside
	// backup's size and mtime (Unix seconds) into the entry at journal-write
	// time, and history's removal gate refuses to unlink a same-pathname
	// occupant whose facts differ — deleting a directory writer's swapped-in
	// file would both destroy foreign bytes and consume the only journal
	// record of the restore. Dev/inode are deliberately NOT journaled: the
	// consumption-failure re-arm compensation republishes the backup under a
	// FRESH inode while preserving size and mtime byte-faithful, so a
	// journaled inode would misjudge the owner's own re-armed backup as
	// foreign and wedge the pending retry forever.
	//
	// BackupModUnix == 0 means the entry predates the stamp (or the capture
	// failed): such legacy entries fall back to pathname-only removal with a
	// documented residual window. Both fields stay omitempty so pre-wave-25
	// blobs serialize byte-identically.
	BackupSize    int64 `json:"backup_size,omitempty"`
	BackupModUnix int64 `json:"backup_mod_unix,omitempty"`
	// BackupSHA256 (wave-63, codex P2 PR#215) is the hex sha256 of the set-aside
	// backup bytes captured at arm time. BackupSize/BackupModUnix alone are
	// forgeable (same length + a coerced unix-second mtime impersonates the
	// owned set-aside); the sha256 binds the restore copy to the exact bytes
	// (mismatch refuses before any byte reaches dest). An empty value (entries
	// armed before this wave, or a capture failure) keeps the wave-25
	// size+mtime+dev/ino posture. Re-arms copy the restored dest bytes (== the
	// original) back to the backup name, so the arm-time sha256 stays valid.
	BackupSHA256 string `json:"backup_sha256,omitempty"`
}

// ReplacementBackupFacts are the backup object's identity facts captured by
// the downloader at set-aside time and carried into the journal entry
// (wave-25, codex P3 PR#215). ModUnix == 0 marks "facts unavailable" — the
// recorded entry then reads as legacy and the removal gate falls back to the
// pathname-only posture documented on the entry fields.
type ReplacementBackupFacts struct {
	Size    int64  // byte length of the set-aside backup file
	ModUnix int64  // backup mtime in Unix seconds
	SHA256  string // hex sha256 of the set-aside bytes (wave-63); empty when unstamped
}

// BackupFactsStamped reports whether an entry carries usable identity facts
// (wave-25): the mtime stamp is the gate — a set-aside backup's mtime is
// never the Unix epoch in practice, while a zero file length is a real
// possibility for overwritten placeholder files.
func (e ReplacementEntry) BackupFactsStamped() bool { return e.BackupModUnix != 0 }

// Restore-pending kinds carried by ReplacementEntry.RestorePendingKind. The
// persistence contract keeps the clean kind UNWRITTEN ("") so wave-19 blobs
// for the legacy path stay byte-identical to their wave-18 form; explicit
// rearm-refused and prune intents materialize in JSON.
const (
	// RestorePendingKindClean certifies the destination bytes are in place
	// while the journal-owned backup still holds this operation's own bytes,
	// awaiting removal + journal consumption.
	RestorePendingKindClean = "clean"
	// RestorePendingKindRearmRefused certifies the destination bytes are in
	// place while the backup name is UNOWNED: a refused no-replace re-arm
	// (fsutil.ErrPublishCollision — a foreign writer owns the name — or
	// fsutil.ErrPublishNoReplaceUnsupported — the volume cannot express the
	// publish, so nothing occupies the name) left it foreign or absent. The
	// pending retry consumes the journal entry without any backup-path
	// operation.
	RestorePendingKindRearmRefused = "rearm_refused"
	// RestorePendingKindPrune records that the destination still contains the
	// organized bytes and the backup is being removed because its operation is
	// fenced for retention. It must never use restore-completion semantics.
	RestorePendingKindPrune = "prune"
)

// PendingKind normalizes the entry's restore-pending kind. A non-pending
// entry has no kind; a pending entry with an empty (legacy, pre-wave-19) kind
// defaults to clean; an UNRECOGNIZED kind conservatively reads as
// rearm-refused — never run path operations against a name whose ownership a
// newer writer's marker describes but this build cannot interpret.
func (e ReplacementEntry) PendingKind() string {
	if !e.RestorePending {
		return ""
	}
	switch e.RestorePendingKind {
	case "", RestorePendingKindClean:
		return RestorePendingKindClean
	case RestorePendingKindPrune:
		return RestorePendingKindPrune
	default:
		return RestorePendingKindRearmRefused
	}
}

// SetRestorePending marks the entry restore-pending with the given kind,
// reporting whether the entry changed. An identical re-mark is a no-op; a
// pending entry UPGRADES to the rearm-refused kind (a later refused re-arm
// vacated the name) but never DOWNGRADES back to clean — a name once proven
// unowned must never re-enter the removal path.
func (e *ReplacementEntry) SetRestorePending(kind string) bool {
	if e.RestorePending {
		if e.PendingKind() == kind {
			return false
		}
		if kind == RestorePendingKindPrune {
			if e.PendingKind() != RestorePendingKindClean {
				return false
			}
			e.RestorePendingKind = RestorePendingKindPrune
			return true
		}
		if e.PendingKind() == RestorePendingKindPrune {
			return false
		}
		if kind != RestorePendingKindRearmRefused {
			return false
		}
		e.RestorePendingKind = RestorePendingKindRearmRefused
		return true
	}
	e.RestorePending = true
	if kind != RestorePendingKindClean {
		e.RestorePendingKind = kind
	}
	return true
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
