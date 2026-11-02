//go:build darwin || linux

package actress_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/actress"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestProvisionUnreadableRegularDumpDoesNotPublishAndCanRecover(t *testing.T) {
	const dump = "COPY public.derived_actress (id,name_romaji) FROM stdin;\npermission-contract\tPermission Contract\n\\.\n"
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0700))
	input := filepath.Join(t.TempDir(), "evidence.sql")
	require.NoError(t, os.WriteFile(input, []byte(dump), 0600))
	require.NoError(t, os.Chmod(input, 0000))
	t.Cleanup(func() { require.NoError(t, os.Chmod(input, 0600)) })
	// Privileged runners may bypass DAC; don't pretend they exercised EACCES.
	probe, err := os.Open(input)
	if err == nil {
		require.NoError(t, probe.Close())
		t.Skip("runner bypasses file read permissions")
	}
	require.ErrorIs(t, err, os.ErrPermission)
	before, err := os.Lstat(input)
	require.NoError(t, err)
	require.True(t, before.Mode().IsRegular())

	execute := func() (string, error) {
		t.Helper()
		var output bytes.Buffer
		root := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true,
			PersistentPreRun: func(*cobra.Command, []string) {
				t.Fatal("provision-cache must not run configuration/database bootstrap")
			},
		}
		root.AddCommand(actress.NewCommand())
		root.SetOut(&output)
		root.SetErr(io.Discard)
		root.SetArgs([]string{"actress", "provision-cache", "--dump", input, "--artifact-dir", directory,
			"--source-url", "https://fixtures.invalid/permission.sql", "--parser-revision", "permission-contract-v1", "--observed-at", "2024-02-02T19:05:06Z"})
		err := root.ExecuteContext(t.Context())
		return output.String(), err
	}
	output, err := execute()
	require.ErrorIs(t, err, os.ErrPermission)
	require.ErrorContains(t, err, "open uncompressed dump:")
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, input, pathErr.Path)
	require.Equal(t, "open", pathErr.Op)
	require.Empty(t, output, "rejected input must not emit a successful publication receipt")
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries, "no artifact, staging file, lock or manifest should be created before readable input admission")
	after, err := os.Lstat(input)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	require.Zero(t, after.Mode().Perm())
	require.Equal(t, before.Size(), after.Size())

	require.NoError(t, os.Chmod(input, 0600))
	output, err = execute()
	require.NoError(t, err, "restored input permissions must permit publication")
	var receipt struct {
		Published       bool   `json:"published"`
		FilesSynced     bool   `json:"files_synced"`
		DirectorySynced bool   `json:"directory_synced"`
		ArtifactSHA256  string `json:"artifact_sha256"`
		SourceSHA256    string `json:"source_sha256"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &receipt))
	require.True(t, receipt.Published)
	require.True(t, receipt.FilesSynced)
	require.True(t, receipt.DirectorySynced)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(dump))), receipt.SourceSHA256)
	store, err := artifactstore.Open(directory, offline.DefaultLimits().Store)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	view, err := store.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, receipt.ArtifactSHA256, view.Snapshot.Digest())
	records := view.Snapshot.Records()
	require.Len(t, records, 1)
	require.Equal(t, "permission-contract", records[0].SourceID)
	require.Equal(t, "r18dev", records[0].Source)
	source, err := os.ReadFile(input)
	require.NoError(t, err)
	require.Equal(t, dump, string(source))
}
