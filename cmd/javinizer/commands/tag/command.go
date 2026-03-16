package tag

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/cobra"
)

// NewCommand creates the tag command
func NewCommand() *cobra.Command {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage per-movie tags",
		Long:  `Manage custom tags for individual movies (stored in database, appear in NFO files)`,
	}

	tagAddCmd := &cobra.Command{
		Use:   "add <movie_id> <tag> [tag2] [tag3]...",
		Short: "Add tag(s) to a movie",
		Long: `Add one or more tags to a specific movie. Tags will appear in the movie's NFO file.

Examples:
  javinizer tag add IPX-535 "Favorite" "Uncensored"
  javinizer tag add ABC-123 "Collection: Summer 2023"`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runAdd(cmd, args, configFile)
		},
	}

	tagListCmd := &cobra.Command{
		Use:   "list [movie_id]",
		Short: "List tags for a movie or all tag mappings",
		Long: `List tags for a specific movie, or show all tag mappings if no movie ID provided.

Examples:
  javinizer tag list              # Show all tag mappings
  javinizer tag list IPX-535      # Show tags for IPX-535`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runList(cmd, args, configFile)
		},
	}

	tagRemoveCmd := &cobra.Command{
		Use:   "remove <movie_id> [tag]",
		Short: "Remove tag(s) from a movie",
		Long: `Remove a specific tag from a movie, or all tags if no tag specified.

Examples:
  javinizer tag remove IPX-535 "Favorite"    # Remove one tag
  javinizer tag remove IPX-535               # Remove all tags`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runRemove(cmd, args, configFile)
		},
	}

	tagSearchCmd := &cobra.Command{
		Use:   "search <tag>",
		Short: "Find all movies with a specific tag",
		Long: `Search for all movies that have been tagged with the specified tag.

Example:
  javinizer tag search "Favorite"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runSearch(cmd, args, configFile)
		},
	}

	tagAllTagsCmd := &cobra.Command{
		Use:   "tags",
		Short: "List all unique tags in database",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runAllTags(cmd, args, configFile)
		},
	}

	tagCmd.AddCommand(tagAddCmd, tagListCmd, tagRemoveCmd, tagSearchCmd, tagAllTagsCmd)
	return tagCmd
}

func runAdd(cmd *cobra.Command, args []string, configFile string) error {
	movieID := args[0]
	tags := args[1:]

	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize dependencies
	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() {
		_ = deps.Close()
	}()

	repo := database.NewMovieTagRepository(deps.DB)

	addedTags := []string{}
	for _, tag := range tags {
		if err := repo.AddTag(movieID, tag); err != nil {
			// Check if it's a duplicate error (UNIQUE constraint)
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
				logging.Warnf("Tag '%s' already exists for %s, skipping", tag, movieID)
				continue
			}
			return fmt.Errorf("failed to add tag '%s': %v", tag, err)
		}
		addedTags = append(addedTags, tag)
	}

	if len(addedTags) == 1 {
		fmt.Printf("✅ Added tag '%s' to %s\n", addedTags[0], movieID)
	} else if len(addedTags) > 1 {
		fmt.Printf("✅ Added %d tags to %s: %v\n", len(addedTags), movieID, addedTags)
	} else {
		fmt.Println("ℹ️  No new tags added (all already exist)")
	}

	return nil
}

func runList(cmd *cobra.Command, args []string, configFile string) error {
	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize dependencies
	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() {
		_ = deps.Close()
	}()

	repo := database.NewMovieTagRepository(deps.DB)

	// List tags for specific movie
	if len(args) == 1 {
		movieID := args[0]
		tags, err := repo.GetTagsForMovie(movieID)
		if err != nil {
			return fmt.Errorf("failed to get tags: %v", err)
		}

		if len(tags) == 0 {
			fmt.Printf("No tags for %s\n", movieID)
			return nil
		}

		fmt.Printf("=== Tags for %s ===\n", movieID)
		for _, tag := range tags {
			fmt.Printf("  - %s\n", tag)
		}
		fmt.Printf("\nTotal: %d tags\n", len(tags))
		return nil
	}

	// List all tag mappings
	allTags, err := repo.ListAll()
	if err != nil {
		return fmt.Errorf("failed to list tags: %v", err)
	}

	if len(allTags) == 0 {
		fmt.Println("No tag mappings configured")
		return nil
	}

	fmt.Println("=== Movie Tag Mappings ===")
	fmt.Printf("%-20s → Tags\n", "Movie ID")
	fmt.Println(strings.Repeat("-", 70))

	for movieID, tags := range allTags {
		fmt.Printf("%-20s → %s\n", movieID, strings.Join(tags, ", "))
	}

	fmt.Printf("\nTotal: %d movies tagged\n", len(allTags))

	return nil
}

func runRemove(cmd *cobra.Command, args []string, configFile string) error {
	movieID := args[0]

	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize dependencies
	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() {
		_ = deps.Close()
	}()

	repo := database.NewMovieTagRepository(deps.DB)

	// Remove specific tag
	if len(args) == 2 {
		tag := args[1]
		if err := repo.RemoveTag(movieID, tag); err != nil {
			return fmt.Errorf("failed to remove tag: %v", err)
		}
		fmt.Printf("✅ Removed tag '%s' from %s\n", tag, movieID)
		return nil
	}

	// Remove all tags
	if err := repo.RemoveAllTags(movieID); err != nil {
		return fmt.Errorf("failed to remove tags: %v", err)
	}
	fmt.Printf("✅ Removed all tags from %s\n", movieID)

	return nil
}

func runSearch(cmd *cobra.Command, args []string, configFile string) error {
	tag := args[0]

	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize dependencies
	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewMovieTagRepository(deps.DB)

	movieIDs, err := repo.GetMoviesWithTag(tag)
	if err != nil {
		return fmt.Errorf("failed to search: %w", err)
	}

	if len(movieIDs) == 0 {
		fmt.Printf("No movies found with tag '%s'\n", tag)
		return nil
	}

	fmt.Printf("=== Movies with tag '%s' ===\n", tag)
	for _, id := range movieIDs {
		fmt.Printf("  - %s\n", id)
	}
	fmt.Printf("\nTotal: %d movies\n", len(movieIDs))
	return nil
}

func runAllTags(cmd *cobra.Command, args []string, configFile string) error {
	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize dependencies
	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewMovieTagRepository(deps.DB)

	tags, err := repo.GetUniqueTagsList()
	if err != nil {
		return fmt.Errorf("failed to list tags: %w", err)
	}

	if len(tags) == 0 {
		fmt.Println("No tags in database")
		return nil
	}

	fmt.Println("=== All Tags ===")
	for _, tag := range tags {
		// Count movies with this tag
		movies, err := repo.GetMoviesWithTag(tag)
		if err != nil {
			logging.Warnf("Failed to count movies for tag '%s': %v", tag, err)
			fmt.Printf("  - %-30s (error)\n", tag)
			continue
		}
		fmt.Printf("  - %-30s (%d movies)\n", tag, len(movies))
	}
	fmt.Printf("\nTotal: %d unique tags\n", len(tags))
	return nil
}
