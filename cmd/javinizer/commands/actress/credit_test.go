package actress_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/actress"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func executeCreditCLI(configPath string, args ...string) (string, error) {
	root := &cobra.Command{Use: "javinizer", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", configPath, "config file")
	root.AddCommand(actress.NewCommand())
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(append([]string{"actress"}, args...))
	err := root.Execute()
	return output.String(), err
}

func TestCreditCLIValidation(t *testing.T) {
	for _, args := range [][]string{
		{"credit", "add"}, {"credit", "add", "movie"}, {"credit", "add", "movie", "extra", "--actress-id", "1"},
		{"credit", "add", "movie", "--actress-id", "0"}, {"credit", "add", " ", "--actress-id", "1"},
		{"credit", "add", "movie", "--actress-id", "-1"}, {"list", "extra"}, {"search"}, {"search", " "},
		{"list", "--limit", "0"}, {"list", "--limit", "1001"}, {"search", "name", "--offset", "-1"},
	} {
		t.Run(strconv.Itoa(len(args))+args[len(args)-1], func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "must-not-exist.yaml")
			_, err := executeCreditCLI(configPath, args...)
			require.Error(t, err)
			_, err = os.Stat(configPath)
			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestCreditCLICatalog(t *testing.T) {
	configPath, _ := setupActressTestDB(t)
	alice := &models.Actress{FirstName: "Alice", JapaneseName: "アリス", Verified: true, Aliases: "Stage Alias"}
	bob := &models.Actress{FirstName: "Bob", JapaneseName: "ボブ", Verified: true}
	candidate := &models.Actress{FirstName: "Hidden", Verified: false, Origin: database.ActressOriginScrape}
	seedActresses(t, configPath, alice, bob, candidate)
	output, err := executeCreditCLI(configPath, "search", "Alice", "--json")
	require.NoError(t, err)
	var results []models.Actress
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	require.Len(t, results, 1)
	require.Equal(t, alice.ID, results[0].ID)
	output, err = executeCreditCLI(configPath, "list", "--json", "--limit", "1", "--offset", "1")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	require.Len(t, results, 1)
	require.Equal(t, bob.ID, results[0].ID)
	output, err = executeCreditCLI(configPath, "search", "not-present", "--json")
	require.NoError(t, err)
	require.JSONEq(t, "[]", output)
	output, err = executeCreditCLI(configPath, "list")
	require.NoError(t, err)
	require.Contains(t, output, "JAPANESE NAME")
	require.Contains(t, output, "Alice")
	require.NotContains(t, output, "Hidden")
}

func TestCreditCLIAddDatabaseOnly(t *testing.T) {
	configPath, dbPath := setupActressTestDB(t)
	identity := &models.Actress{FirstName: "Manual", Verified: true}
	seedActresses(t, configPath, identity)
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	movie := &models.Movie{ContentID: "cli-content", ID: "CLI-001", Title: "Existing"}
	require.NoError(t, db.Create(movie).Error)
	nfoPath := filepath.Join(filepath.Dir(configPath), "CLI-001.nfo")
	require.NoError(t, os.WriteFile(nfoPath, []byte("original NFO"), 0600))
	for range 2 {
		output, err := executeCreditCLI(configPath, "credit", "add", "CLI-001", "--actress-id", strconv.FormatUint(uint64(identity.ID), 10))
		require.NoError(t, err)
		require.Contains(t, output, "movie cli-content")
		require.Contains(t, output, "Existing NFO files have not been rewritten")
	}
	credits, err := database.NewMovieCreditRepository(db).ListByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, credits, 1)
	require.Equal(t, string(models.CreditOriginUser), credits[0].Origin)
	contents, err := os.ReadFile(nfoPath)
	require.NoError(t, err)
	require.Equal(t, "original NFO", string(contents))
	output, err := executeCreditCLI(configPath, "credit", "add", "missing", "--actress-id", strconv.FormatUint(uint64(identity.ID), 10))
	require.Error(t, err)
	require.NotContains(t, output, "Database updated")
}

func TestCreditCLISearchFullName(t *testing.T) {
	configPath, _ := setupActressTestDB(t)
	identity := &models.Actress{FirstName: "Yui", LastName: "Hatano", Verified: true}
	seedActresses(t, configPath, identity)
	for _, query := range []string{identity.FullName(), "Yui Hatano"} {
		output, err := executeCreditCLI(configPath, "search", query, "--json")
		require.NoError(t, err)
		var results []models.Actress
		require.NoError(t, json.Unmarshal([]byte(output), &results))
		require.Len(t, results, 1)
		require.Equal(t, identity.ID, results[0].ID)
	}
}
