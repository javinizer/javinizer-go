package batch

import (
	"context"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/worker"
)

type scrapeParams struct {
	queryOverride           string
	selectedScrapers        []string
	scraperPriorityOverride []string
	parsed                  *matcher.ParsedInput
}

func resolveScrapeParams(req *BatchRescrapeRequest, movieID string, deps *ServerDependencies) (*scrapeParams, error) {
	var queryOverride string
	var parsed *matcher.ParsedInput

	if req.ManualSearchInput != "" {
		var err error
		parsed, err = matcher.ParseInput(req.ManualSearchInput, deps.GetRegistry())
		if err != nil {
			logging.Warnf("Failed to parse manual input '%s': %v, using as-is", req.ManualSearchInput, err)
			queryOverride = strings.TrimSpace(req.ManualSearchInput)
		} else {
			if parsed.IsURL {
				queryOverride = req.ManualSearchInput
				logging.Infof("Manual input is a URL, preserving for direct scraping: %s (extracted ID: %s, scraper hint: %s)", req.ManualSearchInput, parsed.ID, parsed.ScraperHint)
			} else {
				queryOverride = parsed.ID
				logging.Debugf("Manual input is not a URL, using as movie ID: %s", parsed.ID)
			}
		}
	} else {
		queryOverride = movieID
	}

	var selectedScrapers []string
	var scraperPriorityOverride []string

	if len(req.SelectedScrapers) > 0 {
		selectedScrapers = matcher.CalculateOptimalScrapers(
			req.SelectedScrapers,
			deps.GetConfig().Scrapers.Priority,
			parsed,
		)
	} else if parsed != nil && parsed.IsURL && len(parsed.CompatibleScrapers) > 0 {
		scraperPriorityOverride = matcher.CalculateOptimalScrapers(
			nil,
			deps.GetConfig().Scrapers.Priority,
			parsed,
		)
	}

	if parsed != nil && parsed.IsURL && len(parsed.CompatibleScrapers) > 0 {
		if len(req.SelectedScrapers) > 0 {
			logging.Infof("URL provided: filtered scrapers from %v to URL-compatible: %v", req.SelectedScrapers, selectedScrapers)
		} else if parsed.ScraperHint != "" {
			logging.Infof("URL provided: using compatible scrapers with %s prioritized: %v", parsed.ScraperHint, scraperPriorityOverride)
		} else {
			logging.Infof("URL provided: using URL-compatible scrapers: %v", scraperPriorityOverride)
		}
	} else if len(req.SelectedScrapers) > 0 {
		logging.Infof("Using custom scrapers: %v", selectedScrapers)
	}

	if req.ManualSearchInput != "" && (strings.HasPrefix(strings.ToLower(req.ManualSearchInput), "http://") || strings.HasPrefix(strings.ToLower(req.ManualSearchInput), "https://")) {
		if parsed == nil || !parsed.IsURL || len(parsed.CompatibleScrapers) == 0 {
			logging.Warnf("[Rescrape] URL detected but no compatible scrapers available: input=%s, compatible_scrapers=%v. Proceeding anyway, scraping may fail.",
				req.ManualSearchInput,
				func() []string {
					if parsed == nil {
						return []string{}
					}
					return parsed.CompatibleScrapers
				}())
		}
	}

	return &scrapeParams{
		queryOverride:           queryOverride,
		selectedScrapers:        selectedScrapers,
		scraperPriorityOverride: scraperPriorityOverride,
		parsed:                  parsed,
	}, nil
}

func executeRescrape(ctx context.Context, params *scrapeParams, job *worker.BatchJob, foundFilePath string, deps *ServerDependencies, req *BatchRescrapeRequest, cfg *config.Config) (*worker.FileResult, error) {
	httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, deps.GetRegistry())
	if err != nil {
		logging.Warnf("Failed to create HTTP client for poster downloads: %v", err)
		httpClient = nil
	}

	scrapeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	_, result, err := worker.RunBatchScrapeOnce(
		scrapeCtx,
		job,
		foundFilePath,
		0,
		params.queryOverride,
		deps.GetRegistry(),
		deps.GetAggregator(),
		deps.MovieRepo,
		deps.GetMatcher(),
		httpClient,
		cfg.Scrapers.UserAgent,
		cfg.Scrapers.Referer,
		req.Force,
		req.Preset != "" || req.ScalarStrategy != "" || req.ArrayStrategy != "",
		params.selectedScrapers,
		params.scraperPriorityOverride,
		nil,
		cfg,
		req.ScalarStrategy,
		req.ArrayStrategy,
	)

	return result, err
}
