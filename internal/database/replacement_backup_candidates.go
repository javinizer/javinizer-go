package database

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ReplacementBackupCandidateRepository is an optional narrow projection used
// for live shared-backup checks. Results are candidates, NOT deletion authority:
// callers must parse them and compare canonical backup keys under their locks.
type ReplacementBackupCandidateRepository interface {
	FindReplacementBackupCandidates(context.Context, string) ([]models.BatchFileOperation, error)
}

// replacementBackupNonce recognizes generated backup names, including the
// allocator's hexadecimal ordinal suffix. Unsupported/legacy/quarantine names
// deliberately take the complete ledger projection instead of guessing.
func replacementBackupNonce(backup string) string {
	lower := strings.ToLower(backup)
	i := strings.LastIndex(lower, ".dlbak.")
	if i < 0 {
		return ""
	}
	suffix := strings.Split(lower[i+len(".dlbak."):], ".")
	if len(suffix) > 2 || len(suffix[0]) != 16 {
		return ""
	}
	if _, err := hex.DecodeString(suffix[0]); err != nil {
		return ""
	}
	if len(suffix) == 2 {
		if suffix[1] == "" {
			return ""
		}
		for _, c := range suffix[1] {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return ""
			}
		}
	}
	return suffix[0]
}

// FindReplacementBackupCandidates avoids hydrating historical NFO snapshots.
// A generated ASCII nonce is invariant under path cleaning, separator aliases,
// current simple per-rune case mapping and NFC of parent/name components. We search
// only that nonce, never the whole pathname or the operation's destination.
// Any JSON Unicode escape is included conservatively because it can conceal a
// nonce character. Malformed JSON is included for the caller's fail-closed gate.
// A syntactically valid ledger with neither spelling cannot refer to this
// backup, even if unrelated fields would fail Go schema decoding; it need not
// globally block cleanup. Candidate schema errors still fail closed in callers.
// This bounds normal Go hydration/parsing, not SQL work: the text query remains
// a table scan, and unsupported names deliberately retain a broad projection.
func (r *BatchFileOperationRepository) FindReplacementBackupCandidates(ctx context.Context, backup string) ([]models.BatchFileOperation, error) {
	query := r.GetDB().WithContext(ctx).Select("id", "generated_files").Where("generated_files IS NOT NULL AND generated_files <> ''")
	if nonce := replacementBackupNonce(backup); nonce != "" {
		query = query.Where("instr(lower(generated_files), ?) > 0 OR instr(generated_files, ?) > 0 OR NOT json_valid(generated_files)", nonce, `\u`)
	}
	var rows []models.BatchFileOperation
	if err := query.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, wrapDBErr("find", "replacement backup candidates", err)
	}
	return rows, nil
}
