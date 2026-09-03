package word_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/word"
)

// #228: word import must hard-abort (nothing stored) on invalid match_mode or
// empty originals.
func TestWordImport_HardAbortOnBadMode(t *testing.T) {
	configPath, _ := setupWordTestDB(t)
	payload := `[{"original":"A","replacement":"B","match_mode":"wildcard"},{"original":"C","replacement":"D","match_mode":"bogus"}]`
	file := filepath.Join(t.TempDir(), "in.json")
	require.NoError(t, os.WriteFile(file, []byte(payload), 0o644))

	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", configPath, "config file")
	root.AddCommand(word.NewCommand())
	root.SetArgs([]string{"word", "import", file})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid match_mode")

	// Nothing stored: entry A (replacement "B") must be absent too. Note the
	// trailing space pin so seeded rows like "-> Bled" never match.
	listRoot := &cobra.Command{Use: "root"}
	listRoot.PersistentFlags().String("config", configPath, "config file")
	listRoot.AddCommand(word.NewCommand())
	listRoot.SetArgs([]string{"word", "list"})
	stdout, _ := captureOutput(t, func() {
		require.NoError(t, listRoot.Execute())
	})
	assert.NotContains(t, stdout, "-> B ")
}

func TestWordImport_HardAbortOnEmptyOriginal(t *testing.T) {
	configPath, _ := setupWordTestDB(t)
	payload := `[{"original":"","replacement":"B","match_mode":"wildcard"}]`
	file := filepath.Join(t.TempDir(), "in.json")
	require.NoError(t, os.WriteFile(file, []byte(payload), 0o644))

	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", configPath, "config file")
	root.AddCommand(word.NewCommand())
	root.SetArgs([]string{"word", "import", file})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty original")
}
