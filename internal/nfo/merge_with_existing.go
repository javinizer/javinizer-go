package nfo

import (
	"path/filepath"
	"strings"

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
	RemapParsedNFOTitleForMerge(parseResult.Movie, movie.Title)
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
//
// The remap only fires when the NFO <title> looks like a javinizer-generated
// display title — i.e. it is prefixed with the movie's own code (optionally
// bracketed). Manually-edited or imported NFOs whose <title> is the desired
// base title are left untouched so preserve_nfo / prefer-nfo strategies keep
// the existing NFO <title> as the base Title.
//
// scrapedTitle is the scraper's base Title for the same movie. When it is empty
// (sparse scraper result or temporary title-extraction failure), moving the NFO
// <title> to DisplayTitle would leave both merge inputs without a base Title,
// and since Title is a critical field the merge would fall back to
// "[Unknown Title]" despite the NFO carrying a valid <title>. To avoid that,
// when the scraped title is empty the NFO <title> is kept in Title (with its
// code prefix stripped) as the base-title fallback instead of being moved.
func RemapParsedNFOTitleForMerge(movie *models.Movie, scrapedTitle string) {
	if movie == nil {
		return
	}
	if movie.DisplayTitle != "" || movie.Title == "" {
		return
	}
	if !looksLikeJavinizerDisplayTitle(movie.Title, movie.ID) {
		return
	}
	if strings.TrimSpace(scrapedTitle) == "" {
		movie.Title = stripCodePrefix(movie.Title, movie.ID)
		return
	}
	movie.DisplayTitle = movie.Title
	movie.Title = ""
}

// looksLikeJavinizerDisplayTitle reports whether title is prefixed with the
// movie's own code, optionally wrapped in brackets (e.g. "[MKMP-094] ..." or
// "MKMP-094 ..."). This is the signature of a javinizer-generated display title
// written to the NFO <title> field; plain base titles from manual/imported NFOs
// do not start with the movie code.
func looksLikeJavinizerDisplayTitle(title, movieID string) bool {
	title = strings.TrimSpace(title)
	if movieID == "" || title == "" {
		return false
	}
	lower := strings.ToLower(title)
	id := strings.ToLower(movieID)
	return strings.HasPrefix(lower, "["+id+"]") || strings.HasPrefix(lower, id+" ")
}

// stripCodePrefix removes a leading "[<id>] " or "<id> " code prefix (case-
// insensitive) from title, if present, and trims surrounding whitespace. When
// the prefix is absent the title is returned unchanged so manual/imported base
// titles are preserved verbatim.
func stripCodePrefix(title, movieID string) string {
	title = strings.TrimSpace(title)
	if movieID == "" {
		return title
	}
	lower := strings.ToLower(title)
	id := strings.ToLower(movieID)
	var stripped string
	if strings.HasPrefix(lower, "["+id+"]") {
		stripped = title[len(movieID)+2:]
	} else if strings.HasPrefix(lower, id+" ") {
		stripped = title[len(movieID)+1:]
	} else {
		return title
	}
	stripped = strings.TrimSpace(stripped)
	for _, sep := range []string{"- ", "\u2013 ", "\u2014 ", "-"} {
		if strings.HasPrefix(stripped, sep) {
			stripped = strings.TrimSpace(stripped[len(sep):])
			break
		}
	}
	return stripped
}
