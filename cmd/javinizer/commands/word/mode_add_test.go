package word_test

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/word"
	"github.com/javinizer/javinizer-go/internal/database"
)

// #228: word add rejects an empty original (zero-width patterns would be
// pathological for wildcard mode).
func TestWordAddMode_EmptyOriginalRejected(t *testing.T) {
	configPath, _ := setupWordTestDB(t)
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", configPath, "config file")
	root.AddCommand(word.NewCommand())
	root.SetArgs([]string{"word", "add", "", "x", "--mode", "wildcard"})
	require.Error(t, root.Execute())
}

// #228: word add --mode validation and listing indicator.
func TestWordAddMode_WildcardPersistsAndLists(t *testing.T) {
	configPath, dbPath := setupWordTestDB(t)

	run := func(args ...string) error {
		root := &cobra.Command{Use: "root"}
		root.PersistentFlags().String("config", configPath, "config file")
		root.AddCommand(word.NewCommand())
		root.SetArgs(args)
		return root.Execute()
	}

	require.NoError(t, run("word", "add", "チ?ポ", "チンポ", "--mode", "wildcard"))
	require.Error(t, run("word", "add", "bad", "x", "--mode", "regex"))

	stdout, _ := captureOutput(t, func() {
		require.NoError(t, run("word", "list"))
	})
	assert.Contains(t, stdout, "チ?ポ")
	assert.Contains(t, stdout, "[wildcard]")

	// Mode is actually stored.
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	stored, err := database.NewWordReplacementRepository(db).FindByOriginal(context.Background(), "チ?ポ")
	require.NoError(t, err)
	assert.Equal(t, "wildcard", stored.MatchMode)
}
