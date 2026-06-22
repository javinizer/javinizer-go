package token

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/token"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: "Manage API tokens",
		Long:  `Create, revoke, and list API tokens for programmatic access to the Javinizer API`,
	}

	tokenCmd.PersistentFlags().Bool("json", false, "Output in JSON format")

	tokenCmd.AddCommand(newCreateCommand())
	tokenCmd.AddCommand(newRevokeCommand())
	tokenCmd.AddCommand(newListCommand())

	return tokenCmd
}

func newCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API token",
		Long:  `Create a new API token and display the full token value. The token value will only be shown once.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runCreate(cmd, configFile)
		},
	}
	cmd.Flags().String("name", "", "Optional name for the token")
	return cmd
}

func newRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <id-or-prefix>",
		Short: "Revoke an API token",
		Long:  `Revoke an API token by its UUID or prefix (e.g., jv_abc12345). The token will be immediately invalidated.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runRevoke(cmd, configFile, args[0])
		},
	}
	return cmd
}

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all active API tokens",
		Long:  `List all active (non-revoked) API tokens with their names, creation dates, and last used timestamps.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runList(cmd, configFile)
		},
	}
	return cmd
}

func RunCreate(cmd *cobra.Command, args []string, configFile string) (*tokenCreateResult, error) {
	name, _ := cmd.Flags().GetString("name")

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)
	if _, err := config.Prepare(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewApiTokenRepository(deps.DB)
	svc := token.NewTokenService(repo)

	apiToken, fullToken, err := svc.Create(context.Background(), name)
	if err != nil {
		return nil, fmt.Errorf("failed to create token: %w", err)
	}

	return &tokenCreateResult{
		ID:          apiToken.ID,
		Name:        apiToken.Name,
		TokenPrefix: apiToken.TokenPrefix,
		Token:       fullToken,
		CreatedAt:   apiToken.CreatedAt,
	}, nil
}

func runCreate(cmd *cobra.Command, configFile string) error {
	result, err := RunCreate(cmd, nil, configFile)
	if err != nil {
		return err
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		return printJSON(cmd, result)
	}

	displayName := result.Name
	if displayName == "" {
		displayName = "(unnamed)"
	}

	w := cmd.OutOrStdout()
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Token created successfully!\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Name:    %s\n", displayName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  ID:      %s\n", shortID(result.ID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Token:   %s\n", result.Token); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "⚠️  This token value will NOT be shown again. Store it securely.\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	return nil
}

func RunRevoke(cmd *cobra.Command, args []string, configFile string) (*tokenRevokeResult, error) {
	idOrPrefix := args[0]

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)
	if _, err := config.Prepare(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewApiTokenRepository(deps.DB)
	svc := token.NewTokenService(repo)

	resolvedID := idOrPrefix
	resolvedPrefix := ""

	if strings.HasPrefix(idOrPrefix, "jv_") {
		prefix := strings.TrimPrefix(idOrPrefix, "jv_")
		if len(prefix) < 8 {
			return nil, fmt.Errorf("prefix too short: provide at least 8 characters after jv_")
		}
		prefix = prefix[:8]
		existing, err := repo.FindByPrefix(context.Background(), prefix)
		if err != nil {
			return nil, fmt.Errorf("no active token found with prefix jv_%s: %w", prefix, err)
		}
		resolvedID = existing.ID
		resolvedPrefix = existing.TokenPrefix
	}

	if err := svc.Revoke(context.Background(), resolvedID); err != nil {
		return nil, fmt.Errorf("failed to revoke token: %w", err)
	}

	if resolvedPrefix == "" {
		existing, err := repo.FindByID(context.Background(), resolvedID)
		if err == nil {
			resolvedPrefix = existing.TokenPrefix
		}
	}

	return &tokenRevokeResult{
		ID:      resolvedID,
		Prefix:  resolvedPrefix,
		Revoked: true,
	}, nil
}

func runRevoke(cmd *cobra.Command, configFile string, idOrPrefix string) error {
	result, err := RunRevoke(cmd, []string{idOrPrefix}, configFile)
	if err != nil {
		return err
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		return printJSON(cmd, result)
	}

	w := cmd.OutOrStdout()
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Token revoked successfully!\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  ID:      %s\n", shortID(result.ID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Prefix:  jv_%s\n", result.Prefix); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	return nil
}

func RunList(cmd *cobra.Command, args []string, configFile string) ([]tokenListEntry, error) {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)
	if _, err := config.Prepare(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	repo := database.NewApiTokenRepository(deps.DB)
	svc := token.NewTokenService(repo)

	tokens, err := svc.List(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}

	entries := make([]tokenListEntry, len(tokens))
	for i, t := range tokens {
		entries[i] = tokenListEntry{
			ID:          t.ID,
			Name:        t.Name,
			TokenPrefix: t.TokenPrefix,
			CreatedAt:   t.CreatedAt,
			LastUsedAt:  t.LastUsedAt,
		}
	}

	return entries, nil
}

func runList(cmd *cobra.Command, configFile string) error {
	result, err := RunList(cmd, nil, configFile)
	if err != nil {
		return err
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		return printJSON(cmd, result)
	}

	if len(result) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "No active tokens found.")
		return err
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tPREFIX\tNAME\tCREATED\tLAST USED"); err != nil {
		return err
	}

	for _, t := range result {
		name := t.Name
		if name == "" {
			name = "(unnamed)"
		}
		if len(name) > 30 {
			name = name[:27] + "..."
		}

		lastUsed := "never"
		if t.LastUsedAt != nil {
			lastUsed = t.LastUsedAt.Format("2006-01-02 15:04")
		}

		if _, err := fmt.Fprintf(w, "%s\tjv_%s\t%s\t%s\t%s\n",
			shortID(t.ID),
			t.TokenPrefix,
			name,
			t.CreatedAt.Format("2006-01-02 15:04"),
			lastUsed,
		); err != nil {
			return err
		}
	}

	return w.Flush()
}

func printJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

type tokenCreateResult struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	TokenPrefix string    `json:"token_prefix"`
	Token       string    `json:"token"`
	CreatedAt   time.Time `json:"created_at"`
}

type tokenRevokeResult struct {
	ID      string `json:"id"`
	Prefix  string `json:"prefix"`
	Revoked bool   `json:"revoked"`
}

type tokenListEntry struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}
