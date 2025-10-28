package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// ScrapeTask scrapes metadata for a JAV ID
type ScrapeTask struct {
	BaseTask
	javID           string
	registry        *models.ScraperRegistry
	aggregator      *aggregator.Aggregator
	movieRepo       *database.MovieRepository
	progressTracker *ProgressTracker
	dryRun          bool
	forceRefresh    bool
}

// NewScrapeTask creates a new scrape task
func NewScrapeTask(
	javID string,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	movieRepo *database.MovieRepository,
	progressTracker *ProgressTracker,
	dryRun bool,
	forceRefresh bool,
) *ScrapeTask {
	desc := fmt.Sprintf("Scraping metadata for %s", javID)
	if dryRun {
		desc = "[DRY RUN] " + desc
	}

	return &ScrapeTask{
		BaseTask: BaseTask{
			id:          javID,
			taskType:    TaskTypeScrape,
			description: desc,
		},
		javID:           javID,
		registry:        registry,
		aggregator:      agg,
		movieRepo:       movieRepo,
		progressTracker: progressTracker,
		dryRun:          dryRun,
		forceRefresh:    forceRefresh,
	}
}

func (t *ScrapeTask) Execute(ctx context.Context) error {
	logging.Debugf("[%s] Starting scrape task (dryRun=%v, forceRefresh=%v)", t.javID, t.dryRun, t.forceRefresh)

	// If force refresh is enabled, delete from cache first
	if t.forceRefresh {
		logging.Debugf("[%s] Force refresh enabled, attempting to delete from cache", t.javID)
		if err := t.movieRepo.Delete(t.javID); err != nil {
			// Log but don't fail - movie might not exist in cache
			logging.Debugf("[%s] Cache delete failed (movie may not exist): %v", t.javID, err)
			t.progressTracker.Update(t.id, 0.05, "Clearing cache...", 0)
		} else {
			logging.Debugf("[%s] Cache cleared successfully", t.javID)
			t.progressTracker.Update(t.id, 0.1, "Cache cleared, re-scraping...", 0)
		}
	} else {
		// Check cache first (only if not forcing refresh)
		logging.Debugf("[%s] Checking cache for existing metadata", t.javID)
		if cached, err := t.movieRepo.FindByID(t.javID); err == nil {
			logging.Debugf("[%s] Found in cache: Title=%s, Maker=%s, Actresses=%d",
				t.javID, cached.Title, cached.Maker, len(cached.Actresses))
			msg := "Found in cache"
			if t.dryRun {
				msg = "[DRY RUN] " + msg
			}
			t.progressTracker.Update(t.id, 1.0, msg, 0)
			return nil
		}
		logging.Debugf("[%s] Not found in cache, will scrape from sources", t.javID)
	}

	// Scrape from sources
	msg := "Querying scrapers..."
	if t.dryRun {
		msg = "[DRY RUN] " + msg
	}
	t.progressTracker.Update(t.id, 0.2, msg, 0)

	results := make([]*models.ScraperResult, 0)
	scrapers := t.registry.GetByPriority([]string{"r18dev", "dmm"})
	logging.Debugf("[%s] Initialized %d scrapers in priority order", t.javID, len(scrapers))

	for i, scraper := range scrapers {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		progress := 0.2 + (float64(i) / float64(len(scrapers)) * 0.5)
		msg := fmt.Sprintf("Scraping from %s...", scraper.Name())
		if t.dryRun {
			msg = "[DRY RUN] " + msg
		}
		t.progressTracker.Update(t.id, progress, msg, 0)

		logging.Debugf("[%s] Querying scraper: %s", t.javID, scraper.Name())
		result, err := scraper.Search(t.javID)
		if err != nil {
			logging.Debugf("[%s] Scraper %s failed: %v", t.javID, scraper.Name(), err)
			continue
		}
		logging.Debugf("[%s] Scraper %s returned: Title=%s, Language=%s, Actresses=%d, Genres=%d",
			t.javID, scraper.Name(), result.Title, result.Language, len(result.Actresses), len(result.Genres))
		results = append(results, result)
	}

	if len(results) == 0 {
		logging.Debugf("[%s] No results from any scraper", t.javID)
		return fmt.Errorf("no results found from any scraper")
	}

	logging.Debugf("[%s] Collected %d results from scrapers", t.javID, len(results))

	msg = "Aggregating metadata..."
	if t.dryRun {
		msg = "[DRY RUN] " + msg
	}
	t.progressTracker.Update(t.id, 0.8, msg, 0)

	// Aggregate results
	logging.Debugf("[%s] Starting metadata aggregation", t.javID)
	movie, err := t.aggregator.Aggregate(results)
	if err != nil {
		return fmt.Errorf("failed to aggregate: %w", err)
	}

	logging.Debugf("[%s] Aggregation complete - Final metadata:", t.javID)
	logging.Debugf("[%s]   Title: %s", t.javID, movie.Title)
	logging.Debugf("[%s]   Maker: %s", t.javID, movie.Maker)
	logging.Debugf("[%s]   Release Date: %v", t.javID, movie.ReleaseDate)
	logging.Debugf("[%s]   Runtime: %d min", t.javID, movie.Runtime)
	logging.Debugf("[%s]   Actresses: %d", t.javID, len(movie.Actresses))
	logging.Debugf("[%s]   Genres: %d", t.javID, len(movie.Genres))
	logging.Debugf("[%s]   Screenshots: %d", t.javID, len(movie.Screenshots))

	// Save to database (skip actual write in dry-run mode)
	if t.dryRun {
		preview := fmt.Sprintf("[DRY RUN] Would save: %s - %s", movie.ID, movie.Title)
		if len(movie.Actresses) > 0 {
			preview += fmt.Sprintf(" (%d actresses)", len(movie.Actresses))
		}
		if len(movie.Genres) > 0 {
			preview += fmt.Sprintf(" (%d genres)", len(movie.Genres))
		}
		logging.Debugf("[%s] DRY RUN mode - skipping database save", t.javID)
		t.progressTracker.Update(t.id, 1.0, preview, 0)
		return nil
	}

	t.progressTracker.Update(t.id, 0.9, "Saving to database...", 0)
	logging.Debugf("[%s] Saving metadata to database", t.javID)

	// Use a transaction-safe upsert to avoid UNIQUE constraint errors
	if err := t.movieRepo.Upsert(movie); err != nil {
		logging.Debugf("[%s] Database save failed: %v", t.javID, err)
		return fmt.Errorf("failed to save movie to database: %w", err)
	}

	logging.Debugf("[%s] Successfully saved to database", t.javID)
	t.progressTracker.Update(t.id, 1.0, "Completed", 0)
	return nil
}

// DownloadTask downloads media for a movie
type DownloadTask struct {
	BaseTask
	movie           *models.Movie
	targetDir       string
	downloader      *downloader.Downloader
	progressTracker *ProgressTracker
	dryRun          bool
	partNumber      int // 0 = single file, 1+ = multi-part
}

// NewDownloadTask creates a new download task
func NewDownloadTask(
	movie *models.Movie,
	targetDir string,
	dl *downloader.Downloader,
	progressTracker *ProgressTracker,
	dryRun bool,
	partNumber int,
) *DownloadTask {
	desc := fmt.Sprintf("Downloading media for %s", movie.ID)
	if dryRun {
		desc = "[DRY RUN] " + desc
	}

	return &DownloadTask{
		BaseTask: BaseTask{
			id:          fmt.Sprintf("download-%s", movie.ID),
			taskType:    TaskTypeDownload,
			description: desc,
		},
		movie:           movie,
		targetDir:       targetDir,
		downloader:      dl,
		progressTracker: progressTracker,
		dryRun:          dryRun,
		partNumber:      partNumber,
	}
}

func (t *DownloadTask) Execute(ctx context.Context) error {
	logging.Debugf("[%s] Starting download task (targetDir=%s, dryRun=%v)", t.movie.ID, t.targetDir, t.dryRun)
	t.progressTracker.Update(t.id, 0.1, "Starting downloads...", 0)

	if t.dryRun {
		// In dry-run mode, just preview what would be downloaded
		t.progressTracker.Update(t.id, 0.5, "[DRY RUN] Checking download URLs...", 0)

		// Build detailed preview of what would be downloaded
		items := []string{}
		if t.movie.CoverURL != "" {
			items = append(items, "cover")
			logging.Debugf("[%s] Would download cover from: %s", t.movie.ID, t.movie.CoverURL)
		}
		if t.movie.TrailerURL != "" {
			items = append(items, "trailer")
			logging.Debugf("[%s] Would download trailer from: %s", t.movie.ID, t.movie.TrailerURL)
		}
		if len(t.movie.Screenshots) > 0 {
			items = append(items, fmt.Sprintf("%d screenshots", len(t.movie.Screenshots)))
			logging.Debugf("[%s] Would download %d screenshots", t.movie.ID, len(t.movie.Screenshots))
		}

		preview := "[DRY RUN] Would download: "
		if len(items) > 0 {
			preview += strings.Join(items, ", ") + " -> " + t.targetDir
		} else {
			preview += "nothing (no media URLs)"
		}
		logging.Debugf("[%s] DRY RUN mode - skipping actual downloads", t.movie.ID)
		t.progressTracker.Update(t.id, 1.0, preview, 0)
		return nil
	}

	logging.Debugf("[%s] Initiating DownloadAll for media files (part %d)", t.movie.ID, t.partNumber)
	results, err := t.downloader.DownloadAll(t.movie, t.targetDir, t.partNumber)
	if err != nil {
		logging.Debugf("[%s] Download failed: %v", t.movie.ID, err)
		return fmt.Errorf("download failed: %w", err)
	}

	downloaded := 0
	skipped := 0
	failed := 0
	for _, r := range results {
		if r.Downloaded {
			downloaded++
			logging.Debugf("[%s] Downloaded %s: %s (%d bytes in %v)", t.movie.ID, r.Type, r.LocalPath, r.Size, r.Duration)
		} else if r.Error != nil {
			failed++
			logging.Debugf("[%s] Failed to download %s: %v", t.movie.ID, r.Type, r.Error)
		} else {
			skipped++
			logging.Debugf("[%s] Skipped %s (already exists): %s", t.movie.ID, r.Type, r.LocalPath)
		}
	}

	logging.Debugf("[%s] Download summary: %d downloaded, %d skipped, %d failed", t.movie.ID, downloaded, skipped, failed)
	t.progressTracker.Update(t.id, 1.0, fmt.Sprintf("Downloaded %d files", downloaded), 0)
	return nil
}

// OrganizeTask organizes a video file
type OrganizeTask struct {
	BaseTask
	match           matcher.MatchResult
	movie           *models.Movie
	destPath        string
	moveFiles       bool
	forceUpdate     bool
	organizer       *organizer.Organizer
	progressTracker *ProgressTracker
	dryRun          bool
}

// NewOrganizeTask creates a new organize task
func NewOrganizeTask(
	match matcher.MatchResult,
	movie *models.Movie,
	destPath string,
	moveFiles bool,
	forceUpdate bool,
	org *organizer.Organizer,
	progressTracker *ProgressTracker,
	dryRun bool,
) *OrganizeTask {
	operation := "copy"
	if moveFiles {
		operation = "move"
	}

	desc := fmt.Sprintf("Organizing %s (%s)", match.File.Name, operation)
	if dryRun {
		desc = "[DRY RUN] " + desc
	}

	return &OrganizeTask{
		BaseTask: BaseTask{
			id:          fmt.Sprintf("organize-%s", match.File.Name),
			taskType:    TaskTypeOrganize,
			description: desc,
		},
		match:           match,
		movie:           movie,
		destPath:        destPath,
		moveFiles:       moveFiles,
		forceUpdate:     forceUpdate,
		organizer:       org,
		progressTracker: progressTracker,
		dryRun:          dryRun,
	}
}

func (t *OrganizeTask) Execute(ctx context.Context) error {
	logging.Debugf("[%s] Starting organize task (source=%s, dest=%s, move=%v, forceUpdate=%v, dryRun=%v)",
		t.movie.ID, t.match.File.Path, t.destPath, t.moveFiles, t.forceUpdate, t.dryRun)
	t.progressTracker.Update(t.id, 0.2, "Planning organization...", 0)

	// Plan the organization
	plan, err := t.organizer.Plan(t.match, t.movie, t.destPath, t.forceUpdate)
	if err != nil {
		logging.Debugf("[%s] Planning failed: %v", t.movie.ID, err)
		return fmt.Errorf("failed to plan: %w", err)
	}

	logging.Debugf("[%s] Organization plan created:", t.movie.ID)
	logging.Debugf("[%s]   Source: %s", t.movie.ID, plan.SourcePath)
	logging.Debugf("[%s]   Target Dir: %s", t.movie.ID, plan.TargetDir)
	logging.Debugf("[%s]   Target File: %s", t.movie.ID, plan.TargetFile)
	logging.Debugf("[%s]   Target Path: %s", t.movie.ID, plan.TargetPath)
	logging.Debugf("[%s]   Will Move: %v", t.movie.ID, plan.WillMove)
	logging.Debugf("[%s]   Conflicts: %d", t.movie.ID, len(plan.Conflicts))

	// Validate plan
	t.progressTracker.Update(t.id, 0.4, "Validating plan...", 0)
	if issues := organizer.ValidatePlan(plan); len(issues) > 0 {
		logging.Debugf("[%s] Validation failed with %d issues: %v", t.movie.ID, len(issues), issues)
		return fmt.Errorf("validation failed: %v", issues)
	}
	logging.Debugf("[%s] Plan validated successfully", t.movie.ID)

	if t.dryRun {
		// In dry-run mode, just preview the plan without executing
		operation := "copy"
		if t.moveFiles {
			operation = "move"
		}

		logging.Debugf("[%s] DRY RUN mode - would %s file to %s", t.movie.ID, operation, plan.TargetPath)
		// Use single-line version for progress tracker with detailed path info
		preview := fmt.Sprintf("[DRY RUN] Would %s: %s -> %s", operation, filepath.Base(t.match.File.Path), plan.TargetPath)
		t.progressTracker.Update(t.id, 1.0, preview, 0)
		return nil
	}

	// Execute plan
	t.progressTracker.Update(t.id, 0.6, "Executing plan...", 0)
	var result *organizer.OrganizeResult
	var execErr error

	if t.moveFiles {
		// Execute moves the file (the default behavior)
		logging.Debugf("[%s] Executing MOVE operation", t.movie.ID)
		result, execErr = t.organizer.Execute(plan, false)
	} else {
		// Copy copies instead of moving
		logging.Debugf("[%s] Executing COPY operation", t.movie.ID)
		result, execErr = t.organizer.Copy(plan, false)
	}

	if execErr != nil {
		logging.Debugf("[%s] Organize execution failed: %v", t.movie.ID, execErr)
		return fmt.Errorf("failed to organize: %w", execErr)
	}

	if result.Error != nil {
		logging.Debugf("[%s] Organize result contains error: %v", t.movie.ID, result.Error)
		return fmt.Errorf("organize error: %w", result.Error)
	}

	logging.Debugf("[%s] File organized successfully to: %s", t.movie.ID, result.NewPath)
	t.progressTracker.Update(t.id, 1.0, "Organized successfully", 0)
	return nil
}

// NFOTask generates an NFO file
type NFOTask struct {
	BaseTask
	movie           *models.Movie
	targetDir       string
	generator       *nfo.Generator
	progressTracker *ProgressTracker
	dryRun          bool
	partSuffix      string // Optional suffix for multi-part files (e.g., "-pt1", "-A")
}

// NewNFOTask creates a new NFO generation task
func NewNFOTask(
	movie *models.Movie,
	targetDir string,
	gen *nfo.Generator,
	progressTracker *ProgressTracker,
	dryRun bool,
	partSuffix string, // Optional suffix for multi-part files (e.g., "-pt1", "-A")
) *NFOTask {
	desc := fmt.Sprintf("Generating NFO for %s", movie.ID)
	if partSuffix != "" {
		desc = fmt.Sprintf("Generating NFO for %s%s", movie.ID, partSuffix)
	}
	if dryRun {
		desc = "[DRY RUN] " + desc
	}

	return &NFOTask{
		BaseTask: BaseTask{
			id:          fmt.Sprintf("nfo-%s%s", movie.ID, partSuffix),
			taskType:    TaskTypeNFO,
			description: desc,
		},
		movie:           movie,
		targetDir:       targetDir,
		generator:       gen,
		progressTracker: progressTracker,
		dryRun:          dryRun,
		partSuffix:      partSuffix,
	}
}

func (t *NFOTask) Execute(ctx context.Context) error {
	t.progressTracker.Update(t.id, 0.5, "Generating NFO...", 0)

	if t.dryRun {
		// In dry-run mode, just preview what would be generated
		nfoPath := filepath.Join(t.targetDir, t.movie.ID+".nfo")
		preview := fmt.Sprintf("[DRY RUN] Would generate NFO: %s", filepath.Base(nfoPath))
		if t.movie.Title != "" {
			preview += fmt.Sprintf(" (Title: %s)", t.movie.Title)
		}
		t.progressTracker.Update(t.id, 1.0, preview, 0)
		return nil
	}

	if err := t.generator.Generate(t.movie, t.targetDir, t.partSuffix); err != nil {
		return fmt.Errorf("failed to generate NFO: %w", err)
	}

	t.progressTracker.Update(t.id, 1.0, "NFO generated", 0)
	return nil
}

// ProcessFileTask is a composite task that processes a single file
// It executes scrape, download, organize, and NFO tasks sequentially
type ProcessFileTask struct {
	BaseTask
	match           matcher.MatchResult
	registry        *models.ScraperRegistry
	aggregator      *aggregator.Aggregator
	movieRepo       *database.MovieRepository
	downloader      *downloader.Downloader
	organizer       *organizer.Organizer
	nfoGenerator    *nfo.Generator
	destPath        string
	moveFiles       bool
	forceUpdate     bool
	forceRefresh    bool
	progressTracker *ProgressTracker
	dryRun          bool
	scrapeEnabled   bool
	downloadEnabled bool
	organizeEnabled bool
	nfoEnabled      bool
}

// NewProcessFileTask creates a new composite task for processing a file
func NewProcessFileTask(
	match matcher.MatchResult,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	movieRepo *database.MovieRepository,
	dl *downloader.Downloader,
	org *organizer.Organizer,
	nfoGen *nfo.Generator,
	destPath string,
	moveFiles bool,
	forceUpdate bool,
	forceRefresh bool,
	progressTracker *ProgressTracker,
	dryRun bool,
	scrapeEnabled bool,
	downloadEnabled bool,
	organizeEnabled bool,
	nfoEnabled bool,
) *ProcessFileTask {
	desc := fmt.Sprintf("Processing %s", match.ID)
	if dryRun {
		desc = "[DRY RUN] " + desc
	}

	return &ProcessFileTask{
		BaseTask: BaseTask{
			id:          fmt.Sprintf("process-%s", match.ID),
			taskType:    "process",
			description: desc,
		},
		match:           match,
		registry:        registry,
		aggregator:      agg,
		movieRepo:       movieRepo,
		downloader:      dl,
		organizer:       org,
		nfoGenerator:    nfoGen,
		destPath:        destPath,
		moveFiles:       moveFiles,
		forceUpdate:     forceUpdate,
		forceRefresh:    forceRefresh,
		progressTracker: progressTracker,
		dryRun:          dryRun,
		scrapeEnabled:   scrapeEnabled,
		downloadEnabled: downloadEnabled,
		organizeEnabled: organizeEnabled,
		nfoEnabled:      nfoEnabled,
	}
}

func (t *ProcessFileTask) Execute(ctx context.Context) error {
	msg := "Starting..."
	if t.dryRun {
		msg = "[DRY RUN] " + msg
	}
	t.progressTracker.Update(t.id, 0.0, msg, 0)

	var movie *models.Movie
	var err error

	// Step 1: Scrape metadata (always scrape, even in dry-run)
	if t.scrapeEnabled {
		scrapeTask := NewScrapeTask(
			t.match.ID,
			t.registry,
			t.aggregator,
			t.movieRepo,
			t.progressTracker,
			t.dryRun,
			t.forceRefresh,
		)
		if err := scrapeTask.Execute(ctx); err != nil {
			return fmt.Errorf("scrape failed: %w", err)
		}

		// In dry-run mode, scraping doesn't save to DB, so we can't fetch it back
		// We need to scrape again but this time keep the result
		if t.dryRun {
			// Re-scrape to get the movie object for preview
			scrapers := t.registry.GetByPriority([]string{"r18dev", "dmm"})
			results := make([]*models.ScraperResult, 0)

			for _, scraper := range scrapers {
				result, err := scraper.Search(t.match.ID)
				if err != nil {
					continue
				}
				results = append(results, result)
			}

			if len(results) > 0 {
				movie, err = t.aggregator.Aggregate(results)
				if err != nil {
					t.progressTracker.Update(t.id, 0.35, "[DRY RUN] Using basic metadata", 0)
					// Create minimal movie for preview
					movie = &models.Movie{
						ID:          t.match.ID,
						DisplayName: t.match.ID,
						Title:       t.match.ID,
					}
				} else {
					// Output detailed metadata in dry-run mode
					t.progressTracker.Update(t.id, 0.35, "[DRY RUN] Metadata extracted:", 0)
					if movie.Title != "" {
						t.progressTracker.Update(t.id, 0.36, fmt.Sprintf("  Title: %s", movie.Title), 0)
					}
					if movie.DisplayName != "" {
						t.progressTracker.Update(t.id, 0.37, fmt.Sprintf("  Display Name: %s", movie.DisplayName), 0)
					}
					if movie.Maker != "" {
						t.progressTracker.Update(t.id, 0.38, fmt.Sprintf("  Maker: %s", movie.Maker), 0)
					}
					if len(movie.Genres) > 0 {
						genres := make([]string, len(movie.Genres))
						for i, g := range movie.Genres {
							genres[i] = g.Name
						}
						t.progressTracker.Update(t.id, 0.39, fmt.Sprintf("  Genres: %s", strings.Join(genres, ", ")), 0)
					}
					if len(movie.Actresses) > 0 {
						actresses := make([]string, len(movie.Actresses))
						for i, a := range movie.Actresses {
							if a.JapaneseName != "" {
								actresses[i] = a.JapaneseName
							} else {
								actresses[i] = fmt.Sprintf("%s %s", a.FirstName, a.LastName)
							}
						}
						t.progressTracker.Update(t.id, 0.40, fmt.Sprintf("  Actresses: %s", strings.Join(actresses, ", ")), 0)
					}
				}
			}
		} else {
			// Not dry-run, get from database
			movie, err = t.movieRepo.FindByID(t.match.ID)
			if err != nil {
				return fmt.Errorf("failed to get movie from repo: %w", err)
			}
			t.progressTracker.Update(t.id, 0.35, "Got movie metadata", 0)
		}
	}

	// Steps 2-4 require movie metadata
	if movie == nil {
		t.progressTracker.Update(t.id, 1.0, "Skipped (no metadata)", 0)
		return nil
	}

	// Determine target directory from organizer plan
	// This ensures all files go to the same folder
	var targetDir string
	if t.organizeEnabled {
		plan, err := t.organizer.Plan(t.match, movie, t.destPath, t.forceUpdate)
		if err != nil {
			return fmt.Errorf("failed to plan organization: %w", err)
		}
		targetDir = plan.TargetDir
	} else {
		// If organize is disabled, use simple ID-based folder
		targetDir = filepath.Join(t.destPath, t.match.ID)
	}

	// Step 2: Download media (before organizing so files are in place)
	if t.downloadEnabled {
		// Use match.PartNumber for deduplication (0 for single, 1+ for multi-part)
		partNumber := t.match.PartNumber
		if !t.match.IsMultiPart {
			partNumber = 0
		}
		downloadTask := NewDownloadTask(
			movie,
			targetDir,
			t.downloader,
			t.progressTracker,
			t.dryRun,
			partNumber,
		)
		if err := downloadTask.Execute(ctx); err != nil {
			// Log but don't fail - continue with other tasks
			t.progressTracker.Update(t.id, 0.5, fmt.Sprintf("Download failed: %v", err), 0)
		}
	}

	// Step 3: Generate NFO (before organizing so it's in place)
	if t.nfoEnabled {
		// Determine part suffix for NFO generation
		partSuffix := ""
		if t.match.IsMultiPart {
			partSuffix = t.match.PartSuffix
		}

		nfoTask := NewNFOTask(
			movie,
			targetDir,
			t.nfoGenerator,
			t.progressTracker,
			t.dryRun,
			partSuffix,
		)
		if err := nfoTask.Execute(ctx); err != nil {
			// Log but don't fail
			t.progressTracker.Update(t.id, 0.7, fmt.Sprintf("NFO failed: %v", err), 0)
		}
	}

	// Step 4: Organize file (move/copy video file to target directory)
	if t.organizeEnabled {
		organizeTask := NewOrganizeTask(
			t.match,
			movie,
			t.destPath,
			t.moveFiles,
			t.forceUpdate,
			t.organizer,
			t.progressTracker,
			t.dryRun,
		)
		if err := organizeTask.Execute(ctx); err != nil {
			return fmt.Errorf("organize failed: %w", err)
		}
	}

	finalMsg := "Completed"
	if t.dryRun {
		finalMsg = "[DRY RUN] Completed preview"
	}
	t.progressTracker.Update(t.id, 1.0, finalMsg, 0)
	return nil
}
