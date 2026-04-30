package word

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

var defaultWordReplacements = map[string]bool{
	"[Recommended For Smartphones] ": true,
	"A*****t":                        true,
	"A*****ted":                      true,
	"A****p":                         true,
	"A***e":                          true,
	"B***d":                          true,
	"B**d":                           true,
	"C***d":                          true,
	"D******ed":                      true,
	"D******eful":                    true,
	"D***k":                          true,
	"D***king":                       true,
	"D**g":                           true,
	"D**gged":                        true,
	"F***":                           true,
	"F*****g":                        true,
	"F***e":                          true,
	"G*********d":                    true,
	"G*******g":                      true,
	"G******g":                       true,
	"H*********n":                    true,
	"H*******ed":                     true,
	"H*******m":                      true,
	"I****t":                         true,
	"I****tuous":                     true,
	"K****p":                         true,
	"K**l":                           true,
	"K**ler":                         true,
	"K*d":                            true,
	"Ko**ji":                         true,
	"Lo**ta":                         true,
	"M******r":                       true,
	"M****t":                         true,
	"M****ted":                       true,
	"M****ter":                       true,
	"M****ting":                      true,
	"P****h":                         true,
	"P****hment":                     true,
	"P*A":                            true,
	"R****g":                         true,
	"R**e":                           true,
	"R**ed":                          true,
	"R*pe":                           true,
	"S*********l":                    true,
	"S*********ls":                   true,
	"S********l":                     true,
	"S********n":                     true,
	"S******g":                       true,
	"S*****t":                        true,
	"S***e":                          true,
	"S***p":                          true,
	"S**t":                           true,
	"Sch**l":                         true,
	"Sch**lgirl":                     true,
	"Sch**lgirls":                    true,
	"SK**lful":                       true,
	"SK**ls":                         true,
	"StepB****************r":         true,
	"StepM************n":             true,
	"StumB**d":                       true,
	"T*****e":                        true,
	"U*********sly":                  true,
	"U**verse":                       true,
	"V*****e":                        true,
	"V*****ed":                       true,
	"V*****es":                       true,
	"V*****t":                        true,
	"Y********l":                     true,
	"D******e":                       true,
}

// NewCommand creates the word command
func NewCommand() *cobra.Command {
	wordCmd := &cobra.Command{
		Use:   "word",
		Short: "Manage word replacements",
		Long:  `Manage word replacements for uncensoring metadata strings`,
	}

	wordListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all word replacements",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runWordList(cmd, args, configFile)
		},
	}

	wordAddCmd := &cobra.Command{
		Use:   "add <original> <replacement>",
		Short: "Add a word replacement",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runWordAdd(cmd, args, configFile)
		},
	}

	wordRemoveCmd := &cobra.Command{
		Use:   "remove <original>",
		Short: "Remove a word replacement",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runWordRemove(cmd, args, configFile)
		},
	}

	wordExportCmd := &cobra.Command{
		Use:   "export [output.json]",
		Short: "Export word replacements",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runWordExport(cmd, args, configFile)
		},
	}

	wordImportCmd := &cobra.Command{
		Use:   "import <input.json>",
		Short: "Import word replacements from JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runWordImport(cmd, args, configFile)
		},
	}
	wordImportCmd.Flags().Bool("include-defaults", false, "Include seeded default replacements in import")

	wordCmd.AddCommand(wordListCmd, wordAddCmd, wordRemoveCmd, wordExportCmd, wordImportCmd)
	return wordCmd
}

func runWordList(cmd *cobra.Command, args []string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewWordReplacementRepository(deps.DB)

	replacements, err := repo.List()
	if err != nil {
		return fmt.Errorf("failed to list word replacements: %v", err)
	}

	if len(replacements) == 0 {
		fmt.Println("No word replacements configured")
		return nil
	}

	fmt.Println("=== Word Replacements ===")
	fmt.Printf("%-30s -> %s\n", "Original", "Replacement")
	fmt.Println(strings.Repeat("-", 65))

	for _, r := range replacements {
		fmt.Printf("%-30s -> %s\n", r.Original, r.Replacement)
	}

	fmt.Printf("\nTotal: %d replacements\n", len(replacements))

	return nil
}

func runWordAdd(cmd *cobra.Command, args []string, configFile string) error {
	original := args[0]
	replacement := args[1]

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewWordReplacementRepository(deps.DB)

	wordReplacement := &models.WordReplacement{
		Original:    original,
		Replacement: replacement,
	}

	if err := repo.Upsert(wordReplacement); err != nil {
		return fmt.Errorf("failed to add word replacement: %v", err)
	}

	fmt.Printf("Word replacement added: '%s' -> '%s'\n", original, replacement)

	return nil
}

func runWordRemove(cmd *cobra.Command, args []string, configFile string) error {
	original := args[0]

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewWordReplacementRepository(deps.DB)

	if err := repo.Delete(original); err != nil {
		return fmt.Errorf("failed to remove word replacement: %v", err)
	}

	fmt.Printf("Word replacement removed: '%s'\n", original)

	return nil
}

func runWordExport(cmd *cobra.Command, args []string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewWordReplacementRepository(deps.DB)

	replacements, err := repo.List()
	if err != nil {
		return fmt.Errorf("failed to list word replacements: %v", err)
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
		fmt.Printf("Exported %d word replacement(s) to stdout\n", len(replacements))
	} else {
		if err := os.WriteFile(args[0], data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %v", err)
		}
		fmt.Printf("Exported %d word replacement(s) to %s\n", len(replacements), args[0])
	}

	return nil
}

func runWordImport(cmd *cobra.Command, args []string, configFile string) error {
	includeDefaults, _ := cmd.Flags().GetBool("include-defaults")

	fileData, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	var replacements []models.WordReplacement
	if err := json.Unmarshal(fileData, &replacements); err != nil {
		return fmt.Errorf("failed to parse JSON: %v", err)
	}

	if len(replacements) == 0 {
		return fmt.Errorf("no word replacements found in import file")
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

	repo := database.NewWordReplacementRepository(deps.DB)

	imported := 0
	skipped := 0
	errorsCount := 0

	for i := range replacements {
		r := &replacements[i]

		if !includeDefaults && defaultWordReplacements[r.Original] {
			skipped++
			continue
		}

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
