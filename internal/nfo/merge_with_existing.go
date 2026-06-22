package nfo

import (
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

type MergeWithExistingOptions struct {
	Match          models.FileMatchInfo
	ForceOverwrite bool
	PreserveNFO    bool
	ScalarStrategy MergeStrategy // Per ADR-0030: resolved at boundary (includes preset application)
	ArrayStrategy  bool          // Per ADR-0030: true=merge, false=replace. Resolved at boundary
}

type MergeWithExistingResult struct {
	Movie        *models.Movie
	Merged       bool
	MergeStats   *MergeStats
	FoundNFOPath string
}

func (n nfoImplementor) MergeWithExistingNFO(movie *models.Movie, opts MergeWithExistingOptions) MergeWithExistingResult {
	result := MergeWithExistingResult{Movie: movie}
	if opts.ForceOverwrite {
		return result
	}

	// Per ADR-0033: use struct-held infrastructure dependencies.
	// Fall back to safe defaults for zero-value implementor (nil fs/nfoConfig/templateEngine).
	fs := n.fs
	if fs == nil {
		return result
	}
	nfoConfig := n.nfoConfig
	templateEngine := n.templateEngine

	isMultiPart := opts.Match.IsMultiPart
	partSuffix := ""
	if isMultiPart {
		partSuffix = opts.Match.PartSuffix
	}

	var nameCfg NFONameConfig
	if nfoConfig != nil {
		nameCfg = nfoConfig.ToNFONameConfig(isMultiPart, partSuffix)
	} else {
		nameCfg = NFONameConfig{
			IsMultiPart: isMultiPart,
			PartSuffix:  partSuffix,
		}
	}
	foundPath := findNFOFile(fs, filepath.Dir(opts.Match.Path), movie, nameCfg, opts.Match.Path, templateEngine)
	if foundPath == "" {
		return result
	}
	result.FoundNFOPath = foundPath
	parseResult, parseErr := ParseNFO(fs, foundPath)
	if parseErr != nil {
		logging.Warnf("[workflow] Failed to parse existing NFO for %s: %v (using scraped data only)", movie.ID, parseErr)
		return result
	}
	scalarStrategy := opts.ScalarStrategy
	mergeArrays := opts.ArrayStrategy
	// Per ADR-0030: preset is resolved at the boundary before constructing
	// MergeWithExistingOptions. No ApplyPreset call here.
	if opts.PreserveNFO {
		scalarStrategy = PreserveExisting
	}
	mergeResult, mergeErr := MergeMovieMetadataWithOptions(movie, parseResult.Movie, scalarStrategy, mergeArrays)
	if mergeErr != nil {
		logging.Warnf("[workflow] Failed to merge NFO data for %s: %v (using scraped data only)", movie.ID, mergeErr)
		return result
	}
	result.Movie = mergeResult.Merged
	result.Merged = true
	result.MergeStats = &mergeResult.Stats
	return result
}
