package history

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
)

// NFOSnapshotResult holds the result of reading an NFO snapshot.
type NFOSnapshotResult struct {
	Content   string // NFO file content (empty if no NFO found)
	FoundPath string // Canonical path where the NFO was found (empty if not found)
}

// ReadNFOSnapshot reads existing NFO content by trying each candidate path in order.
// Returns an NFOSnapshotResult with the content and the canonical path of the first
// file that exists. Returns empty Content and FoundPath if no file exists.
// Uses filepath.Abs + filepath.Clean for path canonicalization (T-03-01).
func ReadNFOSnapshot(fs afero.Fs, candidatePaths ...string) NFOSnapshotResult {
	for _, p := range candidatePaths {
		if p == "" {
			continue
		}
		canonical, err := filepath.Abs(filepath.Clean(p))
		if err != nil {
			continue
		}
		data, err := afero.ReadFile(fs, canonical)
		if err == nil {
			return NFOSnapshotResult{Content: string(data), FoundPath: canonical}
		}
		if !os.IsNotExist(err) {
			logging.Warnf("Failed to read NFO snapshot from %q: %v", canonical, err)
		}
	}
	return NFOSnapshotResult{}
}

// DetermineOperationType maps organize flags to operation type constants.
func DetermineOperationType(moveFiles bool, linkMode organizer.LinkMode, isUpdateMode bool) string {
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

// NewPreOrganizeRecord creates a BatchFileOperation with NFO snapshot
// for crash-safe persistence BEFORE organize (per D-01).
// Does NOT call repo.Create — caller is responsible for persisting
// (gives control over timing for crash-safety).
func NewPreOrganizeRecord(batchJobID, movieID, originalPath, nfoSnapshot, nfoPath, originalDirPath, operationType string, inPlaceRenamed bool) *models.BatchFileOperation {
	return &models.BatchFileOperation{
		BatchJobID:      batchJobID,
		MovieID:         movieID,
		OriginalPath:    originalPath,
		NewPath:         "", // filled after organize
		OperationType:   operationType,
		NFOSnapshot:     nfoSnapshot,
		NFOPath:         nfoPath,
		GeneratedFiles:  "", // filled after post-organize ops (D-03)
		RevertStatus:    models.RevertStatusApplied,
		InPlaceRenamed:  inPlaceRenamed,
		OriginalDirPath: originalDirPath,
	}
}

// BuildGeneratedFilesJSON creates JSON with Delete list (NFO path, image paths)
// and MoveBack list (subtitle paths) for the GeneratedFiles field.
// Returns "" on empty or marshal error.
func BuildGeneratedFilesJSON(nfoPath string, subtitleResults []organizer.SubtitleResult, downloadPaths []string) string {
	gf := GeneratedFilesJSON{}

	// Build Delete list: NFO + downloaded files
	deleteList := make([]string, 0, 1+len(downloadPaths))
	if nfoPath != "" {
		deleteList = append(deleteList, nfoPath)
	}
	deleteList = append(deleteList, downloadPaths...)
	if len(deleteList) > 0 {
		gf.Delete = deleteList
	}

	// Build MoveBack list: subtitle results
	if len(subtitleResults) > 0 {
		moveBackList := make([]FileMove, 0, len(subtitleResults))
		for _, sr := range subtitleResults {
			if sr.Moved && sr.OriginalPath != "" && sr.NewPath != "" {
				moveBackList = append(moveBackList, FileMove{
					OriginalPath: sr.OriginalPath,
					NewPath:      sr.NewPath,
				})
			}
		}
		if len(moveBackList) > 0 {
			gf.MoveBack = moveBackList
		}
	}

	// Return empty string if nothing to track
	if len(gf.Delete) == 0 && len(gf.MoveBack) == 0 {
		return ""
	}

	data, err := json.Marshal(gf)
	if err != nil {
		logging.Warnf("Failed to marshal GeneratedFilesJSON: %v", err)
		return ""
	}
	return string(data)
}

// UpdatePostOrganize updates the BatchFileOperation in-place (the struct, not the DB):
//   - op.NewPath = newPath
//   - op.InPlaceRenamed = inPlaceRenamed
//   - op.OriginalDirPath = originalDirPath
//   - op.GeneratedFiles = generatedFilesJSON
//
// Caller persists via repo.Update after calling this.
func UpdatePostOrganize(op *models.BatchFileOperation, newPath string, inPlaceRenamed bool, originalDirPath string, generatedFilesJSON string) {
	op.NewPath = newPath
	op.InPlaceRenamed = inPlaceRenamed
	op.OriginalDirPath = originalDirPath
	op.GeneratedFiles = generatedFilesJSON
}
