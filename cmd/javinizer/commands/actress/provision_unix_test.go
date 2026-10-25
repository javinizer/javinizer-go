//go:build darwin || linux

package actress

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

const provisionFixture = "COPY public.derived_actress (id,name_romaji) FROM stdin;\nsynthetic-001\tSynthetic Name\n\\.\n"

func provisionFixtureOptions(t *testing.T) provisionOptions {
	t.Helper()
	options := validProvisionOptions()
	options.artifactDir = t.TempDir()
	options.dumpPath = filepath.Join(t.TempDir(), "synthetic.sql")
	if err := os.WriteFile(options.dumpPath, []byte(provisionFixture), 0600); err != nil {
		t.Fatal(err)
	}
	return options
}

func TestProvisionCommandConvertsAndPublishesWithoutBootstrap(t *testing.T) {
	options := provisionFixtureOptions(t)
	var output bytes.Buffer
	root := &cobra.Command{Use: "test", PersistentPreRun: func(*cobra.Command, []string) {
		t.Fatal("offline CLI invoked config/database/desktop bootstrap")
	}}
	root.AddCommand(NewCommand())
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"actress", "provision-cache", "--dump", options.dumpPath, "--artifact-dir", options.artifactDir, "--source-url", options.sourceURL, "--parser-revision", options.parserRevision, "--observed-at", options.observedAt})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	store, err := artifactstore.Open(options.artifactDir, offline.DefaultLimits().Store)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	view, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	records := view.Snapshot.Records()
	if len(records) != 1 || records[0].Source != "r18dev" || records[0].SourceID != "synthetic-001" || records[0].Metadata.Aliases[0] != "Synthetic Name" || records[0].Provenance.ObservedAt.Format("2006-01-02T15:04:05Z07:00") != "2024-02-02T19:05:06Z" {
		t.Fatalf("incorrect converted evidence: %+v", records)
	}
	if view.Manifest.Source.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(provisionFixture))) || view.Manifest.Source.Bytes != int64(len(provisionFixture)) {
		t.Fatalf("source receipt does not bind exact supplied bytes: %+v", view.Manifest)
	}
	var report map[string]any
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report["published"] != true || report["directory_synced"] != true || report["artifact_sha256"] != view.Snapshot.Digest() {
		t.Fatalf("publication report mismatch: %s %v", output.String(), err)
	}
	entries, err := os.ReadDir(filepath.Dir(options.dumpPath))
	if err != nil || len(entries) != 1 || entries[0].Name() != "synthetic.sql" {
		t.Fatalf("offline provisioning changed source directory: %v %v", entries, err)
	}
}

func TestProvisionRejectsInvalidInputWithoutPublishing(t *testing.T) {
	for _, kind := range []string{"missing-dir", "writable-dir", "missing-dump", "symlink-dump", "directory-dump", "fifo-dump", "compressed", "malformed", "oversized", "secret-url", "empty"} {
		t.Run(kind, func(t *testing.T) {
			options := provisionFixtureOptions(t)
			switch kind {
			case "missing-dir":
				options.artifactDir = filepath.Join(options.artifactDir, "not-created")
			case "writable-dir":
				if err := os.Chmod(options.artifactDir, 0777); err != nil {
					t.Fatal(err)
				}
			case "missing-dump":
				options.dumpPath += ".missing"
			case "symlink-dump":
				if err := os.Symlink(options.dumpPath, options.dumpPath+".link"); err != nil {
					t.Fatal(err)
				}
				options.dumpPath += ".link"
			case "directory-dump":
				options.dumpPath = filepath.Dir(options.dumpPath)
			case "fifo-dump":
				options.dumpPath += ".fifo"
				if err := unix.Mkfifo(options.dumpPath, 0600); err != nil {
					t.Fatal(err)
				}
			case "compressed":
				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				_, _ = writer.Write([]byte(provisionFixture))
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(options.dumpPath, compressed.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(options.dumpPath, []byte("COPY public.derived_actress (id,name_romaji) FROM stdin;\nbroken-row\n\\.\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.Truncate(options.dumpPath, offline.DefaultLimits().Source.MaxInputBytes+1); err != nil {
					t.Fatal(err)
				}
			case "secret-url":
				options.sourceURL += "?token=do-not-persist"
			case "empty":
				if err := os.WriteFile(options.dumpPath, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			if err := runProvisionCache(t.Context(), &output, options); err == nil {
				t.Fatal("invalid offline input accepted")
			}
			if output.Len() != 0 {
				t.Fatalf("invalid input produced publication report: %s", output.String())
			}
			if _, err := os.Stat(filepath.Join(options.artifactDir, "current.json")); !os.IsNotExist(err) {
				t.Fatalf("invalid source changed current: %v", err)
			}
			if _, err := os.Stat(filepath.Join(options.artifactDir, ".writer.lock")); !os.IsNotExist(err) {
				t.Fatalf("invalid source acquired writer: %v", err)
			}
			if kind == "missing-dir" {
				if _, err := os.Stat(options.artifactDir); !os.IsNotExist(err) {
					t.Fatal("CLI created implicit artifact directory")
				}
			}
			if kind == "writable-dir" {
				info, err := os.Stat(options.artifactDir)
				if err != nil || info.Mode().Perm() != 0777 {
					t.Fatalf("CLI changed directory permissions: %v %v", info, err)
				}
			}
		})
	}
}

func TestProvisionRefusesForeignStoreEntryWithoutRetry(t *testing.T) {
	options := provisionFixtureOptions(t)
	if err := os.WriteFile(filepath.Join(options.artifactDir, "foreign.txt"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := runProvisionCache(t.Context(), &output, options)
	if err == nil || !strings.Contains(err.Error(), "did not replace current") {
		t.Fatalf("unsafe store admitted: %v", err)
	}
	var report map[string]any
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report["published"] != false || report["commit_attempted"] != false {
		t.Fatalf("incorrect precommit refusal report: %s %v", output.String(), err)
	}
	if data, err := os.ReadFile(filepath.Join(options.artifactDir, "foreign.txt")); err != nil || string(data) != "retained" {
		t.Fatal("foreign file pruned")
	}
}
