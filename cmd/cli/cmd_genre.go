package main

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
)

func newGenreCmd() *cobra.Command {
	genreCmd := &cobra.Command{
		Use:   "genre",
		Short: "Manage genre replacements",
		Long:  `Manage genre name replacements for customizing genre names from scrapers`,
	}

	genreAddCmd := &cobra.Command{
		Use:   "add <original> <replacement>",
		Short: "Add a genre replacement",
		Args:  cobra.ExactArgs(2),
		RunE:  runWithDeps(runGenreAdd),
	}

	genreListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all genre replacements",
		RunE:  runWithDeps(runGenreList),
	}

	genreRemoveCmd := &cobra.Command{
		Use:   "remove <original>",
		Short: "Remove a genre replacement",
		Args:  cobra.ExactArgs(1),
		RunE:  runWithDeps(runGenreRemove),
	}

	genreCmd.AddCommand(genreAddCmd, genreListCmd, genreRemoveCmd)
	return genreCmd
}

func runGenreAdd(cmd *cobra.Command, args []string, deps *Dependencies) error {
	original := args[0]
	replacement := args[1]

	repo := database.NewGenreReplacementRepository(deps.DB)

	genreReplacement := &models.GenreReplacement{
		Original:    original,
		Replacement: replacement,
	}

	if err := repo.Upsert(genreReplacement); err != nil {
		return fmt.Errorf("Failed to add genre replacement: %v", err)
	}

	fmt.Printf("✅ Genre replacement added: '%s' → '%s'\n", original, replacement)

	return nil
}

func runGenreList(cmd *cobra.Command, args []string, deps *Dependencies) error {
	repo := database.NewGenreReplacementRepository(deps.DB)

	replacements, err := repo.List()
	if err != nil {
		return fmt.Errorf("Failed to list genre replacements: %v", err)
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

func runGenreRemove(cmd *cobra.Command, args []string, deps *Dependencies) error {
	original := args[0]

	repo := database.NewGenreReplacementRepository(deps.DB)

	if err := repo.Delete(original); err != nil {
		return fmt.Errorf("Failed to remove genre replacement: %v", err)
	}

	fmt.Printf("✅ Genre replacement removed: '%s'\n", original)

	return nil
}
