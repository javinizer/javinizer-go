package nfo

import (
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// MergeWithExistingOptions configures how MergeWithExistingNFO combines scraped data with an existing NFO.
type MergeWithExistingOptions struct {
	Match          models.FileMatchInfo
	ForceOverwrite bool
	PreserveNFO    bool
	ScalarStrategy MergeStrategy // resolved at boundary (includes preset application)
	ArrayStrategy  bool          // true=merge, false=replace. Resolved at boundary
}

// MergeWithExistingResult is the outcome of merging scraped metadata with an existing NFO.
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

	// use struct-held infrastructure dependencies.
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
	RemapParsedNFOTitleForMerge(parseResult.Movie)
	scalarStrategy := opts.ScalarStrategy
	mergeArrays := opts.ArrayStrategy
	// preset is resolved at the boundary before constructing
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

// RemapParsedNFOTitleForMerge treats a parsed NFO <title> as a display title
// (javinizer writes DisplayTitle there): when DisplayTitle is empty it moves
// Title to DisplayTitle and clears Title, so merges never pull a code-prefixed
// NFO display title into the base Title.
func RemapParsedNFOTitleForMerge(movie *models.Movie) {
	if movie == nil {
		return
	}
	if movie.DisplayTitle == "" && movie.Title != "" {
		movie.DisplayTitle = movie.Title
		movie.Title = ""
	}
}
