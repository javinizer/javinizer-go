package batch

import (
	"context"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
)

const (
	// minScanTimeout is the minimum timeout duration for directory scans.
	// This prevents immediate context cancellation when ScanTimeoutSeconds is 0 or negative.
	minScanTimeout = 5 * time.Second
)

// isDirAllowed checks if a directory is allowed based on API security settings.
// It delegates to core.PathValidator (constructed with the full security config,
// including Windows UNC policy) which enforces both denied (blocklist) and allowed
// (allowlist) directory rules, the built-in denylist, and the UNC gate.
func isDirAllowed(fs afero.Fs, dir string, secCfg *core.SecurityNarrowConfig) bool {
	v := core.NewPathValidatorWithUNC(fs, secCfg.AllowedDirectories, secCfg.DeniedDirectories, secCfg.AllowUNC, secCfg.AllowedUNCServers)
	if !v.IsUNCAllowed(dir) {
		return false
	}
	return v.IsDirAllowed(dir)
}

// discoverSiblingPartsWithMetadata finds all multi-part files and returns match metadata.
// This preserves multipart info (IsMultiPart, PartNumber, PartSuffix) from the discovery phase
// so it's available when creating FileResults during scraping.
// Uses the ScanAndMatch seam to avoid direct scanner/matcher imports.
//
// The snapshot pins a consistent reload epoch so the workflow used for sibling
// discovery matches the security/scanner config the caller read from the same
// snapshot (issue #44).
func discoverSiblingPartsWithMetadata(ctx context.Context, files []string, snap *core.RuntimeSnapshot, secCfg *core.SecurityNarrowConfig, scanCfg *core.ScannerNarrowConfig) ([]string, map[string]models.FileMatchInfo) {
	if len(files) == 0 {
		return files, nil
	}

	deps := snap.RT().Deps()
	wf, wfErr := snap.BatchWorkflow("")
	if wfErr != nil {
		logging.Warnf("Failed to create workflow for sibling discovery: %v, using original files only", wfErr)
		return files, nil
	}

	seenPaths := make(map[string]bool)
	fileMatchInfo := make(map[string]models.FileMatchInfo)
	dirsToScan := make(map[string]bool)

	for _, filePath := range files {
		seenPaths[filePath] = true
		dirsToScan[filepath.Dir(filePath)] = true
	}

	fs := deps.GetFs()

	// Scan each directory once and collect both submitted-file metadata
	// and multi-part sibling files from the same results.
	movieIDsToProcess := make(map[string]bool)

	// dirResults caches scan results per directory so we can reuse them
	// for sibling discovery without re-scanning.
	type dirScan struct {
		dir   string
		files []models.FileMatchInfo
	}
	var dirScans []dirScan

	for dir := range dirsToScan {
		if !isDirAllowed(fs, dir, secCfg) {
			logging.Debugf("Skipping sibling discovery in disallowed directory: %s", dir)
			continue
		}

		timeout := time.Duration(scanCfg.ScanTimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = minScanTimeout
		}

		scanResult, err := wf.ScanAndMatch(ctx, workflow.ScanAndMatchCmd{
			Directory:      dir,
			Recursive:      false,
			TimeoutSeconds: int(timeout.Seconds()),
			MaxFiles:       scanCfg.MaxFilesPerScan,
		})
		if err != nil {
			logging.Debugf("Failed to scan directory %s for siblings: %v", dir, err)
			continue
		}

		dirScans = append(dirScans, dirScan{dir: dir, files: scanResult.Files})

		// Collect match metadata for submitted files and detect multi-part status
		for _, fmi := range scanResult.Files {
			if seenPaths[fmi.Path] {
				fileMatchInfo[fmi.Path] = fmi
				if fmi.IsMultiPart && fmi.MovieID != "" {
					movieIDsToProcess[fmi.MovieID] = true
					logging.Debugf("Detected multi-part file: %s (movie ID: %s, part: %d)",
						fmi.Name, fmi.MovieID, fmi.PartNumber)
				}
			}
		}
	}

	// If no multi-part files detected, return original list with metadata
	if len(movieIDsToProcess) == 0 {
		return files, fileMatchInfo
	}

	// Start with original files
	allFiles := make([]string, 0, len(files))
	allFiles = append(allFiles, files...)

	// Find siblings using the already-cached scan results — no re-scanning needed
	for _, ds := range dirScans {
		for _, fmi := range ds.files {
			if fmi.MovieID != "" && movieIDsToProcess[fmi.MovieID] && fmi.IsMultiPart {
				if !seenPaths[fmi.Path] {
					// Sanity check: Ensure file is actually in the scanned directory
					parent := filepath.Dir(fmi.Path)
					if filepath.Clean(parent) != filepath.Clean(ds.dir) {
						logging.Warnf("Scanner returned file outside scanned directory: %s (expected: %s)", parent, ds.dir)
						continue
					}

					seenPaths[fmi.Path] = true
					allFiles = append(allFiles, fmi.Path)
					logging.Infof("Auto-discovered multi-part sibling: %s (movie ID: %s, part: %d)",
						fmi.Name, fmi.MovieID, fmi.PartNumber)
				}
				// Add metadata for discovered files too
				fileMatchInfo[fmi.Path] = fmi
			}
		}
	}

	return allFiles, fileMatchInfo
}
