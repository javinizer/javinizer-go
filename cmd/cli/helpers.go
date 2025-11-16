package main

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

// MediaDownloader defines the interface for downloading media files.
// This interface allows for easier testing by enabling mock implementations.
type MediaDownloader interface {
	DownloadAll(movie *models.Movie, destDir string, partNumber int) ([]downloader.DownloadResult, error)
}

// scanAndMatch scans files and extracts JAV IDs.
// Returns matched results, scan result, and error.
func scanAndMatch(
	sourcePath string,
	recursive bool,
	fileScanner *scanner.Scanner,
	fileMatcher *matcher.Matcher,
) ([]matcher.MatchResult, *scanner.ScanResult, error) {
	// Step 1: Scan for video files
	fmt.Println("📂 Scanning for video files...")
	var scanResult *scanner.ScanResult
	var err error
	if recursive {
		scanResult, err = fileScanner.Scan(sourcePath)
	} else {
		scanResult, err = fileScanner.ScanSingle(sourcePath)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("scan failed: %w", err)
	}

	fmt.Printf("   Found %d video file(s)\n", len(scanResult.Files))
	if len(scanResult.Skipped) > 0 {
		fmt.Printf("   Skipped %d file(s)\n", len(scanResult.Skipped))
	}
	if len(scanResult.Errors) > 0 {
		fmt.Printf("   ⚠️  %d error(s) during scan\n", len(scanResult.Errors))
	}

	if len(scanResult.Files) == 0 {
		fmt.Println("\n✅ No files to process")
		return nil, scanResult, nil
	}

	// Step 2: Match JAV IDs
	fmt.Println("\n🔍 Extracting JAV IDs...")
	matches := fileMatcher.Match(scanResult.Files)
	fmt.Printf("   Matched %d file(s)\n", len(matches))

	if len(matches) == 0 {
		fmt.Println("\n⚠️  No JAV IDs found in filenames")
		return nil, scanResult, nil
	}

	// Group by ID
	grouped := matcher.GroupByID(matches)
	fmt.Printf("   Found %d unique ID(s)\n", len(grouped))

	return matches, scanResult, nil
}

// scrapeMetadata scrapes and aggregates metadata with caching support.
// Returns movies map, scrapedCount, cachedCount, and error.
func scrapeMetadata(
	matches []matcher.MatchResult,
	movieRepo *database.MovieRepository,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	scraperPriority []string,
	forceRefresh bool,
) (map[string]*models.Movie, int, int, error) {
	// Group matches by ID
	grouped := matcher.GroupByID(matches)

	fmt.Println("\n🌐 Scraping metadata...")
	movies := make(map[string]*models.Movie)
	scrapedCount := 0
	cachedCount := 0

	for id := range grouped {
		fmt.Printf("   %s... ", id)

		// Force refresh - clear cache if requested
		if forceRefresh {
			if err := movieRepo.Delete(id); err != nil {
				logging.Debugf("Failed to delete %s from cache (may not exist): %v", id, err)
			} else {
				logging.Debugf("[%s] Cache cleared successfully", id)
			}
		}

		// Check cache first (skip if force refresh)
		if !forceRefresh {
			if movie, err := movieRepo.FindByID(id); err == nil {
				movies[id] = movie
				cachedCount++
				fmt.Println("✅ (cached)")
				logging.Debugf("[%s] Found in cache: Title=%s, Maker=%s, Actresses=%d",
					id, movie.Title, movie.Maker, len(movie.Actresses))
				continue
			}
		}

		logging.Debugf("[%s] Not found in cache, scraping from sources", id)

		// Scrape from sources
		results := []*models.ScraperResult{}
		scrapers := registry.GetByPriority(scraperPriority)
		logging.Debugf("[%s] Initialized %d scrapers in priority order", id, len(scrapers))

		for _, scraper := range scrapers {
			logging.Debugf("[%s] Querying scraper: %s", id, scraper.Name())
			if result, err := scraper.Search(id); err == nil {
				logging.Debugf("[%s] Scraper %s returned: Title=%s, Language=%s, Actresses=%d, Genres=%d",
					id, scraper.Name(), result.Title, result.Language, len(result.Actresses), len(result.Genres))
				results = append(results, result)
			} else {
				logging.Debugf("[%s] Scraper %s failed: %v", id, scraper.Name(), err)
			}
		}

		if len(results) == 0 {
			fmt.Println("❌ (not found)")
			logging.Debugf("[%s] No results from any scraper", id)
			continue
		}

		logging.Debugf("[%s] Collected %d results from scrapers, starting aggregation", id, len(results))

		// Aggregate and save
		movie, err := agg.Aggregate(results)
		if err != nil {
			fmt.Printf("❌ (aggregate error: %v)\n", err)
			logging.Debugf("[%s] Aggregation failed: %v", id, err)
			continue
		}

		// Log aggregated metadata details
		logging.Debugf("[%s] Aggregation complete - Final metadata:", id)
		logging.Debugf("[%s]   Title: %s", id, movie.Title)
		logging.Debugf("[%s]   Maker: %s", id, movie.Maker)
		logging.Debugf("[%s]   Release Date: %v", id, movie.ReleaseDate)
		logging.Debugf("[%s]   Runtime: %d min", id, movie.Runtime)
		logging.Debugf("[%s]   Actresses: %d", id, len(movie.Actresses))
		if len(movie.Actresses) > 0 {
			actressNames := make([]string, len(movie.Actresses))
			for i, a := range movie.Actresses {
				actressNames[i] = a.FullName()
			}
			logging.Debugf("[%s]   Actress Names: %v", id, actressNames)
		}
		logging.Debugf("[%s]   Genres: %d", id, len(movie.Genres))
		if len(movie.Genres) > 0 {
			genreNames := make([]string, len(movie.Genres))
			for i, g := range movie.Genres {
				genreNames[i] = g.Name
			}
			logging.Debugf("[%s]   Genre Names: %v", id, genreNames)
		}
		logging.Debugf("[%s]   Screenshots: %d", id, len(movie.Screenshots))
		logging.Debugf("[%s]   Cover URL: %s", id, movie.CoverURL)
		logging.Debugf("[%s]   Trailer URL: %s", id, movie.TrailerURL)

		if err := movieRepo.Upsert(movie); err != nil {
			logging.Infof("Warning: Failed to save %s to database: %v", id, err)
		}

		movies[id] = movie
		scrapedCount++
		fmt.Println("✅ (scraped)")
	}

	fmt.Printf("   Scraped: %d, Cached: %d, Failed: %d\n", scrapedCount, cachedCount, len(grouped)-len(movies))

	if len(movies) == 0 {
		fmt.Println("\n⚠️  No metadata found")
		return nil, 0, 0, nil
	}

	return movies, scrapedCount, cachedCount, nil
}

// generateNFOs generates NFO files for the given movies.
// Returns the count of NFOs generated and any error.
func generateNFOs(
	movies map[string]*models.Movie,
	matches []matcher.MatchResult,
	nfoGenerator *nfo.Generator,
	fileOrganizer *organizer.Organizer,
	nfoEnabled bool,
	moveToFolder bool,
	perFileNFO bool,
	destPath string,
	forceUpdate bool,
	dryRun bool,
) (int, error) {
	if !nfoEnabled {
		return 0, nil
	}

	fmt.Println("\n📝 Generating NFO files...")
	nfoCount := 0

	for id, movie := range movies {
		// Find all matches for this ID
		var idMatches []matcher.MatchResult
		for _, m := range matches {
			if m.ID == id {
				idMatches = append(idMatches, m)
			}
		}

		// Determine output directory: either organized folder or source directory
		var outputDir string
		if moveToFolder {
			// Create destination folder for this movie (use first match for planning)
			plan, err := fileOrganizer.Plan(idMatches[0], movie, destPath, forceUpdate)
			if err != nil {
				logging.Infof("Failed to plan for %s: %v", id, err)
				continue
			}
			outputDir = plan.TargetDir
		} else {
			// Use source directory (directory of the first file)
			outputDir = idMatches[0].File.Dir
		}

		// If per_file is enabled and this is multi-part, generate NFO for each part
		if perFileNFO && len(idMatches) > 1 {
			for _, match := range idMatches {
				partSuffix := ""
				if match.IsMultiPart {
					partSuffix = match.PartSuffix
				}

				if dryRun {
					nfoCount++
					fmt.Printf("   %s%s.nfo (would generate)\n", id, partSuffix)
				} else {
					if err := nfoGenerator.Generate(movie, outputDir, partSuffix, ""); err != nil {
						logging.Infof("Failed to generate NFO for %s%s: %v", id, partSuffix, err)
					} else {
						nfoCount++
						fmt.Printf("   %s%s.nfo ✅\n", id, partSuffix)
					}
				}
			}
		} else {
			// Single NFO for all parts (or single file)
			if dryRun {
				nfoCount++
				fmt.Printf("   %s.nfo (would generate)\n", id)
			} else {
				if err := nfoGenerator.Generate(movie, outputDir, "", ""); err != nil {
					logging.Infof("Failed to generate NFO for %s: %v", id, err)
				} else {
					nfoCount++
					fmt.Printf("   %s.nfo ✅\n", id)
				}
			}
		}
	}

	if dryRun {
		fmt.Printf("   Would generate %d NFO file(s)\n", nfoCount)
	} else {
		fmt.Printf("   Generated %d NFO file(s)\n", nfoCount)
	}

	return nfoCount, nil
}

// downloadMediaFiles downloads covers, posters, screenshots, and trailers.
// Returns the count of files downloaded and any error.
func downloadMediaFiles(
	movies map[string]*models.Movie,
	matches []matcher.MatchResult,
	mediaDownloader MediaDownloader,
	fileOrganizer *organizer.Organizer,
	downloadCover bool,
	downloadExtrafanart bool,
	moveToFolder bool,
	destPath string,
	forceUpdate bool,
	dryRun bool,
) (int, error) {

	fmt.Println("\n📥 Downloading media...")
	downloadCount := 0

	for id, movie := range movies {
		// Find all matches for this ID
		var idMatches []matcher.MatchResult
		for _, m := range matches {
			if m.ID == id {
				idMatches = append(idMatches, m)
			}
		}
		if len(idMatches) == 0 {
			continue
		}
		firstMatch := idMatches[0]

		// Determine output directory: either organized folder or source directory
		var downloadDir string
		if moveToFolder {
			plan, err := fileOrganizer.Plan(firstMatch, movie, destPath, forceUpdate)
			if err != nil {
				continue
			}
			downloadDir = plan.TargetDir
		} else {
			// Use source directory
			downloadDir = firstMatch.File.Dir
		}

		if dryRun {
			count := 0
			if downloadCover {
				count++
				logging.Debugf("[%s] Would download cover from: %s", id, movie.CoverURL)
			}
			if downloadExtrafanart {
				count += len(movie.Screenshots)
				logging.Debugf("[%s] Would download %d screenshots", id, len(movie.Screenshots))
			}
			fmt.Printf("   %s: would download ~%d file(s)\n", id, count)
		} else {
			logging.Debugf("[%s] Starting download to: %s", id, downloadDir)
			// Use PartNumber for deduplication (0 for single file, 1+ for multi-part)
			// Find the lowest part number to determine if we should download shared media
			partNumber := 0
			if firstMatch.IsMultiPart {
				// For multi-part, find the lowest part number among all matches
				minPartNumber := idMatches[0].PartNumber
				for _, m := range idMatches {
					if m.PartNumber < minPartNumber {
						minPartNumber = m.PartNumber
					}
				}
				// Clamp to 1 so even if only later segments exist (e.g., only pt2),
				// we still download shared media once
				if minPartNumber > 1 {
					minPartNumber = 1
				}
				partNumber = minPartNumber
			}
			results, err := mediaDownloader.DownloadAll(movie, downloadDir, partNumber)
			if err != nil {
				logging.Infof("Download error for %s: %v", id, err)
			}

			downloaded := 0
			skipped := 0
			failed := 0
			for _, r := range results {
				if r.Downloaded {
					downloaded++
					logging.Debugf("[%s] Downloaded %s: %s (%d bytes in %v)", id, r.Type, r.LocalPath, r.Size, r.Duration)
				} else if r.Error != nil {
					failed++
					logging.Debugf("[%s] Failed to download %s: %v", id, r.Type, r.Error)
				} else {
					skipped++
					logging.Debugf("[%s] Skipped %s (already exists): %s", id, r.Type, r.LocalPath)
				}
			}
			logging.Debugf("[%s] Download summary: %d downloaded, %d skipped, %d failed", id, downloaded, skipped, failed)
			if downloaded > 0 {
				downloadCount += downloaded
				fmt.Printf("   %s: %d file(s) ✅\n", id, downloaded)
			}
		}
	}

	if !dryRun {
		fmt.Printf("   Downloaded %d file(s)\n", downloadCount)
	}

	return downloadCount, nil
}
