package sort_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/sort"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortCommandRejectsInvalidLinkOptionsBeforeBootstrap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		flags     []string
		wantError string
	}{
		{"invalid mode", []string{"--link-mode=invalid"}, "invalid link mode"},
		{"move and hard link", []string{"--move", "--link-mode=hard"}, "--link-mode can only be used when --move is disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := t.TempDir()
			video := testutil.CreateTestVideoFile(t, source, "FAIL-001.mp4")
			stateDir := t.TempDir()
			configPath := filepath.Join(stateDir, "config.yaml")
			dbPath := filepath.Join(stateDir, "javinizer.db")
			destination := filepath.Join(t.TempDir(), "sorted")
			t.Setenv("JAVINIZER_HOME", t.TempDir())
			t.Setenv("JAVINIZER_TEMP_DIR", t.TempDir())
			t.Setenv("JAVINIZER_DB", dbPath)
			t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")

			// Use the installed RunE with the normal parent config flag, not
			// sort.Run directly. Rejection must precede LoadOrCreate/bootstrap
			// and scanning, despite the nonempty synthetic video fixture.
			var output bytes.Buffer
			root := &cobra.Command{Use: "javinizer", SilenceUsage: true, SilenceErrors: true}
			root.PersistentFlags().String("config", "", "configuration file")
			root.AddCommand(sort.NewCommand())
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(append([]string{"sort", source, "--config", configPath, "--dest", destination}, tc.flags...))

			err := root.Execute()
			require.ErrorContains(t, err, tc.wantError)
			assert.Empty(t, output.String(), "rejection must precede scan/processing output")
			entries, err := os.ReadDir(stateDir)
			require.NoError(t, err)
			assert.Empty(t, entries, "invalid flags must not create a config, database, or sidecars")
			_, err = os.Stat(destination)
			assert.ErrorIs(t, err, os.ErrNotExist, "invalid options must not create a destination")
			data, err := os.ReadFile(video)
			require.NoError(t, err)
			assert.Equal(t, "dummy video content", string(data))
			entries, err = os.ReadDir(source)
			require.NoError(t, err)
			require.Len(t, entries, 1, "no NFO, artwork, or organized subdirectory may be created")
			assert.Equal(t, filepath.Base(video), entries[0].Name())
		})
	}
}
