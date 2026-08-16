package workflow

import (
	"context"
	"encoding/json"
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
	RecordReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string) error

	// ReleaseReplacement retracts a journal entry the downloader rolled back
	// itself (record landed, install failed, backup restored over the
	// destination). The row must not keep pointing at the consumed backup.
	ReleaseReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string) error

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

func (noOpRevertLog) RecordReplacement(_ context.Context, _ OperationID, _, _ string) error {
	// No durable store — journal nothing. Callers must not arm the downloader
	// ledger with a no-op recorder: workflow threads the recorder only when the
	// concrete RevertLog is the DB-backed implementation (see replacementRecorder).
	return nil
}

func (noOpRevertLog) ReleaseReplacement(_ context.Context, _ OperationID, _, _ string) error {
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
func appendLedgerRoot(logger logging.Logger, raw, root string) string {
	if raw == "" {
		data, err := json.Marshal(models.GeneratedFilesJSON{Roots: []string{root}})
		if err != nil {
			return raw
		}
		return string(data)
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
	data, err := json.Marshal(gf)
	if err != nil {
		logger.Warnf("Failed to append ledger root %s: %v", root, err)
		return raw
	}
	return string(data)
}

func mergeReplacementLedger(logger logging.Logger, priorRaw, newRaw string) string {
	if priorRaw == "" {
		return newRaw
	}
	prior, err := models.ParseGeneratedFiles(priorRaw)
	if err != nil || (len(prior.Replacements) == 0 && len(prior.Roots) == 0) {
		return newRaw
	}
	if newRaw == "" {
		data, mErr := json.Marshal(models.GeneratedFilesJSON{Replacements: prior.Replacements, Roots: prior.Roots})
		if mErr != nil {
			logger.Warnf("Failed to marshal replacement-only generatedFilesJSON: %v", mErr)
			return newRaw
		}
		return string(data)
	}
	fresh, err := models.ParseGeneratedFiles(newRaw)
	if err != nil {
		return newRaw
	}
	fresh.Replacements = prior.Replacements
	if len(fresh.Roots) == 0 {
		fresh.Roots = prior.Roots
	}
	data, mErr := json.Marshal(fresh)
	if mErr != nil {
		logger.Warnf("Failed to re-marshal merged generatedFilesJSON: %v", mErr)
		return newRaw
	}
	return string(data)
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
			if sr.Moved && sr.OriginalPath != "" && sr.NewPath != "" {
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

	if updateErr := l.repo.Update(ctx, preRecord); updateErr != nil {
		resolveLogger(l.logger).Warnf("[revert-log] CaptureSnapshot: failed to update record %s: %v", opID, updateErr)
	}
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
		if updateErr := l.repo.Update(ctx, preRecord); updateErr != nil {
			return fmt.Errorf("revert log Complete: mark record %s as failed: %w", opID, updateErr)
		}
		resolveLogger(l.logger).Warnf("[revert-log] Apply failed for %s — pre-record marked as incomplete", opID)
		return nil
	}

	sourceDir := preRecord.OriginalDirPath
	var newPath string
	var inPlaceRenamed bool
	var subtitles []models.SubtitleMove
	if result.OrganizeResult != nil {
		newPath = result.OrganizeResult.NewPath
		inPlaceRenamed = result.OrganizeResult.InPlaceRenamed
		for _, sr := range result.OrganizeResult.Subtitles {
			if sr.Moved {
				subtitles = append(subtitles, sr.SubtitleMove)
			}
		}
		if result.OrganizeResult.FolderPath != "" && sourceDir == "" {
			sourceDir = result.OrganizeResult.OldDirectoryPath
		}
	}

	generatedFilesJSON := mergeReplacementLedger(resolveLogger(l.logger), preRecord.GeneratedFiles, buildGeneratedFilesJSON(resolveLogger(l.logger), result.NFOPath, subtitles, result.DownloadPaths))
	// R4-2: media actually lands in the organizer's leaf folder — add it to
	// the seeded roots so the sweeper's bounded recursion starts there.
	if result.OrganizeResult != nil && result.OrganizeResult.FolderPath != "" {
		generatedFilesJSON = appendLedgerRoot(resolveLogger(l.logger), generatedFilesJSON, result.OrganizeResult.FolderPath)
	}

	if result.FoundNFOPath != "" {
		preRecord.NFOPath = result.FoundNFOPath
	}
	if result.NFOPath != "" && preRecord.NFOPath == "" {
		preRecord.NFOPath = result.NFOPath
	}

	updatePostOrganize(preRecord, newPath, inPlaceRenamed, sourceDir, generatedFilesJSON)
	if err := l.repo.Update(ctx, preRecord); err != nil {
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
		if updateErr := l.repo.Update(ctx, preRecord); updateErr != nil {
			return fmt.Errorf("revert log CompleteFailed: mark record %s as failed: %w", opID, updateErr)
		}
		resolveLogger(l.logger).Warnf("[revert-log] Apply failed for %s — pre-record marked as incomplete", opID)
		return nil
	}

	sourceDir := preRecord.OriginalDirPath
	var newPath string
	var inPlaceRenamed bool
	var subtitles []models.SubtitleMove
	if result.OrganizeResult != nil {
		newPath = result.OrganizeResult.NewPath
		inPlaceRenamed = result.OrganizeResult.InPlaceRenamed
		for _, sr := range result.OrganizeResult.Subtitles {
			if sr.Moved {
				subtitles = append(subtitles, sr.SubtitleMove)
			}
		}
		if result.OrganizeResult.FolderPath != "" && sourceDir == "" {
			sourceDir = result.OrganizeResult.OldDirectoryPath
		}
	}
	generatedFilesJSON := mergeReplacementLedger(resolveLogger(l.logger), preRecord.GeneratedFiles, buildGeneratedFilesJSON(resolveLogger(l.logger), result.NFOPath, subtitles, result.DownloadPaths))
	if result.FoundNFOPath != "" {
		preRecord.NFOPath = result.FoundNFOPath
	}
	if result.NFOPath != "" && preRecord.NFOPath == "" {
		preRecord.NFOPath = result.NFOPath
	}
	updatePostOrganize(preRecord, newPath, inPlaceRenamed, sourceDir, generatedFilesJSON)
	preRecord.RevertStatus = models.RevertStatusFailed
	if err := l.repo.Update(ctx, preRecord); err != nil {
		return fmt.Errorf("revert log CompleteFailed: update failed record for %s: %w", opID, err)
	}
	resolveLogger(l.logger).Warnf("[revert-log] Apply failed for %s after filesystem mutation — record kept revertable (NewPath=%q)", opID, newPath)
	return nil
}

// replacementLedgerLocks serializes RecordReplacement appends per operation row
// so concurrent downloader goroutines journaling onto one row never lose an
// append (load-modify-write under the keyed lock).
var replacementLedgerLocks = fsutil.NewKeyedLockRegistry()

// RecordReplacement journals one replaced byte pair onto the operation row.
// The caller (downloader installOverwriting) already holds the
// per-destination lock and has moved the pre-existing bytes aside — the
// per-destination sequence assigned here is therefore the true replace order
// within this process; the sequence floor is read back from the database so
// it stays monotonic across restarts.
func (l *dbRevertLog) RecordReplacement(ctx context.Context, opID OperationID, replacedPath, backupPath string) error {
	if opID == "" {
		return fmt.Errorf("revert log RecordReplacement: empty operation ID")
	}
	recordID64, err := strconv.ParseUint(opID, 10, 64)
	if err != nil || recordID64 == 0 {
		return fmt.Errorf("revert log RecordReplacement: unparsable operation ID %q", opID)
	}

	release := replacementLedgerLocks.Acquire(opID)
	defer release()

	preRecord, err := l.repo.FindByID(ctx, uint(recordID64))
	if err != nil {
		return fmt.Errorf("revert log RecordReplacement: find record %s: %w", opID, err)
	}
	if preRecord == nil {
		return fmt.Errorf("revert log RecordReplacement: record %s not found", opID)
	}

	// Legacy tolerance: rows written before the journal existed (or with no
	// ledger at all) start from the zero value.
	gf, err := models.ParseGeneratedFiles(preRecord.GeneratedFiles)
	if err != nil {
		return fmt.Errorf("revert log RecordReplacement: parse ledger for record %s: %w", opID, err)
	}

	seq, err := nextDestSequence(ctx, l.repo, replacedPath)
	if err != nil {
		return fmt.Errorf("revert log RecordReplacement: sequence for %s: %w", replacedPath, err)
	}
	gf.Replacements = append(gf.Replacements, models.ReplacementEntry{
		Destination: replacedPath,
		Backup:      backupPath,
		DestSeq:     seq,
	})

	data, err := json.Marshal(gf)
	if err != nil {
		return fmt.Errorf("revert log RecordReplacement: marshal ledger for record %s: %w", opID, err)
	}
	preRecord.GeneratedFiles = string(data)
	if err := l.repo.Update(ctx, preRecord); err != nil {
		return fmt.Errorf("revert log RecordReplacement: persist record %s: %w", opID, err)
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

	preRecord, err := l.repo.FindByID(ctx, uint(recordID64))
	if err != nil {
		return fmt.Errorf("revert log ReleaseReplacement: find record %s: %w", opID, err)
	}
	if preRecord == nil {
		return fmt.Errorf("revert log ReleaseReplacement: record %s not found", opID)
	}
	gf, err := models.ParseGeneratedFiles(preRecord.GeneratedFiles)
	if err != nil {
		return fmt.Errorf("revert log ReleaseReplacement: parse ledger for record %s: %w", opID, err)
	}
	kept := gf.Replacements[:0]
	for _, e := range gf.Replacements {
		if e.Destination == replacedPath && e.Backup == backupPath {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == len(gf.Replacements) {
		return nil // entry already gone (e.g. sweep consumed it) — idempotent
	}
	gf.Replacements = kept
	data, err := json.Marshal(gf)
	if err != nil {
		return fmt.Errorf("revert log ReleaseReplacement: marshal ledger for record %s: %w", opID, err)
	}
	preRecord.GeneratedFiles = string(data)
	if err := l.repo.Update(ctx, preRecord); err != nil {
		return fmt.Errorf("revert log ReleaseReplacement: persist record %s: %w", opID, err)
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

	preRecord, err := l.repo.FindByID(ctx, uint(recordID64))
	if err != nil {
		return fmt.Errorf("revert log ConfirmReplacement: find record %s: %w", opID, err)
	}
	if preRecord == nil {
		return fmt.Errorf("revert log ConfirmReplacement: record %s not found", opID)
	}
	gf, err := models.ParseGeneratedFiles(preRecord.GeneratedFiles)
	if err != nil {
		return fmt.Errorf("revert log ConfirmReplacement: parse ledger for record %s: %w", opID, err)
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
		return nil // entry already confirmed or retracted — idempotent
	}
	data, err := json.Marshal(gf)
	if err != nil {
		return fmt.Errorf("revert log ConfirmReplacement: marshal ledger for record %s: %w", opID, err)
	}
	preRecord.GeneratedFiles = string(data)
	if err := l.repo.Update(ctx, preRecord); err != nil {
		return fmt.Errorf("revert log ConfirmReplacement: persist record %s: %w", opID, err)
	}
	return nil
}

// nextDestSequence returns the next per-destination sequence: the maximum
// DestSeq already journaled for this destination across ALL operations
// (applied and failed rows both count — a failed record's backups are still
// restorable), plus one. Restart-persistent because it derives from rows.
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
			if rep.Destination == destination && rep.DestSeq > maxSeq {
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
