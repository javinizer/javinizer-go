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

// generatedFilesJSON and fileMove moved to internal/models/revert_types.go per ADR-0034.
// The workflow package imports models.GeneratedFilesJSON and models.FileMove instead of
// defining duplicate types.
//
// generatedFilesJSON mirrors the history package structure so workflow stays dependency-free.
// Per ADR-0034: no longer mirrors — both import from models.

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
// Per ADR-0033: Begin writes a database record with movie ID, original path, and operation
// type — no filesystem access. CaptureSnapshot reads the NFO and updates the record.
// If CaptureSnapshot fails, Begin's record still exists — Apply proceeds with partial revert
// safety. The NFO snapshot is optional enrichment, not a precondition.
type RevertLog interface {
	// Begin persists a pre-mutation record before Apply starts filesystem changes.
	// Returns an OperationID for correlating with the revert record.
	// Must be called BEFORE any filesystem mutation.
	// Per ADR-0033: pure database write — no filesystem I/O.
	Begin(ctx context.Context, cmd ApplyCmd) (OperationID, error)

	// CaptureSnapshot reads the existing NFO file and updates the revert record
	// with the snapshot content. Call after Begin, before filesystem mutation.
	// If snapshot fails, the revert record still exists — partial safety is better
	// than none. Per ADR-0033: filesystem I/O separated from Begin's DB write.
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
func (c *RevertLogConfig) ToNFONameConfig(isMultiPart bool, partSuffix string) nfo.NFONameConfig {
	if c.NFOCfg != nil {
		return c.NFOCfg.ToNFONameConfig(isMultiPart, partSuffix)
	}
	return nfo.NFONameConfig{
		IsMultiPart: isMultiPart,
		PartSuffix:  partSuffix,
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
// Per ADR-0033: Begin is a pure database write — no filesystem I/O.
func (l *dbRevertLog) Begin(ctx context.Context, cmd ApplyCmd) (OperationID, error) {
	if cmd.Movie == nil {
		return "", nil
	}

	isUpdateMode := cmd.Organize.Skip
	opType := determineOperationType(!cmd.Organize.Skip && cmd.Organize.MoveFiles, cmd.Organize.LinkMode, isUpdateMode)

	sourceDir := filepath.Dir(cmd.Match.Path)

	// Per ADR-0033: write the DB record without NFO snapshot.
	// CaptureSnapshot will fill in the snapshot content separately.
	preRecord := newPreOrganizeRecord(
		l.jobID, cmd.Movie.ID, cmd.Match.Path,
		"", "", sourceDir, // no snapshot yet
		opType, false,
	)
	if err := l.repo.Create(ctx, preRecord); err != nil {
		return "", fmt.Errorf("revert log Begin failed: %w", err)
	}

	return fmt.Sprintf("%d", preRecord.ID), nil
}

// CaptureSnapshot reads the existing NFO file and updates the revert record
// with the snapshot content. Per ADR-0033: filesystem I/O separated from Begin's
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
		nameCfg = l.cfg.ToNFONameConfig(isMultiPart, partSuffix)
	} else {
		nameCfg = nfo.NFONameConfig{
			IsMultiPart: isMultiPart,
			PartSuffix:  partSuffix,
		}
	}

	// Per ADR-0045: resolve NFO paths through the NFOFieldMerger seam instead of
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

	generatedFilesJSON := buildGeneratedFilesJSON(resolveLogger(l.logger), result.NFOPath, subtitles, result.DownloadPaths)

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
	generatedFilesJSON := buildGeneratedFilesJSON(resolveLogger(l.logger), result.NFOPath, subtitles, result.DownloadPaths)
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
