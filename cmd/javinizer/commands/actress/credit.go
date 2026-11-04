package actress

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
)

func newCatalogCommand(search bool) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List verified actresses and their database IDs", Args: cobra.NoArgs}
	if search {
		cmd.Use = "search <name>"
		cmd.Short = "Search verified actresses by name or DMM ID"
		cmd.Args = cobra.ExactArgs(1)
	}
	cmd.Flags().Int("limit", 50, "Maximum number of actresses to return (1-1000)")
	cmd.Flags().Int("offset", 0, "Number of matching actresses to skip")
	cmd.Flags().Bool("json", false, "Output actress records as JSON")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		if limit < 1 || limit > 1000 || offset < 0 {
			return fmt.Errorf("--limit must be between 1 and 1000 and --offset must be non-negative")
		}
		query := ""
		if search {
			query = strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("search name must not be empty")
			}
		}
		deps, err := creditDependencies(cmd)
		if err != nil {
			return err
		}
		defer func() { _ = deps.Close() }()
		return runActressCatalog(cmd, database.NewActressRepository(deps.DB), search, query, limit, offset)
	}
	return cmd
}

// runActressCatalog separates database/output work from command bootstrap.
func runActressCatalog(cmd *cobra.Command, repo *database.ActressRepository, search bool, query string, limit, offset int) error {
	var actresses []models.Actress
	var err error
	if search {
		actresses, err = repo.SearchPaged(cmd.Context(), query, limit, offset)
	} else {
		actresses, err = repo.List(cmd.Context(), limit, offset)
	}
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		// GORM Find initializes an empty slice for a successful empty result.
		return json.NewEncoder(cmd.OutOrStdout()).Encode(actresses)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	// Every row has four cells, with user text quoted to escape controls.
	// These writes only buffer cells; underlying output errors occur at Flush.
	_, _ = fmt.Fprintln(w, "ID\tNAME\tJAPANESE NAME\tDMM ID")
	for _, a := range actresses {
		_, _ = fmt.Fprintf(w, "%d\t%q\t%q\t%d\n", a.ID, a.FullName(), a.JapaneseName, a.DMMID)
	}
	return w.Flush()
}

func newCreditCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "credit", Short: "Manage user-owned movie credits"}
	add := &cobra.Command{
		Use: "add <movie-id>", Short: "Add a user-owned actress credit without replacing the cast",
		Long: "Add an actress credit in the database. Suppressed credits must be explicitly restored first. Accepts an unambiguous movie ID or content ID. Existing NFO files are not rewritten. Use actress list or actress search to find a verified actress database ID.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			actressID, _ := cmd.Flags().GetUint("actress-id")
			movieID := strings.TrimSpace(args[0])
			if actressID == 0 || movieID == "" {
				return fmt.Errorf("movie ID must not be empty and --actress-id must be greater than zero")
			}
			deps, err := creditDependencies(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = deps.Close() }()
			credit, err := database.NewMovieCreditRepository(deps.DB).AddUserCredit(cmd.Context(), movieID, actressID)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Saved user-owned credit: movie %s, actress ID %d (credit ID %d).\nDatabase updated only. Existing NFO files have not been rewritten.\n", credit.MovieContentID, credit.ActressID, credit.ID)
			return err
		},
	}
	add.Flags().Uint("actress-id", 0, "Verified actress database ID (from actress list/search, not DMM ID)")
	_ = add.MarkFlagRequired("actress-id")
	cmd.AddCommand(add)
	return cmd
}

func creditDependencies(cmd *cobra.Command) (*commandutil.CoreDeps, error) {
	configFile, _ := cmd.Flags().GetString("config")
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{Ctx: cmd.Context()})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	return deps, nil
}
