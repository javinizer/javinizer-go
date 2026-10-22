package actress

import (
	"bufio"
	"context"
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
	mergeCmd.Flags().String("policy", database.MergePolicyFillEmpty, "Scalar policy: fill_empty or preserve_target (including blanks)")
	mergeCmd.Flags().StringToString("resolve", nil, "Explicit scalar decisions, e.g. last_name=target,dmm_id=source")
	mergeCmd.Flags().Bool("dry-run", false, "Preview resulting scalar fields without merging or deleting the source")
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
	cmd.SetIn(bufio.NewReader(cmd.InOrStdin()))
	targetID, _ := cmd.Flags().GetUint("target")
	sourceID, _ := cmd.Flags().GetUint("source")
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	prefer, _ := cmd.Flags().GetString("prefer")
	skipConfirm, _ := cmd.Flags().GetBool("yes")
	policy, _ := cmd.Flags().GetString("policy")
	explicitResolutions, _ := cmd.Flags().GetStringToString("resolve")
	resolutions, err := database.NormalizeActressMergeResolutions(explicitResolutions)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewActressRepository(deps.DB)
	preview, err := repo.PreviewMergeWithOptions(context.Background(), targetID, sourceID, database.ActressMergeOptions{Policy: policy, Resolutions: resolutions})
	if err != nil {
		return err
	}

	if nonInteractive {
		prefer = strings.ToLower(strings.TrimSpace(prefer))
		if prefer != "target" && prefer != "source" {
			return fmt.Errorf("invalid --prefer value: %s (expected target or source)", prefer)
		}
		for _, conflict := range preview.Conflicts {
			if _, explicit := resolutions[conflict.Field]; !explicit {
				resolutions[conflict.Field] = prefer
			}
		}
	} else if !dryRun {
		if err := promptMergeResolutions(cmd, preview, resolutions); err != nil {
			return err
		}
	}

	preview, err = repo.PreviewMergeWithOptions(context.Background(), targetID, sourceID, database.ActressMergeOptions{Policy: policy, Resolutions: resolutions})
	if err != nil {
		return err
	}
	printMergeOutcome(cmd, preview)
	if dryRun {
		return nil
	}

	if !skipConfirm && !nonInteractive {
		if !promptConfirmation(cmd) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Merge cancelled")
			return nil
		}
	}

	result, err := repo.MergeWithOptions(context.Background(), targetID, sourceID, database.ActressMergeOptions{Policy: policy, Resolutions: resolutions})
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

	fields := preview.Conflicts
	if len(preview.Fields) > 0 {
		fields = nil
		for _, field := range preview.Fields {
			fields = append(fields, database.ActressMergeConflict{Field: field.Field, TargetValue: field.TargetValue, SourceValue: field.SourceValue, DefaultResolution: field.DefaultResolution})
		}
	}
	if len(fields) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No field conflicts detected; default merge behavior will be applied")
		return nil
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Reviewing %d scalar field(s)\n", len(fields))
	for _, conflict := range fields {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nField: %s\n", conflict.Field)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  target: %v\n", conflict.TargetValue)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  source: %v\n", conflict.SourceValue)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Choose value [t=target/s=source, Enter=keep current policy/decision]: ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		choice := strings.ToLower(strings.TrimSpace(line))
		switch choice {
		case "":
			// Omitted decisions remain policy-driven at execution time.
			if len(preview.Fields) == 0 {
				resolutions[conflict.Field] = "target"
			}
		case "t", "target":
			resolutions[conflict.Field] = "target"
		case "s", "source":
			resolutions[conflict.Field] = "source"
		default:
			return fmt.Errorf("invalid choice for field %s: %s", conflict.Field, choice)
		}
	}

	return nil
}

func printMergeOutcome(cmd *cobra.Command, preview *database.ActressMergePreview) {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Policy: %s; source #%d will be deleted, target #%d retained.\n", preview.Policy, preview.Source.ID, preview.Target.ID)
	for _, field := range preview.Fields {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s: target=%#v source=%#v result=%#v\n", field.Field, field.TargetValue, field.SourceValue, field.ResultValue)
	}
	if preview.Source.DMMID > 0 && preview.ProposedMerged.DMMID != preview.Source.DMMID {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Source DMM ID %d will not be retained.\n", preview.Source.DMMID)
	}
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

	repo := database.NewActressRepository(deps.DB)
	actresses, err := repo.List(context.Background(), 0, 0)
	if err != nil {
		return fmt.Errorf("failed to list actresses: %w", err)
	}

	data, err := json.MarshalIndent(actresses, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if len(args) == 0 {
		_, _ = cmd.OutOrStdout().Write(data)
		_, _ = cmd.OutOrStdout().Write([]byte("\n"))
		_, _ = fmt.Printf("Exported %d actress(es) to stdout\n", len(actresses))
	} else {
		if err := os.WriteFile(args[0], data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Printf("Exported %d actress(es) to %s\n", len(actresses), args[0])
	}

	return nil
}

func findActressImportMatch(ctx context.Context, repo *database.ActressRepository, incoming *models.Actress) (*models.Actress, error) {
	lookups := make([]func() (*models.Actress, error), 0, 4)
	if incoming.ID > 0 {
		lookups = append(lookups, func() (*models.Actress, error) { return repo.FindByID(ctx, incoming.ID) })
	}
	if incoming.DMMID > 0 {
		lookups = append(lookups, func() (*models.Actress, error) { return repo.FindByDMMID(ctx, incoming.DMMID) })
	}
	if strings.TrimSpace(incoming.JapaneseName) != "" {
		lookups = append(lookups, func() (*models.Actress, error) { return repo.FindByJapaneseName(ctx, incoming.JapaneseName) })
	}
	if incoming.FirstName != "" || incoming.LastName != "" {
		lookups = append(lookups, func() (*models.Actress, error) {
			return repo.FindByFirstNameLastName(ctx, incoming.FirstName, incoming.LastName)
		})
	}
	for _, lookup := range lookups {
		found, err := lookup()
		if err == nil {
			return found, nil
		}
		if !database.IsNotFound(err) {
			return nil, err
		}
	}
	return nil, nil
}

func sameImportedActress(existing, incoming *models.Actress) bool {
	return existing != nil && incoming != nil &&
		existing.DMMID == incoming.DMMID &&
		existing.FirstName == incoming.FirstName &&
		existing.LastName == incoming.LastName &&
		existing.JapaneseName == incoming.JapaneseName &&
		existing.ThumbURL == incoming.ThumbURL &&
		existing.Aliases == incoming.Aliases
}

func importActressRecords(ctx context.Context, repo *database.ActressRepository, actresses []models.Actress) (imported, skipped, errorsCount int) {
	for i := range actresses {
		a := &actresses[i]
		existing, err := findActressImportMatch(ctx, repo, a)
		if err != nil {
			errorsCount++
			continue
		}
		unchanged := sameImportedActress(existing, a)
		if err := repo.ImportUpsert(ctx, a); err != nil {
			errorsCount++
			continue
		}
		if unchanged && existing.Verified &&
			(existing.Origin == database.ActressOriginUser || existing.Origin == database.ActressOriginImport) {
			skipped++
		} else {
			imported++
		}
	}
	return imported, skipped, errorsCount
}

func runActressImport(cmd *cobra.Command, args []string, configFile string) error {
	fileData, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var actresses []models.Actress
	if err := json.Unmarshal(fileData, &actresses); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
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

	repo := database.NewActressRepository(deps.DB)
	imported, skipped, errorsCount := importActressRecords(context.Background(), repo, actresses)

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Imported: %d, Skipped: %d, Errors: %d\n", imported, skipped, errorsCount)

	return nil
}
