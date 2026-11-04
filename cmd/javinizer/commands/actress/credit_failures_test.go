package actress

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func executeCreditFailureCommand(t *testing.T, configPath string, output io.Writer, args ...string) error {
	t.Helper()
	root := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("config", configPath, "config file")
	root.AddCommand(NewCommand())
	root.SetOut(output)
	root.SetErr(io.Discard)
	root.SetArgs(append([]string{"actress"}, args...))
	return root.Execute()
}

func TestCreditCLIStartupFailures(t *testing.T) {
	for _, scenario := range []string{"invalid config", "database is directory"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.yaml")
			expected := "failed to load config"
			if scenario == "invalid config" {
				require.NoError(t, os.WriteFile(path, []byte("database: [broken"), 0600))
			} else {
				cfg := config.DefaultConfig(nil, nil)
				cfg.Database.DSN = filepath.Join(root, "database-directory")
				require.NoError(t, os.Mkdir(cfg.Database.DSN, 0700))
				require.NoError(t, config.Save(cfg, path))
				expected = "failed to initialize dependencies"
			}
			for _, args := range [][]string{{"list"}, {"search", "name"}, {"credit", "add", "movie", "--actress-id", "1"}} {
				var output bytes.Buffer
				err := executeCreditFailureCommand(t, path, &output, args...)
				require.ErrorContains(t, err, expected)
				require.Empty(t, output.String())
			}
		})
	}
}

type creditFailingOutput struct {
	err   error
	calls int
}

func (w *creditFailingOutput) Write([]byte) (int, error) { w.calls++; return 0, w.err }

func TestCreditCLIOutputFailures(t *testing.T) {
	path, dbPath := setupMissTestDB(t)
	actor := &models.Actress{FirstName: "Output", JapaneseName: "表示", Verified: true}
	seedMissActresses(t, path, actor)
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.Create(&models.Movie{ContentID: "output-movie", ID: "OUTPUT-1"}).Error)
	failure := errors.New("output destination closed")
	for _, args := range [][]string{{"list"}, {"search", "Output"}, {"list", "--json"}, {"search", "missing", "--json"}} {
		writer := &creditFailingOutput{err: failure}
		err := executeCreditFailureCommand(t, path, writer, args...)
		require.ErrorIs(t, err, failure)
		require.Positive(t, writer.calls)
	}
	// A reporting failure does not roll back an already committed database edit.
	// Retrying the operation remains idempotent rather than adding a second credit.
	for range 2 {
		writer := &creditFailingOutput{err: failure}
		err := executeCreditFailureCommand(t, path, writer, "credit", "add", "OUTPUT-1", "--actress-id", strconv.FormatUint(uint64(actor.ID), 10))
		require.ErrorIs(t, err, failure)
		credits, err := database.NewMovieCreditRepository(db).ListByMovie(t.Context(), "output-movie")
		require.NoError(t, err)
		require.Len(t, credits, 1)
		require.Equal(t, string(models.CreditOriginUser), credits[0].Origin)
	}
}

func TestCreditCatalogClosedDatabase(t *testing.T) {
	_, dbPath := setupMissTestDB(t)
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	require.NoError(t, db.Close())
	for _, search := range []bool{false, true} {
		cmd := newCatalogCommand(search)
		cmd.SetContext(t.Context())
		var output bytes.Buffer
		cmd.SetOut(&output)
		err := runActressCatalog(cmd, database.NewActressRepository(db), search, "name", 10, 0)
		require.ErrorContains(t, err, "database is closed")
		require.Empty(t, output.String())
	}
}

func TestCreditCatalogEscapesControlCharacters(t *testing.T) {
	path, _ := setupMissTestDB(t)
	actor := &models.Actress{FirstName: "First\nInjected\tCell", JapaneseName: "名前\fNew section", Verified: true}
	seedMissActresses(t, path, actor)
	var output bytes.Buffer
	require.NoError(t, executeCreditFailureCommand(t, path, &output, "list"))
	require.Contains(t, output.String(), `First\nInjected\tCell`)
	require.Contains(t, output.String(), `名前\fNew section`)
	require.NotContains(t, output.String(), "\f")
}

func TestCreditCatalogEmptyJSON(t *testing.T) {
	path, _ := setupMissTestDB(t)
	for _, args := range [][]string{{"list", "--json"}, {"search", "missing", "--json"}} {
		var output bytes.Buffer
		require.NoError(t, executeCreditFailureCommand(t, path, &output, args...))
		require.JSONEq(t, "[]", output.String())
	}
}
