package actress

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
)

// NewCommand creates the actress command.
func NewCommand() *cobra.Command {
	actressCmd := &cobra.Command{
		Use:   "actress",
		Short: "Manage actress records",
		Long:  "Manage actress records and merge duplicated actresses",
	}

	mergeCmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge duplicated actresses",
		Long:  "Merge a source actress into a target actress with field-level conflict resolution",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runMerge(cmd, configFile)
		},
	}

	mergeCmd.Flags().Uint("target", 0, "Target actress ID to keep")
	mergeCmd.Flags().Uint("source", 0, "Source actress ID to merge and delete")
	mergeCmd.Flags().Bool("non-interactive", false, "Do not prompt; apply a global preference to all conflicts")
	mergeCmd.Flags().String("prefer", "target", "Conflict preference for non-interactive mode: target or source")
	mergeCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	_ = mergeCmd.MarkFlagRequired("target")
	_ = mergeCmd.MarkFlagRequired("source")

	exportCmd := &cobra.Command{
		Use:   "export [output.json]",
		Short: "Export actresses",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runActressExport(cmd, args, configFile)
		},
	}

	importCmd := &cobra.Command{
		Use:   "import <input.json>",
		Short: "Import actresses from JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runActressImport(cmd, args, configFile)
		},
	}

	actressCmd.AddCommand(mergeCmd, exportCmd, importCmd)
	return actressCmd
}

func runMerge(cmd *cobra.Command, configFile string) error {
	targetID, _ := cmd.Flags().GetUint("target")
	sourceID, _ := cmd.Flags().GetUint("source")
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	prefer, _ := cmd.Flags().GetString("prefer")
	skipConfirm, _ := cmd.Flags().GetBool("yes")

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewActressRepository(deps.GetDB())
	preview, err := repo.PreviewMerge(targetID, sourceID)
	if err != nil {
		return err
	}

	resolutions := make(map[string]string, len(preview.DefaultResolutions))
	for field, decision := range preview.DefaultResolutions {
		resolutions[field] = decision
	}

	if nonInteractive {
		prefer = strings.ToLower(strings.TrimSpace(prefer))
		if prefer != "target" && prefer != "source" {
			return fmt.Errorf("invalid --prefer value: %s (expected target or source)", prefer)
		}
		for _, conflict := range preview.Conflicts {
			resolutions[conflict.Field] = prefer
		}
	} else {
		if err := promptMergeResolutions(cmd, preview, resolutions); err != nil {
			return err
		}
	}

	if !skipConfirm && !nonInteractive {
		if !promptConfirmation(cmd) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Merge cancelled")
			return nil
		}
	}

	result, err := repo.Merge(targetID, sourceID, resolutions)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Merged actress #%d into #%d\n", result.MergedFromID, result.MergedActress.ID)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated movies: %d\n", result.UpdatedMovies)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Conflicts resolved: %d\n", result.ConflictsResolved)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Aliases added: %d\n", result.AliasesAdded)

	return nil
}

func promptMergeResolutions(cmd *cobra.Command, preview *database.ActressMergePreview, resolutions map[string]string) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Preparing merge source #%d -> target #%d\n", preview.Source.ID, preview.Target.ID)

	if len(preview.Conflicts) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No field conflicts detected; default merge behavior will be applied")
		return nil
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Detected %d conflicting field(s)\n", len(preview.Conflicts))
	for _, conflict := range preview.Conflicts {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nField: %s\n", conflict.Field)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  target: %v\n", conflict.TargetValue)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  source: %v\n", conflict.SourceValue)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Choose value [t=target/s=source, default=t]: ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		choice := strings.ToLower(strings.TrimSpace(line))
		switch choice {
		case "", "t", "target":
			resolutions[conflict.Field] = "target"
		case "s", "source":
			resolutions[conflict.Field] = "source"
		default:
			return fmt.Errorf("invalid choice for field %s: %s", conflict.Field, choice)
		}
	}

	return nil
}

func promptConfirmation(cmd *cobra.Command) bool {
	reader := bufio.NewReader(cmd.InOrStdin())
	_, _ = fmt.Fprint(cmd.OutOrStdout(), "\nApply merge? [y/N]: ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func runActressExport(cmd *cobra.Command, args []string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewActressRepository(deps.GetDB())
	actresses, err := repo.List(0, 0)
	if err != nil {
		return fmt.Errorf("failed to list actresses: %v", err)
	}

	data, err := json.MarshalIndent(actresses, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	if len(args) == 0 {
		_, _ = cmd.OutOrStdout().Write(data)
		_, _ = cmd.OutOrStdout().Write([]byte("\n"))
		_, _ = fmt.Printf("Exported %d actress(es) to stdout\n", len(actresses))
	} else {
		if err := os.WriteFile(args[0], data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %v", err)
		}
		fmt.Printf("Exported %d actress(es) to %s\n", len(actresses), args[0])
	}

	return nil
}

func runActressImport(cmd *cobra.Command, args []string, configFile string) error {
	fileData, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	var actresses []models.Actress
	if err := json.Unmarshal(fileData, &actresses); err != nil {
		return fmt.Errorf("failed to parse JSON: %v", err)
	}

	if len(actresses) == 0 {
		return fmt.Errorf("no actresses found in import file")
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

	repo := database.NewActressRepository(deps.GetDB())
	imported := 0
	skipped := 0
	errorsCount := 0

	for i := range actresses {
		a := &actresses[i]
		if a.ID > 0 {
			existing, err := repo.FindByID(a.ID)
			if err == nil {
				if existing.FirstName == a.FirstName && existing.LastName == a.LastName &&
					existing.JapaneseName == a.JapaneseName && existing.ThumbURL == a.ThumbURL &&
					existing.Aliases == a.Aliases && existing.DMMID == a.DMMID {
					skipped++
					continue
				}
				a.UpdatedAt = existing.UpdatedAt
				if err := repo.Update(a); err != nil {
					errorsCount++
					continue
				}
				imported++
				continue
			}
			if err := repo.Create(a); err != nil {
				errorsCount++
				continue
			}
			imported++
		} else {
			var existing *models.Actress
			if a.JapaneseName != "" {
				existing, err = repo.FindByJapaneseName(a.JapaneseName)
				if err == nil {
					if existing.FirstName == a.FirstName && existing.LastName == a.LastName &&
						existing.ThumbURL == a.ThumbURL && existing.Aliases == a.Aliases &&
						existing.DMMID == a.DMMID {
						skipped++
						continue
					}
					a.ID = existing.ID
					a.CreatedAt = existing.CreatedAt
					if err := repo.Update(a); err != nil {
						errorsCount++
						continue
					}
					imported++
					continue
				}
			}
			if err := repo.Create(a); err != nil {
				errorsCount++
				continue
			}
			imported++
		}
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Imported: %d, Skipped: %d, Errors: %d\n", imported, skipped, errorsCount)

	return nil
}
