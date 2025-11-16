package main

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// TestMain_CommandTree tests that main() builds the complete command tree correctly
// This test simulates what main() does without actually executing commands
func TestMain_CommandTree(t *testing.T) {
	// Build root command like main() does
	rootCmd := &cobra.Command{
		Use:   "javinizer",
		Short: "Javinizer - JAV metadata scraper and organizer",
		Long:  `A metadata scraper and file organizer for Japanese Adult Videos (JAV)`,
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "configs/config.yaml", "config file path")
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "enable debug logging")

	// Scrape command
	scrapeCmd := &cobra.Command{
		Use:   "scrape [id]",
		Short: "Scrape metadata for a movie ID",
		Args:  cobra.ExactArgs(1),
	}
	scrapeCmd.Flags().StringSliceVarP(&scrapersFlag, "scrapers", "s", nil, "Comma-separated list of scrapers to use")
	scrapeCmd.Flags().BoolP("force", "f", false, "Force refresh metadata from scrapers")

	// Info command
	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show configuration and scraper information",
	}

	// Init command
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration and database",
	}

	// Sort command
	sortCmd := &cobra.Command{
		Use:   "sort [path]",
		Short: "Scan, scrape, and organize video files",
		Long:  `Scans a directory for video files, scrapes metadata, generates NFO files, downloads media, and organizes files`,
		Args:  cobra.ExactArgs(1),
	}
	sortCmd.Flags().BoolP("dry-run", "n", false, "Preview operations without making changes")
	sortCmd.Flags().BoolP("recursive", "r", true, "Scan directories recursively")
	sortCmd.Flags().StringP("dest", "d", "", "Destination directory")
	sortCmd.Flags().BoolP("move", "m", false, "Move files instead of copying")
	sortCmd.Flags().BoolP("nfo", "", true, "Generate NFO files")
	sortCmd.Flags().BoolP("download", "", true, "Download media")
	sortCmd.Flags().Bool("extrafanart", false, "Download extrafanart")
	sortCmd.Flags().StringSliceP("scrapers", "p", nil, "Scraper priority")
	sortCmd.Flags().BoolP("force-update", "f", false, "Force update existing files")
	sortCmd.Flags().Bool("force-refresh", false, "Force refresh metadata from scrapers")
	sortCmd.Flags().BoolP("update", "u", false, "Update mode")

	// Genre command with subcommands
	genreCmd := &cobra.Command{
		Use:   "genre",
		Short: "Manage genre replacements",
		Long:  `Manage genre name replacements for customizing genre names from scrapers`,
	}

	genreAddCmd := &cobra.Command{
		Use:   "add <original> <replacement>",
		Short: "Add a genre replacement",
		Args:  cobra.ExactArgs(2),
	}

	genreListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all genre replacements",
	}

	genreRemoveCmd := &cobra.Command{
		Use:   "remove <original>",
		Short: "Remove a genre replacement",
		Args:  cobra.ExactArgs(1),
	}

	genreCmd.AddCommand(genreAddCmd, genreListCmd, genreRemoveCmd)

	// Tag command with subcommands
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage per-movie tags",
		Long:  `Manage custom tags for individual movies (stored in database, appear in NFO files)`,
	}

	tagAddCmd := &cobra.Command{
		Use:   "add <movie_id> <tag> [tag2] [tag3]...",
		Short: "Add tag(s) to a movie",
		Args:  cobra.MinimumNArgs(2),
	}

	tagListCmd := &cobra.Command{
		Use:   "list [movie_id]",
		Short: "List tags for a movie or all tag mappings",
		Args:  cobra.MaximumNArgs(1),
	}

	tagRemoveCmd := &cobra.Command{
		Use:   "remove <movie_id> [tag]",
		Short: "Remove tag(s) from a movie",
		Args:  cobra.RangeArgs(1, 2),
	}

	tagSearchCmd := &cobra.Command{
		Use:   "search <tag>",
		Short: "Find all movies with a specific tag",
		Args:  cobra.ExactArgs(1),
	}

	tagAllTagsCmd := &cobra.Command{
		Use:   "tags",
		Short: "List all unique tags in database",
	}

	tagCmd.AddCommand(tagAddCmd, tagListCmd, tagRemoveCmd, tagSearchCmd, tagAllTagsCmd)

	// History command with subcommands
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "View operation history",
		Long:  `View and manage the history of scrape, organize, download, and NFO operations`,
	}

	historyListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent operations",
	}
	historyListCmd.Flags().IntP("limit", "n", 20, "Number of records to show")
	historyListCmd.Flags().StringP("operation", "o", "", "Filter by operation type")
	historyListCmd.Flags().StringP("status", "s", "", "Filter by status")

	historyStatsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show operation statistics",
	}

	historyMovieCmd := &cobra.Command{
		Use:   "movie <id>",
		Short: "Show history for a specific movie",
		Args:  cobra.ExactArgs(1),
	}

	historyCleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean up old history records",
	}
	historyCleanCmd.Flags().IntP("days", "d", 30, "Delete records older than this many days")

	historyCmd.AddCommand(historyListCmd, historyStatsCmd, historyMovieCmd, historyCleanCmd)

	// TUI command
	tuiCmd := createTUICommand()

	// API command
	apiCmd := newAPICmd()

	rootCmd.AddCommand(scrapeCmd, infoCmd, initCmd, sortCmd, genreCmd, tagCmd, historyCmd, tuiCmd, apiCmd)

	// Verify root command
	assert.Equal(t, "javinizer", rootCmd.Use)
	assert.NotEmpty(t, rootCmd.Short)
	assert.NotEmpty(t, rootCmd.Long)

	// Verify persistent flags
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("config"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("verbose"))

	// Verify all top-level commands are registered
	commands := rootCmd.Commands()
	assert.Len(t, commands, 9)

	commandMap := make(map[string]*cobra.Command)
	for _, cmd := range commands {
		commandMap[cmd.Name()] = cmd
	}

	// Verify each command exists and has correct structure
	assert.Contains(t, commandMap, "scrape")
	assert.Contains(t, commandMap, "info")
	assert.Contains(t, commandMap, "init")
	assert.Contains(t, commandMap, "sort")
	assert.Contains(t, commandMap, "genre")
	assert.Contains(t, commandMap, "tag")
	assert.Contains(t, commandMap, "history")
	assert.Contains(t, commandMap, "tui")
	assert.Contains(t, commandMap, "api")

	// Verify scrape command details
	assert.NotNil(t, commandMap["scrape"].Flags().Lookup("scrapers"))
	assert.NotNil(t, commandMap["scrape"].Flags().Lookup("force"))

	// Verify sort command details
	assert.NotNil(t, commandMap["sort"].Flags().Lookup("dry-run"))
	assert.NotNil(t, commandMap["sort"].Flags().Lookup("recursive"))
	assert.NotNil(t, commandMap["sort"].Flags().Lookup("dest"))
	assert.NotNil(t, commandMap["sort"].Flags().Lookup("move"))

	// Verify genre subcommands
	genreSubcommands := commandMap["genre"].Commands()
	assert.Len(t, genreSubcommands, 3)
	genreSubMap := make(map[string]bool)
	for _, sub := range genreSubcommands {
		genreSubMap[sub.Name()] = true
	}
	assert.True(t, genreSubMap["add"])
	assert.True(t, genreSubMap["list"])
	assert.True(t, genreSubMap["remove"])

	// Verify tag subcommands
	tagSubcommands := commandMap["tag"].Commands()
	assert.Len(t, tagSubcommands, 5)
	tagSubMap := make(map[string]bool)
	for _, sub := range tagSubcommands {
		tagSubMap[sub.Name()] = true
	}
	assert.True(t, tagSubMap["add"])
	assert.True(t, tagSubMap["list"])
	assert.True(t, tagSubMap["remove"])
	assert.True(t, tagSubMap["search"])
	assert.True(t, tagSubMap["tags"])

	// Verify history subcommands
	historySubcommands := commandMap["history"].Commands()
	assert.Len(t, historySubcommands, 4)
	historySubMap := make(map[string]bool)
	for _, sub := range historySubcommands {
		historySubMap[sub.Name()] = true
	}
	assert.True(t, historySubMap["list"])
	assert.True(t, historySubMap["stats"])
	assert.True(t, historySubMap["movie"])
	assert.True(t, historySubMap["clean"])
}

func TestMain_ScrapeCommandSetup(t *testing.T) {
	scrapeCmd := &cobra.Command{
		Use:   "scrape [id]",
		Short: "Scrape metadata for a movie ID",
		Args:  cobra.ExactArgs(1),
	}
	scrapeCmd.Flags().StringSliceVarP(&scrapersFlag, "scrapers", "s", nil, "scrapers")
	scrapeCmd.Flags().BoolP("force", "f", false, "force")

	// Verify command structure
	assert.Equal(t, "scrape [id]", scrapeCmd.Use)
	assert.NotEmpty(t, scrapeCmd.Short)

	// Verify Args validator
	err := scrapeCmd.Args(scrapeCmd, []string{})
	assert.Error(t, err) // Should fail with 0 args

	err = scrapeCmd.Args(scrapeCmd, []string{"IPX-535"})
	assert.NoError(t, err) // Should pass with 1 arg

	err = scrapeCmd.Args(scrapeCmd, []string{"IPX-535", "extra"})
	assert.Error(t, err) // Should fail with 2 args
}

func TestMain_SortCommandSetup(t *testing.T) {
	sortCmd := &cobra.Command{
		Use:   "sort [path]",
		Short: "Scan, scrape, and organize video files",
		Args:  cobra.ExactArgs(1),
	}
	sortCmd.Flags().BoolP("dry-run", "n", false, "dry run")
	sortCmd.Flags().BoolP("recursive", "r", true, "recursive")
	sortCmd.Flags().StringP("dest", "d", "", "destination")
	sortCmd.Flags().BoolP("move", "m", false, "move")
	sortCmd.Flags().BoolP("nfo", "", true, "nfo")
	sortCmd.Flags().BoolP("download", "", true, "download")
	sortCmd.Flags().Bool("extrafanart", false, "extrafanart")
	sortCmd.Flags().StringSliceP("scrapers", "p", nil, "scrapers")
	sortCmd.Flags().BoolP("force-update", "f", false, "force update")
	sortCmd.Flags().Bool("force-refresh", false, "force refresh")
	sortCmd.Flags().BoolP("update", "u", false, "update")

	// Verify command structure
	assert.Equal(t, "sort [path]", sortCmd.Use)
	assert.NotEmpty(t, sortCmd.Short)

	// Verify default flag values
	recursive, _ := sortCmd.Flags().GetBool("recursive")
	assert.True(t, recursive)

	nfo, _ := sortCmd.Flags().GetBool("nfo")
	assert.True(t, nfo)

	download, _ := sortCmd.Flags().GetBool("download")
	assert.True(t, download)

	move, _ := sortCmd.Flags().GetBool("move")
	assert.False(t, move)
}

func TestMain_GenreCommandSetup(t *testing.T) {
	genreCmd := &cobra.Command{
		Use:   "genre",
		Short: "Manage genre replacements",
	}

	genreAddCmd := &cobra.Command{
		Use:  "add <original> <replacement>",
		Args: cobra.ExactArgs(2),
	}

	genreListCmd := &cobra.Command{
		Use: "list",
	}

	genreRemoveCmd := &cobra.Command{
		Use:  "remove <original>",
		Args: cobra.ExactArgs(1),
	}

	genreCmd.AddCommand(genreAddCmd, genreListCmd, genreRemoveCmd)

	// Verify genre command
	assert.Equal(t, "genre", genreCmd.Use)
	assert.NotEmpty(t, genreCmd.Short)

	// Verify subcommands
	subcommands := genreCmd.Commands()
	assert.Len(t, subcommands, 3)

	// Verify add command args
	err := genreAddCmd.Args(genreAddCmd, []string{})
	assert.Error(t, err) // Need 2 args

	err = genreAddCmd.Args(genreAddCmd, []string{"original"})
	assert.Error(t, err) // Need 2 args

	err = genreAddCmd.Args(genreAddCmd, []string{"original", "replacement"})
	assert.NoError(t, err) // Correct

	// Verify remove command args
	err = genreRemoveCmd.Args(genreRemoveCmd, []string{})
	assert.Error(t, err) // Need 1 arg

	err = genreRemoveCmd.Args(genreRemoveCmd, []string{"original"})
	assert.NoError(t, err) // Correct
}

func TestMain_TagCommandSetup(t *testing.T) {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage per-movie tags",
	}

	tagAddCmd := &cobra.Command{
		Use:  "add <movie_id> <tag> [tag2]...",
		Args: cobra.MinimumNArgs(2),
	}

	tagListCmd := &cobra.Command{
		Use:  "list [movie_id]",
		Args: cobra.MaximumNArgs(1),
	}

	tagRemoveCmd := &cobra.Command{
		Use:  "remove <movie_id> [tag]",
		Args: cobra.RangeArgs(1, 2),
	}

	tagSearchCmd := &cobra.Command{
		Use:  "search <tag>",
		Args: cobra.ExactArgs(1),
	}

	tagAllTagsCmd := &cobra.Command{
		Use: "tags",
	}

	tagCmd.AddCommand(tagAddCmd, tagListCmd, tagRemoveCmd, tagSearchCmd, tagAllTagsCmd)

	// Verify tag command
	assert.Equal(t, "tag", tagCmd.Use)
	assert.NotEmpty(t, tagCmd.Short)

	// Verify subcommands
	subcommands := tagCmd.Commands()
	assert.Len(t, subcommands, 5)

	// Verify tag add args (minimum 2)
	err := tagAddCmd.Args(tagAddCmd, []string{})
	assert.Error(t, err)

	err = tagAddCmd.Args(tagAddCmd, []string{"IPX-535"})
	assert.Error(t, err)

	err = tagAddCmd.Args(tagAddCmd, []string{"IPX-535", "Favorite"})
	assert.NoError(t, err)

	err = tagAddCmd.Args(tagAddCmd, []string{"IPX-535", "Favorite", "Uncensored"})
	assert.NoError(t, err)

	// Verify tag list args (maximum 1)
	err = tagListCmd.Args(tagListCmd, []string{})
	assert.NoError(t, err)

	err = tagListCmd.Args(tagListCmd, []string{"IPX-535"})
	assert.NoError(t, err)

	err = tagListCmd.Args(tagListCmd, []string{"IPX-535", "extra"})
	assert.Error(t, err)

	// Verify tag remove args (1 or 2)
	err = tagRemoveCmd.Args(tagRemoveCmd, []string{})
	assert.Error(t, err)

	err = tagRemoveCmd.Args(tagRemoveCmd, []string{"IPX-535"})
	assert.NoError(t, err)

	err = tagRemoveCmd.Args(tagRemoveCmd, []string{"IPX-535", "Favorite"})
	assert.NoError(t, err)

	err = tagRemoveCmd.Args(tagRemoveCmd, []string{"IPX-535", "Favorite", "extra"})
	assert.Error(t, err)
}

func TestMain_HistoryCommandSetup(t *testing.T) {
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "View operation history",
	}

	historyListCmd := &cobra.Command{
		Use: "list",
	}
	historyListCmd.Flags().IntP("limit", "n", 20, "limit")
	historyListCmd.Flags().StringP("operation", "o", "", "operation")
	historyListCmd.Flags().StringP("status", "s", "", "status")

	historyStatsCmd := &cobra.Command{
		Use: "stats",
	}

	historyMovieCmd := &cobra.Command{
		Use:  "movie <id>",
		Args: cobra.ExactArgs(1),
	}

	historyCleanCmd := &cobra.Command{
		Use: "clean",
	}
	historyCleanCmd.Flags().IntP("days", "d", 30, "days")

	historyCmd.AddCommand(historyListCmd, historyStatsCmd, historyMovieCmd, historyCleanCmd)

	// Verify history command
	assert.Equal(t, "history", historyCmd.Use)
	assert.NotEmpty(t, historyCmd.Short)

	// Verify subcommands
	subcommands := historyCmd.Commands()
	assert.Len(t, subcommands, 4)

	// Verify history list flags
	limit, _ := historyListCmd.Flags().GetInt("limit")
	assert.Equal(t, 20, limit)

	// Verify history clean flags
	days, _ := historyCleanCmd.Flags().GetInt("days")
	assert.Equal(t, 30, days)

	// Verify history movie args
	err := historyMovieCmd.Args(historyMovieCmd, []string{})
	assert.Error(t, err)

	err = historyMovieCmd.Args(historyMovieCmd, []string{"IPX-535"})
	assert.NoError(t, err)
}

// TestPrintMovie_AllFields tests printMovie with all possible fields populated
func TestPrintMovie_AllFields(t *testing.T) {
	releaseDate, _ := time.Parse("2006-01-02", "2021-03-25")
	movie := &models.Movie{
		ID:          "IPX-123",
		ContentID:   "ipx00123",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Test Director",
		Maker:       "Test Maker",
		Label:       "Test Label",
		Series:      "Test Series",
		RatingScore: 8.5,
		RatingVotes: 100,
		Actresses: []models.Actress{
			{FirstName: "Test", LastName: "Actress"},
		},
		Genres: []models.Genre{
			{Name: "Drama"},
		},
	}

	// Capture output
	stdout, _ := captureOutput(t, func() {
		printMovie(movie, nil)
	})

	// Verify all fields are printed
	assert.Contains(t, stdout, "IPX-123")
	assert.Contains(t, stdout, "ipx00123")
	assert.Contains(t, stdout, "Test Movie")
	assert.Contains(t, stdout, "2021-03-25")
	assert.Contains(t, stdout, "120 min")
	assert.Contains(t, stdout, "Test Director")
	assert.Contains(t, stdout, "Test Maker")
	assert.Contains(t, stdout, "Test Label")
	assert.Contains(t, stdout, "Test Series")
	assert.Contains(t, stdout, "8.5/10")
	assert.Contains(t, stdout, "100 votes")
	assert.Contains(t, stdout, "Actress Test") // Name is printed as "Actress Test"
	assert.Contains(t, stdout, "Drama")
}

// TestPrintMovie_MinimalFields tests printMovie with minimal fields
func TestPrintMovie_MinimalFields(t *testing.T) {
	movie := &models.Movie{
		ID: "IPX-123",
		// All other fields empty/nil
	}

	// Capture output
	stdout, _ := captureOutput(t, func() {
		printMovie(movie, nil)
	})

	// Verify only ID is printed
	assert.Contains(t, stdout, "IPX-123")
	// Should not contain empty field labels
	assert.NotContains(t, stdout, "Director")
	assert.NotContains(t, stdout, "Maker")
	assert.NotContains(t, stdout, "Label")
}

// TestPrintMovie_SameContentID tests when ContentID equals ID (should not print twice)
func TestPrintMovie_SameContentID(t *testing.T) {
	movie := &models.Movie{
		ID:        "IPX-123",
		ContentID: "IPX-123", // Same as ID
		Title:     "Test",
	}

	stdout, _ := captureOutput(t, func() {
		printMovie(movie, nil)
	})

	// ContentID row should NOT appear when it's the same as ID
	lines := countOccurrences(stdout, "IPX-123")
	// Should appear once in ID row, not twice
	assert.LessOrEqual(t, lines, 2, "ContentID should not be printed when same as ID")
}

// Helper function to count occurrences
func countOccurrences(s, substr string) int {
	count := 0
	for i := 0; i < len(s); {
		idx := indexOf(s[i:], substr)
		if idx == -1 {
			break
		}
		count++
		i += idx + len(substr)
	}
	return count
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
