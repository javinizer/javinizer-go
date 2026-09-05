package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// generatedFilesJSON and fileMove moved to internal/models/revert_types.go.
// The workflow package imports models.GeneratedFilesJSON and models.FileMove instead of
// defining duplicate types.
//
// generatedFilesJSON mirrors the history package structure so workflow stays dependency-free.
// no longer mirrors — both import from models.

// nfoSnapshotResult holds the result of reading an NFO snapshot.
type nfoSnapshotResult struct {
	Content   string
	FoundPath string
}

// OperationID correlates a RevertLog record with an Apply operation.
// Per CONTEXT.md: returned by Begin, passed to Complete for correlation.
type OperationID = string

// RevertLog is the seam for revert-history lifecycle.
// Per CONTEXT.md: Begin is called before Apply mutates the filesystem (crash-safety guarantee).
// Complete is called after Apply succeeds (records the outcome for undo).
// Disabled by default — enabled via config.Output.AllowRevert.
//
// Begin writes a database record with movie ID, original path, and operation
// type — no filesystem access. CaptureSnapshot reads the NFO and updates the record.
// If CaptureSnapshot fails, Begin's record still exists — Apply proceeds with partial revert
// safety. The NFO snapshot is optional enrichment, not a precondition.
type RevertLog interface {
	// Begin persists a pre-mutation record before Apply starts filesystem changes.
	// Returns an OperationID for correlating with the revert record.
	// Must be called BEFORE any filesystem mutation.
	// pure database write — no filesystem I/O.
	Begin(ctx context.Context, cmd ApplyCmd) (OperationID, error)

	// CaptureSnapshot reads the existing NFO file and updates the revert record
	// with the snapshot content. Call after Begin, before filesystem mutation.
	// If snapshot fails, the revert record still exists — partial safety is better
	// than none. filesystem I/O separated from Begin's DB write.
	CaptureSnapshot(ctx context.Context, opID OperationID, cmd ApplyCmd)

	// Complete records the outcome after Apply finishes — success or failure.
	// When result is non-nil (success path), the pre-record is updated with post-apply state.
	// When result is nil (failure path), the pre-record is marked as failed to prevent
	// orphaned records with RevertStatusApplied that are indistinguishable from successful applies.
	Complete(ctx context.Context, opID OperationID, result *ApplyResult) error

	// CompleteFailed records a failed apply while preserving any filesystem
	// mutations already performed (e.g. an organize that moved the file). Unlike
	// Complete with a nil result, the partial result's NewPath/generated files
	// are persisted so the record remains revertable. The record is marked
	// RevertStatusFailed. Use this when a later pipeline step fails after an
	// earlier step has already mutated the filesystem.
	CompleteFailed(ctx context.Context, opID OperationID, result *ApplyResult) error

	// RecordReplacement implements the downloader's ReplacementRecorder seam
	// (POSTER-WRITE-HARDENING P3): the pre-existing bytes at replacedPath have
	// already been moved aside to backupPath under the downloader's
	// per-destination lock; this journals the pair on the operation row BEFORE
	// the new bytes install. Appends are serialized per operation row and each
	// entry carries a restart-persistent per-destination sequence (assigned
	// inside the downloader's destination lock, so call order is the true
	// replace order). Complete/CompleteFailed merge these incremental entries
	// into the final generated-files ledger.
	// Wave-25 (codex P3 PR#215): the optional trailing backupFacts stamp the
	// set-aside backup's size + mtime into the entry so history's removal gate
	// can verify the object at backupPath is the OWNED set-aside before
	// unlinking it (journal-append and stamp are one atomic write here).
	RecordReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string, backupFacts ...models.ReplacementBackupFacts) error

	// ReleaseReplacement retracts a journal entry the downloader rolled back
	// itself (record landed, install failed, backup restored over the
	// destination). The row must not keep pointing at the consumed backup.
	ReleaseReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string) error

	// MarkReplacementRestorePending disarms a journaled entry the downloader
	// rolled back but whose backup re-arm was REFUSED with the occupied-name
	// classes (fsutil.PublishRefusal): the backup name is foreign-occupied or
	// absent, so leaving the entry armed would aim the next revert at bytes
	// this operation does not own. The rollback already restored the
	// destination, so the entry is marked RestorePending with the wave-19
	// rearm-refused kind — certified destination, consumption retry without
	// any backup-path operation (codex P2, PR#215 wave-19).
	MarkReplacementRestorePending(ctx context.Context, opID OperationID, replacedPath, backupPath string) error

	// MarkReplacementRestorePendingKind is MarkReplacementRestorePending with
	// an explicit restore-pending kind (wave-21, codex P2 PR#215): the
	// downloader's rollback re-arm now disarms the journal entry for EVERY
	// re-arm failure class, and the kind alone routes on backup-name
	// ownership — models.RestorePendingKindRearmRefused for the unowned
	// (foreign-occupied or absent) name, models.RestorePendingKindClean when
	// the failed re-arm demonstrably published this operation's own bytes
	// (the fsutil.PublishCompleted class). Unknown kinds are rejected, never
	// persisted.
	MarkReplacementRestorePendingKind(ctx context.Context, opID OperationID, replacedPath, backupPath, kind string) error

	// ConfirmReplacement marks the journaled entry installed after the new
	// bytes landed (P3 R4-3 crash-window marker).
	ConfirmReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string) error
}

// RevertLogConfig holds the subset of configuration needed by dbRevertLog.
// Uses *nfo.Config directly — no duplication of NFO template resolution fields.
// The NFO package owns FilenameTemplate, PerFile, GroupActress, GroupActressName,
// and FirstNameOrder; this struct only adds the workflow-level AllowRevert toggle.
type RevertLogConfig struct {
	AllowRevert bool
	NFOCfg      *nfo.Config
}

// NewRevertLogConfig constructs a RevertLogConfig from its constituent fields.
// Provided so that callers outside the workflow package (e.g. API batch tests)
// can build a RevertLogConfig without constructing a full *nfo.Config.
func NewRevertLogConfig(allowRevert bool, nfoCfg *nfo.Config) *RevertLogConfig {
	return &RevertLogConfig{
		AllowRevert: allowRevert,
		NFOCfg:      nfoCfg,
	}
}

// ToNFONameConfig converts RevertLogConfig to nfo.NFONameConfig, filling in
// the caller-provided multipart fields.
func (c *RevertLogConfig) ToNFONameConfig(isMultiPart bool, partSuffix string, partNumber int) nfo.NFONameConfig {
	if c.NFOCfg != nil {
		return c.NFOCfg.ToNFONameConfig(isMultiPart, partSuffix, partNumber)
	}
	return nfo.NFONameConfig{
		IsMultiPart: isMultiPart,
		PartSuffix:  partSuffix,
		PartNumber:  partNumber,
	}
}

// noOpRevertLog is a no-op adapter used when no repository/config is available
// (e.g. scrape-only workflows, or defensive nil-deps paths). Operation recording
// is NOT gated by AllowRevert — see NewRevertLogFromConfig.
type noOpRevertLog struct{}

func (noOpRevertLog) Begin(_ context.Context, _ ApplyCmd) (OperationID, error) {
	return "", nil
}

func (noOpRevertLog) CaptureSnapshot(_ context.Context, _ OperationID, _ ApplyCmd) {
	// no-op when revert is disabled
}

func (noOpRevertLog) Complete(_ context.Context, _ OperationID, _ *ApplyResult) error {
	return nil
}

func (noOpRevertLog) CompleteFailed(_ context.Context, _ OperationID, _ *ApplyResult) error {
	return nil
}

func (noOpRevertLog) RecordReplacement(_ context.Context, _ OperationID, _, _ string, _ ...models.ReplacementBackupFacts) error {
	// No durable store — journal nothing. Callers must not arm the downloader
	// ledger with a no-op recorder: workflow threads the recorder only when the
	// concrete RevertLog is the DB-backed implementation (see replacementRecorder).
	return nil
}

func (noOpRevertLog) ReleaseReplacement(_ context.Context, _ OperationID, _, _ string) error {
	return nil
}

func (noOpRevertLog) MarkReplacementRestorePending(_ context.Context, _ OperationID, _, _ string) error {
	return nil
}

func (noOpRevertLog) MarkReplacementRestorePendingKind(_ context.Context, _ OperationID, _, _, _ string) error {
	return nil
}

func (noOpRevertLog) ConfirmReplacement(_ context.Context, _ OperationID, _, _ string) error {
	return nil
}

// dbRevertLog persists BatchFileOperation records via BatchFileOperationRepository.
// Per CONTEXT.md: the existing models.BatchFileOperation model and history.go snapshot
// functions are the persistence mechanism.
type dbRevertLog struct {
	repo           database.BatchFileOperationRepositoryInterface
	cfg            *RevertLogConfig
	jobID          string
	fs             afero.Fs
	templateEngine template.EngineInterface
	nfoFieldMerger nfo.NFOFieldMerger
	logger         logging.Logger
}

// NewDBRevertLog returns a RevertLog that persists batch file operation records through repo.
func NewDBRevertLog(repo database.BatchFileOperationRepositoryInterface, cfg *RevertLogConfig, jobID string, fs afero.Fs, templateEngine template.EngineInterface, nfoFieldMerger nfo.NFOFieldMerger, logger logging.Logger) RevertLog {
	logger = resolveLogger(logger)
	return &dbRevertLog{repo: repo, cfg: cfg, jobID: jobID, fs: fs, templateEngine: templateEngine, nfoFieldMerger: nfoFieldMerger, logger: logger}
}

func readNFOSnapshot(logger logging.Logger, fs afero.Fs, candidatePaths ...string) nfoSnapshotResult {
	for _, p := range candidatePaths {
		if p == "" {
			continue
		}
		canonical, err := fsutil.CanonicalizePath(p)
		if err != nil {
			continue
		}
		data, err := afero.ReadFile(fs, canonical)
		if err == nil {
			return nfoSnapshotResult{Content: string(data), FoundPath: canonical}
		}
		if !os.IsNotExist(err) {
			logger.Warnf("Failed to read NFO snapshot from %q: %v", canonical, err)
		}
	}
	return nfoSnapshotResult{}
}

func determineOperationType(moveFiles bool, linkMode organizer.LinkMode, isUpdateMode bool) models.OperationTypeEnum {
	if isUpdateMode {
		return models.OperationTypeUpdate
	}
	if !moveFiles && linkMode == organizer.LinkModeHard {
		return models.OperationTypeHardlink
	}
	if !moveFiles && linkMode == organizer.LinkModeSoft {
		return models.OperationTypeSymlink
	}
	if !moveFiles {
		return models.OperationTypeCopy
	}
	return models.OperationTypeMove
}

func newPreOrganizeRecord(batchJobID, movieID, originalPath, nfoSnapshot, nfoPath, originalDirPath string, operationType models.OperationTypeEnum, inPlaceRenamed bool) *models.BatchFileOperation {
	return &models.BatchFileOperation{
		BatchJobID:      batchJobID,
		MovieID:         movieID,
		OriginalPath:    originalPath,
		NewPath:         "",
		OperationType:   operationType,
		NFOSnapshot:     nfoSnapshot,
		NFOPath:         nfoPath,
		GeneratedFiles:  "",
		RevertStatus:    models.RevertStatusApplied,
		InPlaceRenamed:  inPlaceRenamed,
		OriginalDirPath: originalDirPath,
	}
}

// mergeReplacementLedger carries replacement entries journaled incrementally
// by RecordReplacement into the freshly-built generated-files payload so
// Complete/CompleteFailed never drop the revert-ledger's move-back journal.
// newRaw == "" (no delete/move-back output) upgrades to a payload carrying
// just the replacements; unparseable prior content degrades to newRaw.
// appendLedgerRoot adds root to the ledger's seeded discovery roots (dedup).
func appendLedgerRoot(raw, root string) string {
	if raw == "" {
		return models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{root}})
	}
	gf, err := models.ParseGeneratedFiles(raw)
	if err != nil {
		return raw
	}
	for _, r := range gf.Roots {
		if r == root {
			return raw
		}
	}
	gf.Roots = append(gf.Roots, root)
	data := models.MarshalLedgerJSON(gf)
	return data
}

func mergeReplacementLedger(priorRaw, newRaw string) string {
	if priorRaw == "" {
		return newRaw
	}
	prior, err := models.ParseGeneratedFiles(priorRaw)
	if err != nil || (len(prior.Replacements) == 0 && len(prior.Roots) == 0) {
		return newRaw
	}
	if newRaw == "" {
		return models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: prior.Replacements, Roots: prior.Roots})
	}
	fresh, err := models.ParseGeneratedFiles(newRaw)
	if err != nil {
		return newRaw
	}
	fresh.Replacements = prior.Replacements
	if len(fresh.Roots) == 0 {
		fresh.Roots = prior.Roots
	}
	return models.MarshalLedgerJSON(fresh)
}

func buildGeneratedFilesJSON(logger logging.Logger, nfoPath string, subtitleMoves []models.SubtitleMove, downloadPaths []string) string {
	gf := models.GeneratedFilesJSON{}

	deleteList := make([]string, 0, 1+len(downloadPaths))
	if nfoPath != "" {
		deleteList = append(deleteList, nfoPath)
	}
	deleteList = append(deleteList, downloadPaths...)
	if len(deleteList) > 0 {
		gf.Delete = deleteList
	}

	if len(subtitleMoves) > 0 {
		moveBackList := make([]models.FileMove, 0, len(subtitleMoves))
		for _, sr := range subtitleMoves {
			switch {
			// #224 phase E mode distinction: a copy-installed subtitle retains
			// its source, so the revert artifact is the installed copy (delete
			// it); only a move-installed subtitle moves back.
			case sr.Copied && sr.NewPath != "":
				gf.Delete = append(gf.Delete, sr.NewPath)
			case sr.Moved && sr.OriginalPath != "" && sr.NewPath != "":
				moveBackList = append(moveBackList, models.FileMove{OriginalPath: sr.OriginalPath, NewPath: sr.NewPath})
			}
		}
		if len(moveBackList) > 0 {
			gf.MoveBack = moveBackList
		}
	}

	if len(gf.Delete) == 0 && len(gf.MoveBack) == 0 {
		return ""
	}

	data, err := json.Marshal(gf)
	if err != nil {
		logger.Warnf("Failed to marshal generatedFilesJSON: %v (attempting partial recovery)", err)
		data, err = json.Marshal(models.GeneratedFilesJSON{Delete: gf.Delete})
		if err != nil {
			logger.Warnf("Failed to marshal partial generatedFilesJSON: %v", err)
			return ""
		}
	}
	return string(data)
}

func updatePostOrganize(op *models.BatchFileOperation, newPath string, inPlaceRenamed bool, originalDirPath string, generatedFilesJSON string) {
	op.NewPath = newPath
	op.InPlaceRenamed = inPlaceRenamed
	op.OriginalDirPath = originalDirPath
	op.GeneratedFiles = generatedFilesJSON
}

// completionLedgerMerge computes the completion-transaction journal: the apply
// outcome's generated-files payload (newRaw) merges into the row's FRESH
// in-transaction journal bytes (currentRaw) — replacement entries and seeded
// discovery roots on the fresh row carry into the merged ledger — and the
// organizer's leaf folder is appended as a discovery root (R4-2: media
// actually lands there, so the sweeper's bounded recursion starts there).
// persist=false reports an idempotent no-op (merged bytes identical to what
// the row already carries, e.g. a retried completion).
func completionLedgerMerge(currentRaw, newRaw, folderRoot string) (next models.GeneratedFilesJSON, persist bool, merged string, err error) {
	merged = mergeReplacementLedger(currentRaw, newRaw)
	if folderRoot != "" {
		merged = appendLedgerRoot(merged, folderRoot)
	}
	if merged == currentRaw {
		return models.GeneratedFilesJSON{}, false, merged, nil
	}
	gf, perr := models.ParseGeneratedFiles(merged)
	if perr != nil {
		// Unreachable through mergeReplacementLedger's byte contract (its output
		// is always empty or marshaled JSON); refuse the transaction rather than
		// persist a blob journal readers cannot parse.
		return models.GeneratedFilesJSON{}, false, "", perr
	}
	return gf, true, merged, nil
}

// mergeJournalInTx persists a completion's journal contribution through the
// serialized journal transaction (codex review 4960250562 follow-up, wave-9):
// the merge runs against the row re-read INSIDE the BEGIN IMMEDIATE
// transaction, never against the completion's stale preRecord snapshot, so a
// concurrent process appending (RecordReplacement) or consuming (revert/sweep)
// entries can no longer be overwritten by a full Save — resurrected consumed
// entries and erased new entries both came from that snapshot merge. Only the
// journal column moves through the transaction; the caller's follow-up
// UpdateNonJournalFields persists ONLY the non-journal columns (wave-10: the
// follow-up full Save used to re-persist the tx-derived bytes, which still
// clobbered any journal mutation committed between the tx commit and the
// Save), so generated_files is owned exclusively by UpdateJournalInTx.
func (l *dbRevertLog) mergeJournalInTx(ctx context.Context, recordID uint, opID OperationID, caller, newRaw, folderRoot string) (string, error) {
	var merged string
	txErr := l.repo.UpdateJournalInTx(ctx, recordID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		next, persist, m, err := completionLedgerMerge(current.GeneratedFiles, newRaw, folderRoot)
		merged = m
		return next, persist, err
	})
	switch {
	case errors.Is(txErr, database.ErrNotFound):
		return "", fmt.Errorf("revert log %s: record %s not found", caller, opID)
	case txErr != nil:
		return "", fmt.Errorf("revert log %s: persist journal for record %s: %w", caller, opID, txErr)
	}
	return merged, nil
}

// persistNonJournalColumns is the wave-15 seam every completion-side
// non-journal column publish routes through (POSTER-WRITE-HARDENING codex
// P1): the repository reports database.ErrOperationRowReverted when a
// concurrent writer reverted the row before this update's status columns
// landed — the non-status columns committed while the stored reverted state
// stays authoritative. The completion LOST the race: warn through the logger
// seam, reflect the truth on the in-memory record so later readers never see
// the reverted operation resurface as live, and report success — external
// behavior is unchanged except that the reverted row is never clobbered.
func (l *dbRevertLog) persistNonJournalColumns(ctx context.Context, opID OperationID, record *models.BatchFileOperation) error {
	err := l.repo.UpdateNonJournalFields(ctx, record)
	if errors.Is(err, database.ErrOperationRowReverted) {
		resolveLogger(l.logger).Warnf("[revert-log] record %s was reverted concurrently with this completion; completion columns persisted, reverted status preserved", opID)
		record.RevertStatus = models.RevertStatusReverted
		return nil
	}
	return err
}

// ctx is accepted for future use when repository methods support context propagation
// Begin is a pure database write — no filesystem I/O.
func (l *dbRevertLog) Begin(ctx context.Context, cmd ApplyCmd) (OperationID, error) {
	if cmd.Movie == nil {
		return "", nil
	}

	isUpdateMode := cmd.Organize.Skip
	opType := determineOperationType(!cmd.Organize.Skip && cmd.Organize.MoveFiles, cmd.Organize.LinkMode, isUpdateMode)

	sourceDir := filepath.Dir(cmd.Match.Path)

	// write the DB record without NFO snapshot.
	// CaptureSnapshot will fill in the snapshot content separately.
	// Seed the discovery root NOW, pre-mutation: the row must name where
	// downloads will land even if the process dies before any journal entry
	// exists (codex P3 R3-3 — the pre-journal crash window).
	seed, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{cmd.DestPath}})
	if cmd.DestPath == "" {
		seed = nil
	}
	preRecord := newPreOrganizeRecord(
		l.jobID, cmd.Movie.ID, cmd.Match.Path,
		"", "", sourceDir, // no snapshot yet
		opType, false,
	)
	preRecord.GeneratedFiles = string(seed)
	if err := l.repo.Create(ctx, preRecord); err != nil {
		return "", fmt.Errorf("revert log Begin failed: %w", err)
	}

	return fmt.Sprintf("%d", preRecord.ID), nil
}

// CaptureSnapshot reads the existing NFO file and updates the revert record
// with the snapshot content. filesystem I/O separated from Begin's
// DB write. If the snapshot fails, the revert record still exists — partial safety
// is better than none.
func (l *dbRevertLog) CaptureSnapshot(ctx context.Context, opID OperationID, cmd ApplyCmd) {
	if opID == "" || cmd.Movie == nil {
		return
	}

	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		return
	}
	recordID := uint(recordID64)

	preRecord, err := l.repo.FindByID(ctx, recordID)
	if err != nil {
		resolveLogger(l.logger).Warnf("[revert-log] CaptureSnapshot: failed to find record %d (opID: %s): %v", recordID, opID, err)
		return
	}
	if preRecord == nil {
		resolveLogger(l.logger).Warnf("[revert-log] CaptureSnapshot: record %d not found (opID: %s)", recordID, opID)
		return
	}

	sourceDir := filepath.Dir(cmd.Match.Path)
	isMultiPart := cmd.Match.IsMultiPart
	partSuffix := ""
	if isMultiPart {
		partSuffix = cmd.Match.PartSuffix
	}

	var nameCfg nfo.NFONameConfig
	if l.cfg != nil {
		nameCfg = l.cfg.ToNFONameConfig(isMultiPart, partSuffix, cmd.Match.PartNumber)
	} else {
		nameCfg = nfo.NFONameConfig{
			IsMultiPart: isMultiPart,
			PartSuffix:  partSuffix,
			PartNumber:  cmd.Match.PartNumber,
		}
	}

	// resolve NFO paths through the NFOFieldMerger seam instead of
	// reaching into the nfo package directly.
	var nfoPath string
	var legacyPaths []string
	if l.nfoFieldMerger != nil {
		nfoPath, legacyPaths = l.nfoFieldMerger.ResolveNFOPath(sourceDir, cmd.Movie, nameCfg, cmd.Match.Path)
	}

	snapshotCandidates := []string{nfoPath}
	snapshotCandidates = append(snapshotCandidates, legacyPaths...)

	snapshotResult := readNFOSnapshot(resolveLogger(l.logger), l.fs, snapshotCandidates...)

	effectiveNFOPath := snapshotResult.FoundPath
	if effectiveNFOPath == "" && len(snapshotCandidates) > 0 {
		effectiveNFOPath = snapshotCandidates[0]
	}

	// Update the existing record with snapshot data
	preRecord.NFOSnapshot = snapshotResult.Content
	preRecord.NFOPath = effectiveNFOPath
	if preRecord.OriginalDirPath == "" {
		preRecord.OriginalDirPath = sourceDir
	}

	// Wave-10: non-journal columns only — the generated_files column is owned
	// by UpdateJournalInTx, so a concurrent journal append between FindByID
	// and this write must not be clobbered by a full Save of the snapshot.
	// Wave-15: a concurrent revert committed first is tolerated (warned,
	// never clobbered) inside persistNonJournalColumns.
	if updateErr := l.persistNonJournalColumns(ctx, opID, preRecord); updateErr != nil {
		resolveLogger(l.logger).Warnf("[revert-log] CaptureSnapshot: failed to update record %s: %v", opID, updateErr)
	}
}

// noopJournal reports whether result's apply mutated NOTHING on the
// filesystem, so its row must journal no target fields and finalize
// completed-noop (codex P1/P2 PR #241 + batch-2 F1/F2):
//   - authorized intra-batch duplicate skips (OrganizeResult.DuplicateSkipped)
//     — NewPath names the batch winner's shared destination for display only;
//   - pre-publication organize terminals (ApplyResult.PrePublication for plan
//     rejections/context aborts — the organizer returns no result on those
//     legs — or OrganizeResult.PrePublication for strategy failures before any
//     publish) — the destination never received this file's bytes, and the
//     intent path may again be a shared destination a promoted claimant later
//     publishes.
//
// Journaling target fields from such results arms this row's revert against
// another claimant's published bytes (moving/deleting them onto this row's
// source); finalizing them failed-with-empty-NewPath leaves the reverter
// probing a "" anchor (anchor_missing) forever, blocking fully-reverted.
func noopJournal(result *ApplyResult) bool {
	if result == nil {
		return false
	}
	if result.PrePublication {
		return true
	}
	org := result.OrganizeResult
	return org != nil && (org.DuplicateSkipped || org.PrePublication)
}

// ctx is accepted for future use when repository methods support context propagation
func (l *dbRevertLog) Complete(ctx context.Context, opID OperationID, result *ApplyResult) error {
	if opID == "" {
		return nil
	}

	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		//nolint:nilerr // non-parseable opID is not an error (e.g. noOpRevertLog returns "")
		return nil
	}
	recordID := uint(recordID64)

	release := replacementLedgerLocks.Acquire(opID)
	defer release()

	preRecord, err := l.repo.FindByID(ctx, recordID)
	if err != nil {
		return fmt.Errorf("revert log Complete: find record %s: %w", opID, err)
	}
	if preRecord == nil {
		return nil // genuinely not found — no error
	}

	if result == nil {
		updatePostOrganize(preRecord, "", false, preRecord.OriginalDirPath, "")
		preRecord.RevertStatus = models.RevertStatusFailed
		// Wave-10: generated_files stays with UpdateJournalInTx even here — a
		// full Save of this snapshot could erase a foreign journal append.
		// Wave-15: a concurrent revert suppresses this Failed mark
		// (persistNonJournalColumns warns + tolerates) instead of being
		// clobbered by it.
		if updateErr := l.persistNonJournalColumns(ctx, opID, preRecord); updateErr != nil {
			return fmt.Errorf("revert log Complete: mark record %s as failed: %w", opID, updateErr)
		}
		resolveLogger(l.logger).Warnf("[revert-log] Apply failed for %s — pre-record marked as incomplete", opID)
		return nil
	}

	sourceDir := preRecord.OriginalDirPath
	var newPath string
	var inPlaceRenamed bool
	var subtitles []models.SubtitleMove
	// codex P1 (PR #241): an authorized intra-batch duplicate skip is a true
	// NO-OP for journal purposes — its OrganizeResult.NewPath names the
	// WINNER's shared destination for display only. Persisting it would make
	// the history reverter arm the winner's video as the loser's primary moved
	// file (moving the winner's bytes onto the loser's path if that source was
	// later removed). The winner's own operation row stays the sole subject.
	// codex batch-2 F1/F2: pre-publication failure terminals obey the same
	// rule — the destination was never published by this operation.
	if org := result.OrganizeResult; org != nil && !noopJournal(result) {
		newPath = org.NewPath
		inPlaceRenamed = org.InPlaceRenamed
		for _, sr := range org.Subtitles {
			if sr.Moved || sr.Copied {
				subtitles = append(subtitles, sr.SubtitleMove)
			}
		}
		if org.FolderPath != "" && sourceDir == "" {
			sourceDir = org.OldDirectoryPath
		}
	}

	// codex P1 (PR #241): a skipped duplicate generates NOTHING — its apply
	// short-circuits before merge/download/NFO — but the STRICT journal no-op
	// is enforced here too: any generated path paired with a DuplicateSkipped
	// result names the WINNER's shared artifact, and journaling it onto the
	// loser's row would arm a loser revert to DELETE the winner's files. The
	// same strict gate covers pre-publication terminals (batch-2 F1/F2): no
	// subtitle/extras/NFO finger of the failed row may name a shared path.
	nfoPath, foundNFOPath, downloadPaths := result.NFOPath, result.FoundNFOPath, result.DownloadPaths
	if noopJournal(result) {
		nfoPath, foundNFOPath, downloadPaths = "", "", nil
	}

	// Wave-9 (codex review 4960250562 follow-up): the journal read-modify-write
	// runs inside the row transaction against the FRESH row — merging into the
	// preRecord snapshot here let a concurrent append/consume be overwritten by
	// the follow-up full Save. Wave-10 closes the residual window: the follow-up
	// itself (UpdateNonJournalFields below) no longer writes generated_files at
	// all, so an append/consume committed after this tx commit survives.
	folderRoot := ""
	if org := result.OrganizeResult; org != nil && !noopJournal(result) {
		folderRoot = org.FolderPath
	}
	mergedJournal, err := l.mergeJournalInTx(ctx, recordID, opID, "Complete",
		buildGeneratedFilesJSON(resolveLogger(l.logger), nfoPath, subtitles, downloadPaths), folderRoot)
	if err != nil {
		return err
	}

	if foundNFOPath != "" {
		preRecord.NFOPath = foundNFOPath
	}
	if nfoPath != "" && preRecord.NFOPath == "" {
		preRecord.NFOPath = nfoPath
	}

	// codex P2 (PR #241 F2): finalize the authorized duplicate skip. The row
	// journaled nothing and named no NewPath, so leaving it RevertStatusApplied
	// makes the reverter probe checkAnchor("") → anchor_missing on every batch
	// revert attempt and the batch can never report fully reverted. The apply
	// SUCCEEDED as a true no-op — the row is completed-noop, excluded from
	// revert selection exactly like a reverted row. Batch-2 F1/F2 extend the
	// identical terminal to pre-publication organize failures: nothing was
	// mutated, so nothing is unwindable.
	if noopJournal(result) {
		preRecord.RevertStatus = models.RevertStatusNoOp
	}

	updatePostOrganize(preRecord, newPath, inPlaceRenamed, sourceDir, mergedJournal)
	if err := l.persistNonJournalColumns(ctx, opID, preRecord); err != nil {
		return fmt.Errorf("revert log Complete: update post-apply record for %s: %w", opID, err)
	}
	return nil
}

// CompleteFailed records a failed apply while preserving any filesystem
// mutations already performed. It reuses the success-path persistence logic
// so NewPath and generated files are retained (keeping the record revertable),
// then marks the record RevertStatusFailed.
func (l *dbRevertLog) CompleteFailed(ctx context.Context, opID OperationID, result *ApplyResult) error {
	if opID == "" {
		return nil
	}
	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		//nolint:nilerr // non-parseable opID is not an error (e.g. noOpRevertLog returns "")
		return nil
	}
	recordID := uint(recordID64)

	release := replacementLedgerLocks.Acquire(opID)
	defer release()

	preRecord, err := l.repo.FindByID(ctx, recordID)
	if err != nil {
		return fmt.Errorf("revert log CompleteFailed: find record %s: %w", opID, err)
	}
	if preRecord == nil {
		return nil // genuinely not found — no error
	}
	// Fall back to a nil-result failure when there is no partial state to preserve.
	if result == nil {
		updatePostOrganize(preRecord, "", false, preRecord.OriginalDirPath, "")
		preRecord.RevertStatus = models.RevertStatusFailed
		// Wave-10: non-journal columns only (see Complete's nil-result path).
		// Wave-15: a concurrent revert suppresses this Failed mark
		// (persistNonJournalColumns warns + tolerates) instead of being
		// clobbered by it.
		if updateErr := l.persistNonJournalColumns(ctx, opID, preRecord); updateErr != nil {
			return fmt.Errorf("revert log CompleteFailed: mark record %s as failed: %w", opID, updateErr)
		}
		resolveLogger(l.logger).Warnf("[revert-log] Apply failed for %s — pre-record marked as incomplete", opID)
		return nil
	}

	sourceDir := preRecord.OriginalDirPath
	var newPath string
	var inPlaceRenamed bool
	var subtitles []models.SubtitleMove
	// codex P1 (PR #241): same duplicate-skip no-op journal rule as Complete —
	// a skipped duplicate persists NO primary-move NewPath even when a later
	// pipeline step fails, so reverting the failed loser row can never rename
	// the winner's video. Batch-2 F1 adds the PRE-PUBLICATION terminal: a
	// strategy execute failure before any publish (the released-claim class —
	// e.g. ForceUpdate with a vanished source) journals NO target fields
	// either, so reverting the failed owner is a pure no-op that can never
	// drag a promoted claimant's published bytes onto the owner's source.
	if org := result.OrganizeResult; org != nil && !noopJournal(result) {
		newPath = org.NewPath
		inPlaceRenamed = org.InPlaceRenamed
		for _, sr := range org.Subtitles {
			if sr.Moved || sr.Copied {
				subtitles = append(subtitles, sr.SubtitleMove)
			}
		}
		if org.FolderPath != "" && sourceDir == "" {
			sourceDir = org.OldDirectoryPath
		}
	}
	// codex P1 (PR #241): same generated-artifact gate as Complete — a skipped
	// duplicate owns no NFO/download paths; a populated one would be the
	// winner's shared artifact, never safe to journal onto the loser's row.
	// Batch-2 F1/F2: pre-publication terminals own none either.
	nfoPath, foundNFOPath, downloadPaths := result.NFOPath, result.FoundNFOPath, result.DownloadPaths
	if noopJournal(result) {
		nfoPath, foundNFOPath, downloadPaths = "", "", nil
	}
	// Wave-9 (codex review 4960250562 follow-up): same journal-transaction
	// routing as Complete — the merge must see the FRESH row, not the stale
	// preRecord snapshot. Wave-10: the follow-up column update below excludes
	// generated_files entirely, so a journal mutation committed after this tx
	// is no longer clobbered by the re-persist (UpdateJournalInTx owns that
	// column exclusively).
	mergedJournal, err := l.mergeJournalInTx(ctx, recordID, opID, "CompleteFailed",
		buildGeneratedFilesJSON(resolveLogger(l.logger), nfoPath, subtitles, downloadPaths), "")
	if err != nil {
		return err
	}

	if foundNFOPath != "" {
		preRecord.NFOPath = foundNFOPath
	}
	if nfoPath != "" && preRecord.NFOPath == "" {
		preRecord.NFOPath = nfoPath
	}
	updatePostOrganize(preRecord, newPath, inPlaceRenamed, sourceDir, mergedJournal)
	// codex P2 (PR #241 F2): a skipped duplicate that failed a LATER pipeline
	// step still mutated nothing — it owns no NewPath and journaled no
	// artifacts — so it finalizes as completed-noop exactly like Complete's
	// DuplicateSkipped path instead of lingering as an unanchored failed row
	// the reverter would probe at "" forever. Batch-2 F1/F2: plan rejections
	// (validation/conflict — incl. rejected intra-batch duplicates) and
	// pre-publish strategy failures join the identical terminal on the same
	// nothing-mutated ground. A PARTIAL publish (fsutil.PublishCompleted) is
	// flatly excluded: its failure landed bytes at the destination, so the
	// record stays failed AND keeps pointing at the shared path (revertable).
	preRecord.RevertStatus = models.RevertStatusFailed
	if noopJournal(result) {
		preRecord.RevertStatus = models.RevertStatusNoOp
	}
	if err := l.persistNonJournalColumns(ctx, opID, preRecord); err != nil {
		return fmt.Errorf("revert log CompleteFailed: update failed record for %s: %w", opID, err)
	}
	if preRecord.RevertStatus == models.RevertStatusFailed {
		resolveLogger(l.logger).Warnf("[revert-log] Apply failed for %s after filesystem mutation — record kept revertable (NewPath=%q)", opID, newPath)
	}
	return nil
}

// replacementLedgerLocks serializes read-modify-writes per operation row
// across every ledger mutation (record/release/confirm/seed/complete) and
// the sweeper's consumption — ALL parties hold the SAME process registry
// (codex P3 R15-1).
var replacementLedgerLocks = fsutil.SharedJournalLocks()

// RecordReplacement journals one replaced byte pair onto the operation row.
// The caller (downloader installOverwriting) already holds the
// per-destination lock and has moved the pre-existing bytes aside — the
// per-destination sequence assigned here is therefore the true replace order
// within this process; the sequence floor is read back from the database so
// it stays monotonic across restarts.
func (l *dbRevertLog) RecordReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string, backupFacts ...models.ReplacementBackupFacts) error {
	if opID == "" {
		return fmt.Errorf("revert log RecordReplacement: empty operation ID")
	}
	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		return fmt.Errorf("revert log RecordReplacement: unparsable operation ID %q", opID)
	}

	release := replacementLedgerLocks.Acquire(opID)
	defer release()

	// The sequence floor is computed OUTSIDE the row transaction: per-destination
	// .dlbusy markers already exclude any cross-process armer of THIS
	// destination, and the floor depends only on other rows for the same
	// destination — a concurrent foreign-destination append to this row cannot
	// shift it.
	seq, err := nextDestSequence(ctx, l.repo, replacedPath)
	if err != nil {
		return fmt.Errorf("revert log RecordReplacement: sequence for %s: %w", replacedPath, err)
	}

	// Review 4960250562: the append merges against the row re-read INSIDE a
	// BEGIN IMMEDIATE transaction, so a revert/sweep consuming a different
	// destination of this row in another process can no longer be resurrected
	// by a stale snapshot here (nor clobber this arm).
	var fnErr error
	txErr := l.repo.UpdateJournalInTx(ctx, uint(recordID64), func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		// Legacy tolerance: rows written before the journal existed (or with no
		// ledger at all) start from the zero value; malformed content still
		// refuses rather than silently dropping the persisted ledger.
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if perr != nil {
			fnErr = fmt.Errorf("revert log RecordReplacement: parse ledger for record %s: %w", opID, perr)
			return models.GeneratedFilesJSON{}, false, fnErr
		}
		// Wave-25: stamp the set-aside backup's identity facts into the same
		// journal write — the removal gate (history removeReplacementBackup)
		// later binds its unlink to these facts, so an unstamped entry reads as
		// legacy while a stamped entry can only ever name the OWNED object.
		var facts models.ReplacementBackupFacts
		if len(backupFacts) > 0 {
			facts = backupFacts[0]
		}
		gf.Replacements = append(gf.Replacements, models.ReplacementEntry{
			Destination:   replacedPath,
			Backup:        backupPath,
			DestSeq:       seq,
			BackupSize:    facts.Size,
			BackupModUnix: facts.ModUnix,
			BackupSHA256:  facts.SHA256,
		})
		return gf, true, nil
	})
	switch {
	case fnErr != nil:
		return fnErr
	case errors.Is(txErr, database.ErrNotFound):
		return fmt.Errorf("revert log RecordReplacement: record %s not found", opID)
	case txErr != nil:
		return fmt.Errorf("revert log RecordReplacement: persist record %s: %w", opID, txErr)
	}
	return nil
}

// ReleaseReplacement removes the journaled entry for a backup the downloader
// rolled back onto its destination. Missing entries are tolerated (idempotent
// rollback); a missing row is not.
func (l *dbRevertLog) ReleaseReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string) error {
	if opID == "" {
		return fmt.Errorf("revert log ReleaseReplacement: empty operation ID")
	}
	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		return fmt.Errorf("revert log ReleaseReplacement: unparsable operation ID %q", opID)
	}

	release := replacementLedgerLocks.Acquire(opID)
	defer release()

	var fnErr error
	txErr := l.repo.UpdateJournalInTx(ctx, uint(recordID64), func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if perr != nil {
			fnErr = fmt.Errorf("revert log ReleaseReplacement: parse ledger for record %s: %w", opID, perr)
			return models.GeneratedFilesJSON{}, false, fnErr
		}
		kept := gf.Replacements[:0]
		for _, e := range gf.Replacements {
			if e.Destination == replacedPath && e.Backup == backupPath {
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == len(gf.Replacements) {
			return gf, false, nil // entry already gone (e.g. sweep consumed it) — idempotent
		}
		gf.Replacements = kept
		return gf, true, nil
	})
	switch {
	case fnErr != nil:
		return fnErr
	case errors.Is(txErr, database.ErrNotFound):
		return fmt.Errorf("revert log ReleaseReplacement: record %s not found", opID)
	case txErr != nil:
		return fmt.Errorf("revert log ReleaseReplacement: persist record %s: %w", opID, txErr)
	}
	return nil
}

// MarkReplacementRestorePending durably marks the matching journal entry
// restore-pending with the wave-19 rearm-refused kind (codex P2 PR#215): the
// downloader's rollback already restored the destination bytes but the
// backup re-arm was REFUSED with the occupied-name classes
// (fsutil.PublishRefusal) — the name is foreign-occupied or absent. The
// marker certifies the destination and keeps every retry off the unowned
// name. The kind-carrying generalization lives in
// MarkReplacementRestorePendingKind; this is the wave-19 shorthand it
// delegates to.
func (l *dbRevertLog) MarkReplacementRestorePending(ctx context.Context, opID OperationID, replacedPath, backupPath string) error {
	return l.MarkReplacementRestorePendingKind(ctx, opID, replacedPath, backupPath, models.RestorePendingKindRearmRefused)
}

// MarkReplacementRestorePendingKind is MarkReplacementRestorePending with an
// explicit restore-pending kind (wave-21, codex P2 PR#215). The downloader's
// rollback re-arm disarms the journaled entry for EVERY failure class, with
// the kind routing the retry: models.RestorePendingKindRearmRefused for an
// unowned (foreign-occupied or absent) backup name — retries consume
// journal-only — and models.RestorePendingKindClean when the failed re-arm
// demonstrably published this operation's own bytes at the name (the
// fsutil.PublishCompleted class — retries reap it first). Matching follows
// the downloader seam's exact-spelling convention (same as
// Record/Release/ConfirmReplacement); a missing entry is tolerated
// (idempotent, exactly like ReleaseReplacement), a missing row is not. The
// merge discipline (one-way upgrade, never a downgrade to clean) lives in
// models.ReplacementEntry.SetRestorePending. Any other kind is rejected:
// this build must never persist a marker whose routing it cannot interpret.
func (l *dbRevertLog) MarkReplacementRestorePendingKind(ctx context.Context, opID OperationID, replacedPath, backupPath, kind string) error {
	if opID == "" {
		return fmt.Errorf("revert log MarkReplacementRestorePending: empty operation ID")
	}
	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		return fmt.Errorf("revert log MarkReplacementRestorePending: unparsable operation ID %q", opID)
	}
	switch kind {
	case models.RestorePendingKindClean, models.RestorePendingKindRearmRefused:
	default:
		return fmt.Errorf("revert log MarkReplacementRestorePending: unknown restore-pending kind %q", kind)
	}

	release := replacementLedgerLocks.Acquire(opID)
	defer release()

	var fnErr error
	txErr := l.repo.UpdateJournalInTx(ctx, uint(recordID64), func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if perr != nil {
			fnErr = fmt.Errorf("revert log MarkReplacementRestorePending: parse ledger for record %s: %w", opID, perr)
			return models.GeneratedFilesJSON{}, false, fnErr
		}
		for i := range gf.Replacements {
			e := &gf.Replacements[i]
			if e.Destination == replacedPath && e.Backup == backupPath {
				if !e.SetRestorePending(kind) {
					return gf, false, nil // already carries the mark at this kind — idempotent
				}
				return gf, true, nil
			}
		}
		return gf, false, nil // entry already gone (e.g. consumed meanwhile) — idempotent
	})
	switch {
	case fnErr != nil:
		return fnErr
	case errors.Is(txErr, database.ErrNotFound):
		return fmt.Errorf("revert log MarkReplacementRestorePending: record %s not found", opID)
	case txErr != nil:
		return fmt.Errorf("revert log MarkReplacementRestorePending: persist record %s: %w", opID, txErr)
	}
	return nil
}

// ConfirmReplacement flips the matching journal entry to installed.
func (l *dbRevertLog) ConfirmReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string) error {
	if opID == "" {
		return fmt.Errorf("revert log ConfirmReplacement: empty operation ID")
	}
	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		return fmt.Errorf("revert log ConfirmReplacement: unparsable operation ID %q", opID)
	}

	release := replacementLedgerLocks.Acquire(opID)
	defer release()

	var fnErr error
	txErr := l.repo.UpdateJournalInTx(ctx, uint(recordID64), func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if perr != nil {
			fnErr = fmt.Errorf("revert log ConfirmReplacement: parse ledger for record %s: %w", opID, perr)
			return models.GeneratedFilesJSON{}, false, fnErr
		}
		changed := false
		for i := range gf.Replacements {
			e := &gf.Replacements[i]
			if e.Destination == replacedPath && e.Backup == backupPath && !e.Installed {
				e.Installed = true
				changed = true
			}
		}
		if !changed {
			return gf, false, nil // entry already confirmed or retracted — idempotent
		}
		return gf, true, nil
	})
	switch {
	case fnErr != nil:
		return fnErr
	case errors.Is(txErr, database.ErrNotFound):
		return fmt.Errorf("revert log ConfirmReplacement: record %s not found", opID)
	case txErr != nil:
		return fmt.Errorf("revert log ConfirmReplacement: persist record %s: %w", opID, txErr)
	}
	return nil
}

// seedRoot appends a discovery root to the operation ledger (dedup). R12-2:
// failures SURFACE (silent log-and-proceed would arm a destructive download
// whose pre-journal crash window the startup sweep cannot discover). Used
// by the orchestrator right after organize: media land in the organizer's
// leaf folder — possibly nested beyond the sweeper's walk bound (codex P3
// R7-3) — so the exact folder is recorded while the run is still alive,
// closing the pre-Complete discovery gap entirely.
func (l *dbRevertLog) seedRoot(ctx context.Context, opID OperationID, root string) error {
	if opID == "" || root == "" {
		return nil
	}
	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		return nil
	}
	release := replacementLedgerLocks.Acquire(opID)
	defer release()
	// Review 4960250562: root seeding rides the same transaction as every other
	// journal mutation. appendLedgerRoot's tolerances are preserved in struct
	// form: a malformed body is left byte-identical (no write), an existing root
	// dedups to a no-op, otherwise the re-marshaled ledger carries the new root.
	txErr := l.repo.UpdateJournalInTx(ctx, uint(recordID64), func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if perr != nil {
			// Deliberate tolerance (Review 4960250562 note above): a malformed
			// ledger body is left byte-identical and seeding dedups to a no-op
			// rather than failing the journal transaction.
			//nolint:nilerr // intentional: malformed ledgers skip the write instead of failing seedRoot.
			return models.GeneratedFilesJSON{}, false, nil
		}
		for _, r := range gf.Roots {
			if r == root {
				return gf, false, nil
			}
		}
		gf.Roots = append(gf.Roots, root)
		return gf, true, nil
	})
	switch {
	case errors.Is(txErr, database.ErrNotFound):
		return fmt.Errorf("revert log seedRoot: record %s not found", opID)
	case txErr != nil:
		return fmt.Errorf("revert log seedRoot: persist root %s for %s: %w", root, opID, txErr)
	}
	return nil
}

// nextDestSequence returns the next per-destination sequence: the maximum
// DestSeq already journaled for this destination across ALL operations
// (applied and failed rows both count — a failed record's backups are still
// restorable), plus one. Restart-persistent because it derives from rows.
// Audit: this workflow grouping follows fsutil's platform separator seam;
// POSIX journals use `/` spellings and keep literal backslashes distinct,
// while Windows legacy slash/backslash spellings remain one destination.
func nextDestSequence(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, destination string) (int64, error) {
	rows, err := repo.FindOperationsByDestination(ctx, destination)
	if err != nil {
		return 0, err
	}
	var maxSeq int64
	for i := range rows {
		gf, err := models.ParseGeneratedFiles(rows[i].GeneratedFiles)
		if err != nil {
			continue // unparsable rows contribute no sequence floor
		}
		for _, rep := range gf.Replacements {
			if fsutil.DestKey(rep.Destination) == fsutil.DestKey(destination) && rep.DestSeq > maxSeq {
				maxSeq = rep.DestSeq
			}
		}
	}
	return maxSeq + 1, nil
}

// NewRevertLogFromConfig creates the appropriate RevertLog based on config.
//
// Operation recording is independent of the AllowRevert toggle: BatchFileOperation
// records are always persisted (when a repository is available) so that the
// operations list and per-file history remain visible even when revert is not
// opted in. config.Output.Operation.AllowRevert gates only the revert *action*,
// which is enforced separately by the revert/revert-check HTTP handlers (they
// return 403 when AllowRevert is false).
//
// Returns noOpRevertLog only when there is no repository or no config to write
// to (defensive — callers in production always pass non-nil repo and cfg).
func NewRevertLogFromConfig(repo database.BatchFileOperationRepositoryInterface, cfg *RevertLogConfig, jobID string, fs afero.Fs, templateEngine template.EngineInterface, nfoFieldMerger nfo.NFOFieldMerger, logger logging.Logger) RevertLog {
	if cfg == nil || repo == nil {
		return noOpRevertLog{}
	}
	return NewDBRevertLog(repo, cfg, jobID, fs, templateEngine, nfoFieldMerger, logger)
}
