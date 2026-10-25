package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/spf13/cobra"
)

func validProvisionOptions() provisionOptions {
	return provisionOptions{
		dumpPath: "dump.sql", artifactDir: "private-store",
		sourceURL: "https://r18.dev/dump", parserRevision: "offline-copy-v1",
		observedAt: "2024-02-03T04:05:06+09:00",
	}
}

func TestProvisionProvenanceRequiresExplicitSafeAssertions(t *testing.T) {
	options := validProvisionOptions()
	provenance, err := options.provenance()
	if err != nil || provenance.DatasetSHA256 != "" || provenance.ObservedAt.Format(time.RFC3339) != "2024-02-02T19:05:06Z" {
		t.Fatalf("source freshness/provenance changed: %+v %v", provenance, err)
	}
	for _, change := range []func(*provisionOptions){
		func(o *provisionOptions) { o.dumpPath = "" },
		func(o *provisionOptions) { o.artifactDir = "" },
		func(o *provisionOptions) { o.sourceURL = "" },
		func(o *provisionOptions) { o.sourceURL = "https://user:secret@r18.dev/dump" },
		func(o *provisionOptions) { o.sourceURL = "https://r18.dev/dump?token=secret" },
		func(o *provisionOptions) { o.sourceURL = "https://r18.dev/dump?" },
		func(o *provisionOptions) { o.sourceURL = "https://r18.dev/dump#secret" },
		func(o *provisionOptions) { o.sourceURL = "https://r18.dev/dump#" },
		func(o *provisionOptions) { o.sourceURL = "ftp://r18.dev/dump" },
		func(o *provisionOptions) { o.sourceURL = "https:///dump" },
		func(o *provisionOptions) { o.sourceURL = "https://r18.dev/%x" },
		func(o *provisionOptions) { o.sourceURL = " https://r18.dev/dump" },
		func(o *provisionOptions) { o.parserRevision = " " },
		func(o *provisionOptions) { o.observedAt = "" },
		func(o *provisionOptions) { o.observedAt = "today" },
		func(o *provisionOptions) { o.observedAt = "0001-01-01T00:00:00Z" },
	} {
		copy := options
		change(&copy)
		if _, err := copy.provenance(); err == nil || strings.Contains(err.Error(), "token=secret") || strings.Contains(err.Error(), "user:secret") {
			t.Fatalf("unsafe/missing provenance accepted or secret reflected: %+v %v", copy, err)
		}
	}
}

func TestProvisionCommandRegistrationAndRequiredFlags(t *testing.T) {
	cmd := NewCommand()
	provision, _, err := cmd.Find([]string{"provision-cache"})
	if err != nil || provision == cmd || provision.Name() != "provision-cache" {
		t.Fatalf("provision command not registered: %v", err)
	}
	for _, flag := range []string{"dump", "artifact-dir", "source-url", "parser-revision", "observed-at"} {
		if provision.Flags().Lookup(flag) == nil {
			t.Fatalf("missing explicit flag %s", flag)
		}
	}
	if err := provision.Args(provision, []string{"implicit-file.sql"}); err == nil {
		t.Fatal("positional/implicit input accepted")
	}
	root := &cobra.Command{Use: "test", PersistentPreRun: func(*cobra.Command, []string) {
		t.Fatal("offline provisioning inherited config/bootstrap side effects")
	}}
	root.AddCommand(cmd)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"actress", "provision-cache"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "required flag") {
		t.Fatalf("missing arguments not refused: %v", err)
	}
}

func TestProvisionCanceledBeforeReading(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runProvisionCache(ctx, io.Discard, validProvisionOptions()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := runProvisionCache(t.Context(), io.Discard, provisionOptions{}); err == nil {
		t.Fatal("missing provenance accepted")
	}
}

func TestProvisionResultReportsVisibilityAndUncertainty(t *testing.T) {
	sentinel := errors.New("synthetic publication error")
	for _, tc := range []struct {
		name      string
		result    artifactstore.Result
		err       error
		uncertain bool
		message   string
	}{
		{"success", artifactstore.Result{Published: true, CommitAttempted: true, FilesSynced: true, DirectorySynced: true}, nil, false, ""},
		{"precommit", artifactstore.Result{}, sentinel, false, "did not replace current"},
		{"uncertain", artifactstore.Result{CommitAttempted: true, FilesSynced: true}, errors.Join(artifactstore.ErrCommitUncertain, sentinel), true, "outcome is uncertain"},
		{"attempted-without-sentinel", artifactstore.Result{CommitAttempted: true}, sentinel, true, "outcome is uncertain"},
		{"explicit-uncertain", artifactstore.Result{}, artifactstore.ErrCommitUncertain, true, "outcome is uncertain"},
		{"visible-sync-failure", artifactstore.Result{Published: true, CommitAttempted: true, FilesSynced: true}, sentinel, false, "publication is visible"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			observed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
			err := reportProvisionResult(&output, strings.Repeat("a", 64), r18dump.Receipt{SHA256: strings.Repeat("b", 64), Bytes: 100, ActressRows: 2}, observed, tc.result, tc.err)
			if tc.err == nil && err != nil || tc.err != nil && (!errors.Is(err, tc.err) || !strings.Contains(err.Error(), tc.message)) {
				t.Fatalf("publication error changed: %v", err)
			}
			var report map[string]any
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report["published"] != tc.result.Published || report["commit_attempted"] != tc.result.CommitAttempted || report["files_synced"] != tc.result.FilesSynced || report["directory_synced"] != tc.result.DirectorySynced || report["outcome_uncertain"] != tc.uncertain || report["observed_at"] != "2020-01-02T03:04:05Z" {
				t.Fatalf("misleading outcome: %s", output.String())
			}
		})
	}
}

type failedProvisionOutput struct{ err error }

func (w failedProvisionOutput) Write([]byte) (int, error) { return 0, w.err }

func TestProvisionOutputFailureRetainsCommittedOutcome(t *testing.T) {
	outputErr := errors.New("output unavailable")
	publishErr := errors.New("final sync failed")
	err := reportProvisionResult(failedProvisionOutput{outputErr}, "digest", r18dump.Receipt{}, time.Time{}, artifactstore.Result{Published: true, CommitAttempted: true, FilesSynced: true}, publishErr)
	if !errors.Is(err, outputErr) || !errors.Is(err, publishErr) || !strings.Contains(err.Error(), "published=true, commit_attempted=true") || !strings.Contains(err.Error(), "do not blindly retry") {
		t.Fatalf("lost committed outcome on output error: %v", err)
	}
}
