package downloader

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ReplacementRecorder is owned by the downloader and implemented by the
// workflow's RevertLog: every replaced byte pair (destination + backup) is
// journaled for the move-back machinery BEFORE the replace lands
// (POSTER-WRITE-HARDENING P3 D8).
type ReplacementRecorder interface {
	// RecordReplacement persists the (replaced → backup) mapping for opID.
	// It runs AFTER the backup has been renamed aside and BEFORE the new
	// bytes install — a failure must keep the downloader from replacing.
	// Wave-25 (codex P3 PR#215): the optional trailing facts stamp the
	// set-aside backup's identity (size + mtime seconds) into the journaled
	// entry so history's removal gate can bind later unlinks to the OWNED
	// object rather than to the pathname alone; a missing/failed capture
	// leaves the entry unstamped (legacy removal posture).
	RecordReplacement(ctx context.Context, opID, replacedPath, backupPath string, backupFacts ...models.ReplacementBackupFacts) error
	// ConfirmReplacement flips the entry to installed AFTER the new bytes
	// landed — distinguishing "install never completed" (crash window) from
	// "media deleted afterwards" for the sweeper (P3 R4-3).
	ConfirmReplacement(ctx context.Context, opID, replacedPath, backupPath string) error
	// ReleaseReplacement retracts a journal entry whose bytes were rolled
	// back by the downloader itself (record landed but the install failed,
	// so the backup was restored over the destination). Without retraction
	// the row keeps pointing at a consumed backup and every later revert
	// fails stat-ing it (codex P3 round 1).
	ReleaseReplacement(ctx context.Context, opID, replacedPath, backupPath string) error
	// MarkReplacementRestorePendingKind disarms a journaled entry whose
	// rollback already restored the destination bytes but whose backup
	// re-arm FAILED — for EVERY failure class (wave-19 refusal classes,
	// generalized by wave-21, codex P2 PR#215). Leaving the entry ARMED
	// against an unowned (foreign-occupied or absent) backupPath would aim
	// the next revert at bytes this operation does not own — foreign bytes
	// restored over the destination, then the occupant deleted — while an
	// entry armed against an ABSENT name wedges every later explicit revert
	// at the backup source stat forever and sweeps find nothing to repair.
	// The kind routes the retry through models' pending-kind state machine:
	// models.RestorePendingKindRearmRefused when the name is unowned
	// (refusal classes AND any pre-publish failure — retries consume
	// journal-only, no backup-path operation), models.RestorePendingKindClean
	// when the failed re-arm demonstrably published THIS operation's own
	// bytes at backupPath (fsutil.PublishCompleted — retries reap the owned
	// name, then consume). Idempotent; the merge discipline (one-way upgrade
	// to rearm-refused, never a downgrade) lives in
	// models.ReplacementEntry.SetRestorePending.
	MarkReplacementRestorePendingKind(ctx context.Context, opID, replacedPath, backupPath, kind string) error
}

// downloadLedger is the internal option wrapper folding command-level
// operation identity (operation ID + recorder) into the variadic options
// chain that fans out through the media-type download helpers.
type downloadLedger struct {
	opID     string
	recorder ReplacementRecorder
}

// resolveDownloadLedger extracts the operation recorder from the options
// chain; zero value = no ledger armed (overwrites of existing bytes are
// refused with skip+warn).
func resolveDownloadLedger(options []any) downloadLedger {
	var d downloadLedger
	for _, option := range options {
		if value, ok := option.(downloadLedger); ok {
			d = value
		}
	}
	return d
}

// armed reports whether the ledger may authorize a destructive overwrite:
// a designated operation ID AND a recorder to journal it under.
func (d downloadLedger) armed() bool {
	return d.recorder != nil && strings.TrimSpace(d.opID) != ""
}
