package dump

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/spf13/cobra"
)

// NewCommand creates the `javinizer dump` command group for managing the local
// r18.dev database dump used to accelerate content_id resolution.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump",
		Short: "Manage the local r18.dev dump for offline content_id lookup",
		Long: `Download and query the r18.dev database dump.

The dump maps DMM content_ids to display dvd_ids. When present, the r18.dev
scraper consults it for exact content_id resolution instead of issuing
rate-limit-prone HTTP probes — a single local lookup replaces the slowest
step of a scrape.

Subcommands:
  download  Fetch the latest dump and build the local lookup database.
  update    Re-download only if a newer dump is available.
  status    Show row count, source date, and database size.
  search    Look up a dvd_id or content_id locally (for verification).`,
	}
	cmd.AddCommand(
		newDownloadCmd(),
		newUpdateCmd(),
		newStatusCmd(),
		newSearchCmd(),
	)
	return cmd
}

// resolveDumpPath returns the configured dump sidecar path, applying the
// default when the config leaves it empty.
func resolveDumpPath(cfg *config.Config) string {
	path := cfg.Metadata.R18DevDump.Path
	if path == "" {
		path = commandutil.DefaultR18DevDumpPath
	}
	return path
}

func newDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "download",
		Short: "Download the latest r18.dev dump and build the lookup database",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runDownload(cmd.Context(), cmd.OutOrStdout(), configFile, false)
		},
	}
}

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Re-download the dump only if a newer version is available",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runDownload(cmd.Context(), cmd.OutOrStdout(), configFile, true)
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show local dump statistics (rows, source date, size)",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runStatus(cmd.OutOrStdout(), configFile)
		},
	}
}

func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <id>",
		Short: "Look up a dvd_id or content_id in the local dump",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runSearch(cmd.OutOrStdout(), configFile, args[0])
		},
	}
}

func runDownload(ctx context.Context, w io.Writer, configFile string, updateOnly bool) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	path := resolveDumpPath(cfg)

	var currentSourceURL string
	if updateOnly {
		if store, err := r18devdump.Open(path); err == nil {
			stats, err := store.Stats(ctx)
			_ = store.Close()
			if err == nil {
				currentSourceURL = stats.SourceURL
			}
		}
	}

	client := &http.Client{Timeout: 0} // stream; rely on context cancellation

	fmt.Fprintf(w, "Fetching r18.dev dump from %s\n", r18devdump.DumpURLOverride())
	if updateOnly && currentSourceURL != "" {
		fmt.Fprintf(w, "Current version: %s\n", currentSourceURL)
	}

	var bar *progressBar
	progress := func(bytes, total int64) {
		if bar != nil {
			bar.update(bytes, total)
		}
	}

	res, err := r18devdump.Download(ctx, client, currentSourceURL, progress, func(r io.Reader, d r18devdump.DownloadResult) error {
		fmt.Fprintf(w, "Importing dump (source: %s, date: %s)...\n", d.FinalURL, d.SourceDate)
		bar = newProgressBar(w, 0)
		impRes, err := r18devdump.Import(ctx, r, path, r18devdump.ImportOptions{
			SourceURL:  d.FinalURL,
			SourceDate: d.SourceDate,
		})
		if err != nil {
			return err
		}
		bar.finish()
		fmt.Fprintf(w, "Imported %d videos into %s\n", impRes.Rows, impRes.Path)
		return nil
	})
	if err != nil {
		if bar != nil {
			bar.finish()
		}
		return fmt.Errorf("dump download failed: %w", err)
	}
	if res.Unchanged {
		fmt.Fprintf(w, "Dump unchanged (%s). No update needed.\n", res.SourceDate)
		return nil
	}
	fmt.Fprintf(w, "✅ Dump ready: %s\n", path)
	return nil
}

func runStatus(w io.Writer, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	path := resolveDumpPath(cfg)

	store, err := r18devdump.Open(path)
	if err != nil {
		fmt.Fprintf(w, "No local dump found at %s\n", path)
		fmt.Fprintln(w, "Run `javinizer dump download` to create it.")
		return nil
	}
	defer func() { _ = store.Close() }()

	stats, err := store.Stats(context.Background())
	if err != nil {
		return fmt.Errorf("failed to read dump stats: %w", err)
	}

	fmt.Fprintf(w, "=== r18.dev dump ===\n")
	fmt.Fprintf(w, "Path:        %s\n", stats.Path)
	fmt.Fprintf(w, "Rows:        %d\n", stats.RowCount)
	if stats.SourceDate != "" {
		fmt.Fprintf(w, "Source date: %s\n", stats.SourceDate)
	}
	if stats.SourceURL != "" {
		fmt.Fprintf(w, "Source URL:  %s\n", stats.SourceURL)
	}
	if stats.ImportedAt != "" {
		fmt.Fprintf(w, "Imported at: %s\n", stats.ImportedAt)
	}
	if size, err := fileSize(stats.Path); err == nil {
		fmt.Fprintf(w, "File size:   %.1f MB\n", float64(size)/1024/1024)
	}
	return nil
}

func runSearch(w io.Writer, configFile, id string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	path := resolveDumpPath(cfg)

	store, err := r18devdump.Open(path)
	if err != nil {
		return fmt.Errorf("no local dump at %s (run `javinizer dump download` first): %w", path, err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if cid, err := store.LookupByDVDID(ctx, id); err == nil {
		fmt.Fprintf(w, "%s -> content_id: %s\n", id, cid)
		return nil
	} else if !errors.Is(err, models.ErrDumpMiss) {
		return fmt.Errorf("dvd_id lookup failed for %s: %w", id, err)
	}
	if did, err := store.LookupByContentID(ctx, id); err == nil {
		fmt.Fprintf(w, "%s -> dvd_id: %s\n", id, did)
		return nil
	} else if !errors.Is(err, models.ErrDumpMiss) {
		return fmt.Errorf("content_id lookup failed for %s: %w", id, err)
	}
	logging.Debugf("dump search: %s not found", id)
	fmt.Fprintf(w, "No match for %s in the local dump.\n", id)
	return nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
