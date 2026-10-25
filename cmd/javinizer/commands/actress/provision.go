package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/spf13/cobra"
)

type provisionOptions struct {
	dumpPath       string
	artifactDir    string
	sourceURL      string
	parserRevision string
	observedAt     string
}

func newProvisionCommand() *cobra.Command {
	var options provisionOptions
	cmd := &cobra.Command{
		Use:   "provision-cache",
		Short: "Publish bounded offline r18.dev evidence from an explicitly supplied dump",
		Long: "Convert an uncompressed regular dump file to immutable evidence in an existing private directory. " +
			"No network, SQL execution, database/config changes or pruning. Source provenance is caller asserted, not authenticated. " +
			"Publication is supported only on Linux and macOS; Windows readers can use an already provisioned store.",
		Args: cobra.NoArgs,
		// Do not inherit the root's config, logging or desktop bootstrap. This
		// operation owns only the explicitly chosen artifact directory.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return runProvisionCache(cmd.Context(), cmd.OutOrStdout(), options)
		},
	}
	cmd.Flags().StringVar(&options.dumpPath, "dump", "", "Explicit regular, uncompressed PostgreSQL COPY dump file")
	cmd.Flags().StringVar(&options.artifactDir, "artifact-dir", "", "Explicit existing private artifact directory (never created or chmodded)")
	cmd.Flags().StringVar(&options.sourceURL, "source-url", "", "Asserted HTTP(S) source URL, without user info, query or fragment")
	cmd.Flags().StringVar(&options.parserRevision, "parser-revision", "", "Explicit source parser revision assertion")
	cmd.Flags().StringVar(&options.observedAt, "observed-at", "", "Explicit source observation time in RFC3339, never inferred from the current time")
	for _, flag := range []string{"dump", "artifact-dir", "source-url", "parser-revision", "observed-at"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	return cmd
}

func (o provisionOptions) provenance() (actresscache.Provenance, error) {
	if o.dumpPath == "" || o.artifactDir == "" {
		return actresscache.Provenance{}, fmt.Errorf("dump and artifact-dir must be explicitly supplied")
	}
	u, err := url.Parse(o.sourceURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(o.sourceURL, "#") || strings.TrimSpace(o.sourceURL) != o.sourceURL {
		return actresscache.Provenance{}, fmt.Errorf("source-url must be HTTP(S) without user info, query or fragment; remove secrets before provisioning")
	}
	if strings.TrimSpace(o.parserRevision) == "" {
		return actresscache.Provenance{}, fmt.Errorf("parser-revision must be explicitly supplied")
	}
	observed, err := time.Parse(time.RFC3339Nano, o.observedAt)
	if err != nil || observed.IsZero() {
		return actresscache.Provenance{}, fmt.Errorf("observed-at must be an explicit nonzero RFC3339 timestamp")
	}
	return actresscache.Provenance{SourceURL: o.sourceURL, ParserRevision: o.parserRevision, ObservedAt: observed.UTC()}, nil
}

func runProvisionCache(ctx context.Context, output io.Writer, options provisionOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	provenance, err := options.provenance()
	if err != nil {
		return err
	}
	if err := provisionPlatformSupported(); err != nil {
		return err
	}
	directory, err := filepath.Abs(options.artifactDir)
	if err != nil {
		return err
	}
	limits := offline.DefaultLimits()
	store, err := artifactstore.Open(directory, limits.Store)
	if err != nil {
		return fmt.Errorf("open artifact directory: %w", err)
	}
	defer func() { _ = store.Close() }()
	input, err := openProvisionInput(options.dumpPath, limits.Source.MaxInputBytes)
	if err != nil {
		return fmt.Errorf("open uncompressed dump: %w", err)
	}
	defer func() { _ = input.Close() }()
	snapshot, receipt, err := r18dump.Convert(ctx, input, provenance, limits.Source)
	if err != nil {
		return fmt.Errorf("convert uncompressed dump: %w", err)
	}
	result, publishErr := store.Publish(ctx, bytes.NewReader(snapshot.Bytes()), receipt)
	return reportProvisionResult(output, snapshot.Digest(), receipt, provenance.ObservedAt, result, publishErr)
}

// Every Publish outcome is emitted before returning its error. In particular,
// Published=true plus an error is visible publication with failed durability;
// CommitAttempted=true/Published=false is not proof of nonexecution. No retry.
func reportProvisionResult(output io.Writer, digest string, receipt r18dump.Receipt, observed time.Time, result artifactstore.Result, publishErr error) error {
	report := struct {
		ArtifactSHA256  string    `json:"artifact_sha256"`
		SourceSHA256    string    `json:"source_sha256"`
		SourceBytes     int64     `json:"source_bytes"`
		ActressRows     int       `json:"actress_rows"`
		ObservedAt      time.Time `json:"observed_at"`
		Published       bool      `json:"published"`
		CommitAttempted bool      `json:"commit_attempted"`
		FilesSynced     bool      `json:"files_synced"`
		DirectorySynced bool      `json:"directory_synced"`
		Uncertain       bool      `json:"outcome_uncertain"`
	}{digest, receipt.SHA256, receipt.Bytes, receipt.ActressRows, observed,
		result.Published, result.CommitAttempted, result.FilesSynced, result.DirectorySynced,
		!result.Published && (result.CommitAttempted || errors.Is(publishErr, artifactstore.ErrCommitUncertain))}
	writeErr := json.NewEncoder(output).Encode(report)
	if publishErr != nil {
		switch {
		case result.Published:
			publishErr = fmt.Errorf("publication is visible but durability was not confirmed; do not blindly retry: %w", publishErr)
		case report.Uncertain:
			publishErr = fmt.Errorf("publication outcome is uncertain; inspect validated current/previous before another action; do not blindly retry: %w", publishErr)
		default:
			publishErr = fmt.Errorf("publication did not replace current: %w", publishErr)
		}
	}
	if writeErr != nil {
		writeErr = fmt.Errorf("publication report failed (published=%t, commit_attempted=%t, files_synced=%t, directory_synced=%t); do not blindly retry: %w", result.Published, result.CommitAttempted, result.FilesSynced, result.DirectorySynced, writeErr)
	}
	return errors.Join(publishErr, writeErr)
}
