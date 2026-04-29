package worker

import (
	"context"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

func resolveDMMContentID(
	job *BatchJob,
	fileIndex int,
	movieID string,
	queryOverride string,
	matcherMissFallback bool,
	selectedScrapers []string,
	registry *models.ScraperRegistry,
	query *scrapeQueryResult,
) string {
	shouldResolve := queryOverride == "" && !matcherMissFallback &&
		(len(selectedScrapers) == 0 || scraperListContains(selectedScrapers, "dmm"))
	if shouldResolve {
		if dmmScraper, exists := registry.Get("dmm"); exists {
			if !dmmScraper.IsEnabled() {
				logging.Debugf("[Batch %s] File %d: DMM scraper disabled, skipping content-ID resolution", job.ID, fileIndex)
				return movieID
			}
			if resolver, ok := dmmScraper.(models.ContentIDResolver); ok {
				contentID, err := resolver.ResolveContentID(movieID)
				if err != nil {
					logging.Debugf("[Batch %s] File %d: DMM content-ID resolution failed for %s: %v, using original ID",
						job.ID, fileIndex, movieID, err)
					return movieID
				}
				logging.Debugf("[Batch %s] File %d: Resolved content-ID for %s: %s",
					job.ID, fileIndex, movieID, contentID)
				return contentID
			}
			logging.Debugf("[Batch %s] File %d: DMM scraper does not implement ContentIDResolver, using original ID", job.ID, fileIndex)
			return movieID
		}
		logging.Debugf("[Batch %s] File %d: DMM scraper not available, using original ID", job.ID, fileIndex)
		return movieID
	}

	if queryOverride == "" && len(selectedScrapers) > 0 && !scraperListContains(selectedScrapers, "dmm") {
		logging.Debugf("[Batch %s] File %d: Skipping DMM content-ID resolution (DMM not selected in custom scrapers)", job.ID, fileIndex)
	}
	return movieID
}

func mergeScrapedNFO(
	ctx context.Context,
	job *BatchJob,
	fileIndex int,
	filePath string,
	movie *models.Movie,
	matchResultPtr *matcher.MatchResult,
	cfg *config.Config,
	scalarStrategy string,
	arrayStrategy string,
	fieldSources map[string]string,
	actressSources map[string]string,
) (*models.Movie, map[string]string, map[string]string) {
	if cfg == nil {
		return movie, fieldSources, actressSources
	}

	sourceDir := filepath.Dir(filePath)
	isMultiPart := matchResultPtr != nil && matchResultPtr.IsMultiPart
	var partSuffix string
	if isMultiPart {
		partSuffix = matchResultPtr.PartSuffix
	}
	nfoPath, legacyPaths := nfo.ResolveNFOPath(sourceDir, movie, cfg.Metadata.NFO.FilenameTemplate, cfg.Output.GroupActress, cfg.Metadata.NFO.PerFile, isMultiPart, partSuffix, filePath)

	foundPath := findExistingNFO(job.ID, fileIndex, nfoPath, legacyPaths)

	if foundPath == "" {
		logging.Debugf("[Batch %s] File %d: No existing NFO found, using scraper data only", job.ID, fileIndex)
		return movie, fieldSources, actressSources
	}

	logging.Infof("[Batch %s] File %d: Found existing NFO, merging data: %s", job.ID, fileIndex, foundPath)

	parseResult, err := nfo.ParseNFO(afero.NewOsFs(), foundPath)
	if err != nil {
		logging.Warnf("[Batch %s] File %d: Failed to parse existing NFO %s: %v (will use scraper data only)", job.ID, fileIndex, foundPath, err)
		return movie, fieldSources, actressSources
	}

	scalar := nfo.ParseScalarStrategy(scalarStrategy)
	mergeArrays := nfo.ParseArrayStrategy(arrayStrategy)
	mergeResult, err := nfo.MergeMovieMetadataWithOptions(movie, parseResult.Movie, scalar, mergeArrays)
	if err != nil {
		logging.Warnf("[Batch %s] File %d: Failed to merge NFO data for %s: %v (using scraper data only)", job.ID, fileIndex, movie.ID, err)
		return movie, fieldSources, actressSources
	}

	preMergeTitle := movie.Title
	movie = mergeResult.Merged
	fieldSources = applyNFOMergeProvenance(fieldSources, mergeResult.Provenance)
	actressSources = applyActressMergeProvenance(actressSources, mergeResult.Provenance, movie.Actresses)
	logging.Infof("[Batch %s] File %d: NFO merge complete for %s: %d from scraper, %d from NFO, %d conflicts resolved",
		job.ID, fileIndex, movie.ID, mergeResult.Stats.FromScraper, mergeResult.Stats.FromNFO, mergeResult.Stats.ConflictsResolved)

	if cfg != nil && cfg.Metadata.NFO.DisplayTitle != "" {
		displayTmplEngine := job.TemplateEngine()
		displayCtx := template.NewContextFromMovie(movie)
		displayCtx.Title = preMergeTitle
		if displayName, err := displayTmplEngine.ExecuteWithContext(ctx, cfg.Metadata.NFO.DisplayTitle, displayCtx); err == nil {
			movie.DisplayTitle = displayName
		} else if movie.DisplayTitle == "" {
			movie.DisplayTitle = movie.Title
		}
	} else if movie.DisplayTitle == "" {
		movie.DisplayTitle = movie.Title
	}

	return movie, fieldSources, actressSources
}

func mergeCachedNFO(
	ctx context.Context,
	job *BatchJob,
	fileIndex int,
	filePath string,
	cached *models.Movie,
	matchResultPtr *matcher.MatchResult,
	cfg *config.Config,
	scalarStrategy string,
	arrayStrategy string,
	fieldSources map[string]string,
	actressSources map[string]string,
) (*models.Movie, map[string]string, map[string]string, bool) {
	logging.Debugf("[Batch %s] File %d: Update mode enabled with cache hit, checking for existing NFO to merge", job.ID, fileIndex)

	sourceDir := filepath.Dir(filePath)
	isMultiPart := matchResultPtr != nil && matchResultPtr.IsMultiPart
	var partSuffix string
	if isMultiPart {
		partSuffix = matchResultPtr.PartSuffix
	}
	nfoPath, legacyPaths := nfo.ResolveNFOPath(sourceDir, cached, cfg.Metadata.NFO.FilenameTemplate, cfg.Output.GroupActress, cfg.Metadata.NFO.PerFile, isMultiPart, partSuffix, filePath)

	foundPath := findExistingNFO(job.ID, fileIndex, nfoPath, legacyPaths)

	if foundPath == "" {
		logging.Debugf("[Batch %s] File %d: No existing NFO found, using cached data only", job.ID, fileIndex)
		return cached, fieldSources, actressSources, false
	}

	logging.Infof("[Batch %s] File %d: Found existing NFO, merging with cached data: %s", job.ID, fileIndex, foundPath)
	logging.Debugf("[Batch %s] File %d: Scalar=%s Array=%s", job.ID, fileIndex, scalarStrategy, arrayStrategy)

	parseResult, err := nfo.ParseNFO(afero.NewOsFs(), foundPath)
	if err != nil {
		logging.Warnf("[Batch %s] File %d: Failed to parse existing NFO %s: %v (will use cached data only)", job.ID, fileIndex, foundPath, err)
		return cached, fieldSources, actressSources, false
	}

	scalar := nfo.ParseScalarStrategy(scalarStrategy)
	mergeArrays := nfo.ParseArrayStrategy(arrayStrategy)
	logging.Debugf("[Batch %s] File %d: Parsed scalar strategy: %v (from string: %s)", job.ID, fileIndex, scalar, scalarStrategy)
	mergeResult, err := nfo.MergeMovieMetadataWithOptions(cached, parseResult.Movie, scalar, mergeArrays)
	if err != nil {
		logging.Warnf("[Batch %s] File %d: Failed to merge NFO data for %s: %v (using cached data only)", job.ID, fileIndex, cached.ID, err)
		return cached, fieldSources, actressSources, false
	}

	movieToReturn := mergeResult.Merged
	fieldSources = applyNFOMergeProvenance(fieldSources, mergeResult.Provenance)
	actressSources = applyActressMergeProvenance(actressSources, mergeResult.Provenance, movieToReturn.Actresses)
	logging.Infof("[Batch %s] File %d: NFO merge complete for cached %s: %d from cache, %d from NFO, %d conflicts resolved",
		job.ID, fileIndex, cached.ID, mergeResult.Stats.FromScraper, mergeResult.Stats.FromNFO, mergeResult.Stats.ConflictsResolved)

	applyDisplayTitle(ctx, job, cfg, movieToReturn, cached)
	return movieToReturn, fieldSources, actressSources, true
}
