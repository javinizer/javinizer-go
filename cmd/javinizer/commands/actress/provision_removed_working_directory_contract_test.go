//go:build linux

package actress_test

import (
	"bytes"
	"encoding/json"
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

func TestProvisionRemovedWorkingDirectoryRejectsRelativeButAllowsAbsoluteStore(t *testing.T) {
	original, err := os.Getwd()
	require.NoError(t, err)
	parent := t.TempDir()
	cwd := filepath.Join(parent, "empty-cwd")
	artifactDir := filepath.Join(parent, "artifacts")
	require.NoError(t, os.Mkdir(cwd, 0700))
	require.NoError(t, os.Mkdir(artifactDir, 0700))
	input := filepath.Join(parent, "evidence.sql")
	const dump = "COPY public.derived_actress (id,name_romaji) FROM stdin;\nremoved-cwd-contract\tRemoved Cwd Contract\n\\.\n"
	require.NoError(t, os.WriteFile(input, []byte(dump), 0600))
	execute := func(t *testing.T, store string) (string, error) {
		t.Helper()
		var output bytes.Buffer
		root := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true,
			PersistentPreRun: func(*cobra.Command, []string) {
				t.Fatal("provision-cache must not invoke configuration/database bootstrap")
			},
		}
		root.AddCommand(actress.NewCommand())
		root.SetOut(&output)
		root.SetErr(io.Discard)
		root.SetArgs([]string{"actress", "provision-cache", "--dump", input, "--artifact-dir", store,
			"--source-url", "https://fixtures.invalid/removed-cwd.sql", "--parser-revision", "removed-cwd-contract-v1", "--observed-at", "2024-02-02T19:05:06Z"})
		err := root.ExecuteContext(t.Context())
		return output.String(), err
	}
	t.Run("missing cwd", func(t *testing.T) {
		// Only this fresh empty child is removed; t.Chdir retains the original
		// directory handle and restores it during subtest cleanup.
		t.Chdir(cwd)
		require.NoError(t, os.Remove(cwd))
		_, err := os.Getwd()
		require.ErrorIs(t, err, os.ErrNotExist)
		output, err := execute(t, "artifacts")
		require.ErrorIs(t, err, os.ErrNotExist)
		var syscallErr *os.SyscallError
		require.ErrorAs(t, err, &syscallErr)
		require.Equal(t, "getwd", syscallErr.Syscall)
		require.Empty(t, output)
		entries, err := os.ReadDir(artifactDir)
		require.NoError(t, err)
		require.Empty(t, entries, "relative-path admission failure must not initialize or publish a store")
		source, err := os.ReadFile(input)
		require.NoError(t, err)
		require.Equal(t, dump, string(source))

		output, err = execute(t, artifactDir)
		require.NoError(t, err, "an explicit absolute store must not depend on the missing working directory")
		var report struct {
			Published      bool   `json:"published"`
			ArtifactSHA256 string `json:"artifact_sha256"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &report))
		require.True(t, report.Published)
		store, err := artifactstore.Open(artifactDir, offline.DefaultLimits().Store)
		require.NoError(t, err)
		defer func() { require.NoError(t, store.Close()) }()
		view, err := store.Load(t.Context())
		require.NoError(t, err)
		require.Equal(t, report.ArtifactSHA256, view.Snapshot.Digest())
		require.Len(t, view.Snapshot.Records(), 1)
		require.Equal(t, "removed-cwd-contract", view.Snapshot.Records()[0].SourceID)
	})
	restored, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, original, restored)
}
