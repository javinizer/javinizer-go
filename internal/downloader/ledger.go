package downloader

import (
	"context"
	"strings"
)

// ReplacementRecorder is owned by the downloader and implemented by the
// workflow's RevertLog: every replaced byte pair (destination + backup) is
// journaled for the move-back machinery BEFORE the replace lands
// (POSTER-WRITE-HARDENING P3 D8).
type ReplacementRecorder interface {
	// RecordReplacement persists the (replaced → backup) mapping for opID.
	// It runs AFTER the backup has been renamed aside and BEFORE the new
	// bytes install — a failure must keep the downloader from replacing.
	RecordReplacement(ctx context.Context, opID, replacedPath, backupPath string) error
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
