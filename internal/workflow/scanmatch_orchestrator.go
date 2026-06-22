package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
)

type scanAndMatchOrchestrator interface {
	Execute(ctx context.Context, cmd ScanAndMatchCmd) (*ScanAndMatchResult, error)
}

type scanAndMatchOrchImpl struct {
	scanner         scanner.ScannerInterface
	scanConfig      scanner.Config
	fs              afero.Fs
	matcher         matcher.MatcherInterface
	maxFilesPerScan int
	logger          logging.Logger
}

var _ scanAndMatchOrchestrator = (*scanAndMatchOrchImpl)(nil)

func newScanAndMatchOrchestrator(s scanner.ScannerInterface, scanCfg scanner.Config, fs afero.Fs, m matcher.MatcherInterface, maxFilesPerScan int, logger logging.Logger) scanAndMatchOrchestrator {
	return &scanAndMatchOrchImpl{
		scanner:         s,
		scanConfig:      scanCfg,
		fs:              fs,
		matcher:         m,
		maxFilesPerScan: maxFilesPerScan,
		logger:          logger,
	}
}

// Execute runs the scan + match + multipart validation pipeline.
func (o *scanAndMatchOrchImpl) Execute(ctx context.Context, cmd ScanAndMatchCmd) (*ScanAndMatchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Validate required inputs.
	if cmd.Directory == "" {
		return nil, fmt.Errorf("directory is required for scan and match")
	}

	// Apply timeout if specified.
	if cmd.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cmd.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	// Use the pre-built scanner, or construct one as fallback.
	scan := o.scanner
	if scan == nil {
		if o.fs == nil {
			return nil, fmt.Errorf("filesystem not configured")
		}
		scanConfig := o.scanConfig
		scan = scanner.NewScanner(o.fs, &scanConfig)
	}

	// Determine max files limit.
	maxFiles := cmd.MaxFiles
	if maxFiles == 0 {
		maxFiles = o.maxFilesPerScan
	}

	// Execute scan.
	var scanResult *scanner.ScanResult
	var err error

	if cmd.Recursive {
		scanResult, err = scan.ScanWithFilter(ctx, cmd.Directory, maxFiles, cmd.Filter)
	} else {
		scanResult, err = scan.ScanSingle(cmd.Directory)
	}

	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	// Match results using the orchestrator's matcher.
	var matchResults []matcher.MatchResult
	if o.matcher != nil {
		// Per ADR-0034: scanResult.Files is already []models.FileMatchInfo
		matchResults = o.matcher.Match(scanResult.Files)
		matchResults = matcher.ValidateMultipartInDirectory(matchResults)
	} else {
		resolveLogger(o.logger).Warnf("[workflow] ScanAndMatch: matcher not configured, skipping ID matching")
		matchResults = make([]matcher.MatchResult, len(scanResult.Files))
		for i, f := range scanResult.Files {
			matchResults[i] = matcher.MatchResult{File: f}
		}
	}

	// Build result.
	result := &ScanAndMatchResult{
		Skipped:      scanResult.SkippedCount,
		SkippedPaths: scanResult.Skipped,
		TimedOut:     scanResult.TimedOut,
	}

	// Build models.FileMatchInfo slice from scan + match results.
	matchMap := make(map[string]int) // path -> index in matchResults
	for i, mr := range matchResults {
		matchMap[mr.File.Path] = i
	}

	for _, fileInfo := range scanResult.Files {
		fmi := models.FileMatchInfo{
			Path:      fileInfo.Path,
			Name:      fileInfo.Name,
			Extension: fileInfo.Extension,
			Size:      fileInfo.Size,
			ModTime:   fileInfo.ModTime,
		}
		if idx, found := matchMap[fileInfo.Path]; found {
			fmi.MovieID = matchResults[idx].ID
			fmi.IsMultiPart = matchResults[idx].IsMultiPart
			fmi.PartNumber = matchResults[idx].PartNumber
			fmi.PartSuffix = matchResults[idx].PartSuffix
		}
		result.Files = append(result.Files, fmi)
	}

	return result, nil
}

// noOpScanAndMatchOrchestrator returns an error when ScanAndMatch is called on a Workflow
// that was not configured for scanning.
// Per T-098-03: returns error (not silent success) — callers detect misconfiguration.
type noOpScanAndMatchOrchestrator struct{}

var _ scanAndMatchOrchestrator = (*noOpScanAndMatchOrchestrator)(nil)

func (noOpScanAndMatchOrchestrator) Execute(_ context.Context, _ ScanAndMatchCmd) (*ScanAndMatchResult, error) {
	return nil, fmt.Errorf("scan and match not configured")
}
