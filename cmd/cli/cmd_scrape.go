package main

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/spf13/cobra"
)

func newScrapeCmd() *cobra.Command {
	scrapeCmd := &cobra.Command{
		Use:   "scrape [id]",
		Short: "Scrape metadata for a movie ID",
		Args:  cobra.ExactArgs(1),
		RunE:  runWithDeps(runScrape),
	}
	scrapeCmd.Flags().StringSliceVarP(&scrapersFlag, "scrapers", "s", nil, "Comma-separated list of scrapers to use (e.g., 'r18dev,dmm' or 'dmm')")
	scrapeCmd.Flags().BoolP("force", "f", false, "Force refresh metadata from scrapers (clear cache)")
	scrapeCmd.Flags().Bool("scrape-actress", false, "Enable actress scraping (overrides config)")
	scrapeCmd.Flags().Bool("no-scrape-actress", false, "Disable actress scraping (overrides config)")
	scrapeCmd.Flags().Bool("headless", false, "Enable headless browser for DMM (overrides config)")
	scrapeCmd.Flags().Bool("no-headless", false, "Disable headless browser for DMM (overrides config)")
	scrapeCmd.Flags().Int("headless-timeout", 0, "Headless browser timeout in seconds (overrides config, 0=use config)")
	scrapeCmd.Flags().Bool("actress-db", false, "Enable actress database lookup (overrides config)")
	scrapeCmd.Flags().Bool("no-actress-db", false, "Disable actress database lookup (overrides config)")
	scrapeCmd.Flags().Bool("genre-replacement", false, "Enable genre replacement (overrides config)")
	scrapeCmd.Flags().Bool("no-genre-replacement", false, "Disable genre replacement (overrides config)")
	return scrapeCmd
}

func runScrape(cmd *cobra.Command, args []string, deps *Dependencies) error {
	id := args[0]

	// Get force flag
	forceRefresh, _ := cmd.Flags().GetBool("force")

	// Note: CLI flag overrides are applied in runWithDeps before dependency initialization

	// Initialize repositories
	movieRepo := database.NewMovieRepository(deps.DB)

	// Use injected scraper registry
	registry := deps.ScraperRegistry

	// Initialize aggregator with database support
	agg := aggregator.NewWithDatabase(deps.Config, deps.DB)

	logging.Infof("Scraping metadata for: %s", id)

	// Determine which scrapers to use: CLI flag overrides config
	scrapersToUse := deps.Config.Scrapers.Priority
	usingCustomScrapers := len(scrapersFlag) > 0
	if usingCustomScrapers {
		scrapersToUse = scrapersFlag
		logging.Infof("Using scrapers from CLI flag: %v", scrapersFlag)
	}

	// Force refresh - clear cache if requested
	if forceRefresh {
		if err := movieRepo.Delete(id); err != nil {
			logging.Debugf("Failed to delete %s from cache (may not exist): %v", id, err)
		} else {
			logging.Infof("🔄 Cache cleared for %s", id)
		}
	}

	// Check cache first (skip cache if user specified custom scrapers or force refresh)
	if !usingCustomScrapers && !forceRefresh {
		if movie, err := movieRepo.FindByID(id); err == nil {
			logging.Info("✅ Found in cache!")
			printMovie(movie, nil)
			return nil
		}
	}

	// Phase 1: Content-ID Resolution using DMM
	logging.Info("🔍 Resolving content-ID using DMM...")
	var resolvedID string
	dmmScraper, exists := registry.Get("dmm")
	if exists {
		if dmmScraperTyped, ok := dmmScraper.(*dmm.Scraper); ok {
			contentID, err := dmmScraperTyped.ResolveContentID(id)
			if err != nil {
				logging.Debugf("DMM content-ID resolution failed: %v, will use original ID", err)
				resolvedID = id // Fallback to original ID
			} else {
				resolvedID = contentID
				logging.Infof("✅ Resolved content-ID: %s", resolvedID)
			}
		} else {
			logging.Debug("DMM scraper type assertion failed, using original ID")
			resolvedID = id
		}
	} else {
		logging.Debug("DMM scraper not available, using original ID")
		resolvedID = id
	}

	// Phase 2: Scrape from sources in priority order
	results := []*models.ScraperResult{}

	for _, scraper := range registry.GetByPriority(scrapersToUse) {
		logging.Infof("Scraping %s...", scraper.Name())
		result, err := scraper.Search(resolvedID)
		if err != nil {
			logging.Warnf("❌ %s: %v", scraper.Name(), err)
			// If scraping with resolved ID fails, try with original ID before giving up
			if resolvedID != id {
				logging.Debugf("Retrying %s with original ID: %s", scraper.Name(), id)
				result, err = scraper.Search(id)
				if err != nil {
					logging.Warnf("❌ %s (with original ID): %v", scraper.Name(), err)
					continue
				}
			} else {
				continue
			}
		}
		logging.Info("✅")
		results = append(results, result)
	}

	if len(results) == 0 {
		logging.Error("❌ No results found from any scraper")
		return fmt.Errorf("no results found from any scraper")
	}

	logging.Infof("✅ Found %d source(s)", len(results))

	// Aggregate results
	movie, err := agg.Aggregate(results)
	if err != nil {
		return fmt.Errorf("failed to aggregate: %w", err)
	}

	movie.OriginalFileName = id

	// Save to database (upsert: create or update)
	if err := movieRepo.Upsert(movie); err != nil {
		logging.Warnf("Failed to save to database: %v", err)
	} else {
		fmt.Println("💾 Saved to database")
	}

	printMovie(movie, results)
	return nil
}

func printMovie(movie *models.Movie, results []*models.ScraperResult) {
	fmt.Println()

	// Build table rows
	rows := [][]string{}

	// ID and Content ID
	rows = append(rows, []string{"ID", movie.ID})
	if movie.ContentID != "" && movie.ContentID != movie.ID {
		rows = append(rows, []string{"ContentID", movie.ContentID})
	}

	// Title
	if movie.Title != "" {
		rows = append(rows, []string{"Title", movie.Title})
	}

	// Release Date
	if movie.ReleaseDate != nil {
		rows = append(rows, []string{"ReleaseDate", movie.ReleaseDate.Format("2006-01-02")})
	}

	// Runtime
	if movie.Runtime > 0 {
		rows = append(rows, []string{"Runtime", fmt.Sprintf("%d min", movie.Runtime)})
	}

	// Director
	if movie.Director != "" {
		rows = append(rows, []string{"Director", movie.Director})
	}

	// Maker
	if movie.Maker != "" {
		rows = append(rows, []string{"Maker", movie.Maker})
	}

	// Label
	if movie.Label != "" {
		rows = append(rows, []string{"Label", movie.Label})
	}

	// Series
	if movie.Series != "" {
		rows = append(rows, []string{"Series", movie.Series})
	}

	// Rating
	if movie.RatingScore > 0 {
		rows = append(rows, []string{"Rating", fmt.Sprintf("%.1f/10 (%d votes)", movie.RatingScore, movie.RatingVotes)})
	}

	// Actresses - show detailed information
	if len(movie.Actresses) > 0 {
		actressHeader := fmt.Sprintf("Actresses (%d)", len(movie.Actresses))
		rows = append(rows, []string{actressHeader, ""})

		for i, actress := range movie.Actresses {
			// Build actress name with Japanese
			name := actress.FullName()
			if actress.JapaneseName != "" {
				name += fmt.Sprintf(" (%s)", actress.JapaneseName)
			}

			// Build actress info line with number and DMM ID
			actressLine := fmt.Sprintf("  [%d] %s", i+1, name)
			if actress.DMMID > 0 {
				actressLine += fmt.Sprintf(" - ID: %d", actress.DMMID)
			}
			rows = append(rows, []string{"", actressLine})

			// Add thumbnail URL on separate line if available
			if actress.ThumbURL != "" {
				rows = append(rows, []string{"", fmt.Sprintf("      Thumb: %s", actress.ThumbURL)})
			}
		}
	}

	// Genres
	if len(movie.Genres) > 0 {
		genreNames := make([]string, 0, len(movie.Genres))
		for i, genre := range movie.Genres {
			if i < 8 || len(movie.Genres) <= 9 {
				genreNames = append(genreNames, genre.Name)
			} else if i == 8 {
				genreNames = append(genreNames, fmt.Sprintf("... and %d more", len(movie.Genres)-8))
				break
			}
		}
		rows = append(rows, []string{"Genres", strings.Join(genreNames, ", ")})
	}

	// Translations
	if len(movie.Translations) > 1 {
		langNames := []string{}
		for _, trans := range movie.Translations {
			langName := map[string]string{
				"en": "English",
				"ja": "Japanese",
				"zh": "Chinese",
				"ko": "Korean",
			}[trans.Language]
			if langName == "" {
				langName = trans.Language
			}
			langNames = append(langNames, fmt.Sprintf("%s (%s)", langName, trans.SourceName))
		}
		rows = append(rows, []string{"Translations", strings.Join(langNames, ", ")})
	}

	// Sources - collect unique sources from translations
	sourcesMap := make(map[string]bool)
	var sources []string

	// Add sources from translations
	for _, trans := range movie.Translations {
		if trans.SourceName != "" && !sourcesMap[trans.SourceName] {
			sourcesMap[trans.SourceName] = true
			sources = append(sources, trans.SourceName)
		}
	}

	// If no translations, fall back to movie.SourceName
	if len(sources) == 0 && movie.SourceName != "" {
		sources = append(sources, movie.SourceName)
	}

	// Display sources (names only in the main table)
	if len(sources) > 0 {
		rows = append(rows, []string{"Sources", strings.Join(sources, ", ")})
	}

	// Calculate column widths
	maxLabelWidth := 0
	for _, row := range rows {
		if len(row[0]) > maxLabelWidth {
			maxLabelWidth = len(row[0])
		}
	}

	// Terminal width for wrapping (default 120, can be adjusted)
	terminalWidth := 120
	valueWidth := terminalWidth - maxLabelWidth - 5 // Account for label, " : ", and padding

	// Helper function to wrap text to specified width
	wrapText := func(text string, width int) []string {
		if width <= 0 {
			width = 80
		}
		words := strings.Fields(text)
		if len(words) == 0 {
			return []string{""}
		}

		var lines []string
		currentLine := ""

		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}
		return lines
	}

	// Print table header
	fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))

	// Print rows with proper wrapping
	for _, row := range rows {
		label := row[0]
		value := row[1]

		// For multi-line values (description, media URLs), wrap them
		lines := wrapText(value, valueWidth)

		for i, line := range lines {
			if i == 0 {
				// First line: show label
				paddedLabel := label + strings.Repeat(" ", maxLabelWidth-len(label))
				fmt.Printf("%-*s : %s\n", maxLabelWidth, paddedLabel, line)
			} else {
				// Continuation lines: indent to align with first line's value
				fmt.Printf("%*s   %s\n", maxLabelWidth, "", line)
			}
		}
	}

	// Print Source URLs section (if we have scraperResults from fresh scrape)
	if results != nil && len(results) > 0 {
		fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))
		fmt.Println()
		fmt.Println("Source URLs:")
		fmt.Println()

		for _, result := range results {
			fmt.Printf("  %-12s : %s\n", result.Source, result.SourceURL)
		}
	}

	// Now print expanded media section
	if movie.CoverURL != "" || movie.PosterURL != "" || len(movie.Screenshots) > 0 || movie.TrailerURL != "" {
		fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))
		fmt.Println()
		fmt.Println("Media URLs:")
		fmt.Println()

		if movie.CoverURL != "" {
			fmt.Printf("  Cover URL    : %s\n", movie.CoverURL)
		}
		if movie.PosterURL != "" && movie.PosterURL != movie.CoverURL {
			fmt.Printf("  Poster URL   : %s\n", movie.PosterURL)
		}
		if movie.TrailerURL != "" {
			fmt.Printf("  Trailer URL  : %s\n", movie.TrailerURL)
		}
		if len(movie.Screenshots) > 0 {
			fmt.Printf("  Screenshots  : %d total\n", len(movie.Screenshots))
			for i, url := range movie.Screenshots {
				fmt.Printf("    [%2d] %s\n", i+1, url)
			}
		}
	}

	// Description section (full text, properly wrapped)
	if movie.Description != "" {
		fmt.Println()
		fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))
		fmt.Println()
		fmt.Println("Description:")
		fmt.Println()

		// Wrap description to terminal width with some padding
		descLines := wrapText(movie.Description, terminalWidth-4)
		for _, line := range descLines {
			fmt.Printf("  %s\n", line)
		}
	}

	// Print table footer
	fmt.Println()
	fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))
	fmt.Println()
}
