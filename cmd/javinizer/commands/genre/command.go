package genre

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
)

// NewCommand creates the genre command
func NewCommand() *cobra.Command {
	genreCmd := &cobra.Command{
		Use:   "genre",
		Short: "Manage genre replacements",
		Long:  `Manage genre name replacements for customizing genre names from scrapers`,
	}

	genreAddCmd := &cobra.Command{
		Use:   "add <original> <replacement>",
		Short: "Add a genre replacement",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runAdd(cmd, args, configFile)
		},
	}

	genreListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all genre replacements",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runList(cmd, args, configFile)
		},
	}

	genreRemoveCmd := &cobra.Command{
		Use:   "remove <original>",
		Short: "Remove a genre replacement",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runRemove(cmd, args, configFile)
		},
	}

	genreExportCmd := &cobra.Command{
		Use:   "export [output.json]",
		Short: "Export genre replacements",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runGenreExport(cmd, args, configFile)
		},
	}

	genreImportCmd := &cobra.Command{
		Use:   "import <input.json>",
		Short: "Import genre replacements from JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runGenreImport(cmd, args, configFile)
		},
	}

	genreCmd.AddCommand(genreAddCmd, genreListCmd, genreRemoveCmd, genreExportCmd, genreImportCmd)
	return genreCmd
}

func runAdd(cmd *cobra.Command, args []string, configFile string) error {
	original := args[0]
	replacement := args[1]

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

	repo := database.NewGenreReplacementRepository(deps.DB)

	genreReplacement := &models.GenreReplacement{
		Original:    original,
		Replacement: replacement,
	}

	if err := repo.Upsert(genreReplacement); err != nil {
		return fmt.Errorf("failed to add genre replacement: %v", err)
	}

	fmt.Printf("✅ Genre replacement added: '%s' → '%s'\n", original, replacement)

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
	defer func() { _ = deps.Close() }()

	repo := database.NewGenreReplacementRepository(deps.DB)

	replacements, err := repo.List()
	if err != nil {
		return fmt.Errorf("failed to list genre replacements: %v", err)
	}

	if len(replacements) == 0 {
		fmt.Println("No genre replacements configured")
		return nil
	}

	fmt.Println("=== Genre Replacements ===")
	fmt.Printf("%-30s → %s\n", "Original", "Replacement")
	fmt.Println(strings.Repeat("-", 65))

	for _, r := range replacements {
		fmt.Printf("%-30s → %s\n", r.Original, r.Replacement)
	}

	fmt.Printf("\nTotal: %d replacements\n", len(replacements))

	return nil
}

func runRemove(cmd *cobra.Command, args []string, configFile string) error {
	original := args[0]

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

	repo := database.NewGenreReplacementRepository(deps.DB)

	if err := repo.Delete(original); err != nil {
		return fmt.Errorf("failed to remove genre replacement: %v", err)
	}

	fmt.Printf("✅ Genre replacement removed: '%s'\n", original)

	return nil
}

func runGenreExport(cmd *cobra.Command, args []string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewGenreReplacementRepository(deps.DB)

	replacements, err := repo.List()
	if err != nil {
		return fmt.Errorf("failed to list genre replacements: %v", err)
	}

	sort.Slice(replacements, func(i, j int) bool {
		return replacements[i].Original < replacements[j].Original
	})

	data, err := json.MarshalIndent(replacements, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	if len(args) == 0 {
		_, _ = cmd.OutOrStdout().Write(data)
		_, _ = cmd.OutOrStdout().Write([]byte("\n"))
		fmt.Printf("Exported %d genre replacement(s) to stdout\n", len(replacements))
	} else {
		if err := os.WriteFile(args[0], data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %v", err)
		}
		fmt.Printf("Exported %d genre replacement(s) to %s\n", len(replacements), args[0])
	}

	return nil
}

func runGenreImport(cmd *cobra.Command, args []string, configFile string) error {
	fileData, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	var replacements []models.GenreReplacement
	if err := json.Unmarshal(fileData, &replacements); err != nil {
		return fmt.Errorf("failed to parse JSON: %v", err)
	}

	if len(replacements) == 0 {
		return fmt.Errorf("no genre replacements found in import file")
	}

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewGenreReplacementRepository(deps.DB)

	imported := 0
	skipped := 0
	errorsCount := 0

	for i := range replacements {
		r := &replacements[i]
		existing, err := repo.FindByOriginal(r.Original)
		if err == nil {
			if existing.Replacement == r.Replacement {
				skipped++
				continue
			}
		}

		if err := repo.Upsert(r); err != nil {
			errorsCount++
			continue
		}
		imported++
	}

	fmt.Printf("Imported: %d, Skipped: %d, Errors: %d\n", imported, skipped, errorsCount)

	return nil
}
