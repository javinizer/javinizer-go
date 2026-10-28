package word_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/word"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// The commands use fmt.Printf as well as Cobra's writer. These nonparallel tests
// capture both, using a regular file so large lists cannot fill a pipe buffer.
func executePersistedCatalogCLI(t *testing.T, configPath string, args ...string) (string, string, error) {
	t.Helper()
	root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "config file")
	root.AddCommand(word.NewCommand())
	var output, diagnostics bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&diagnostics)
	root.SetArgs(append([]string{"--config", configPath, "word"}, args...))
	capturePath := filepath.Join(t.TempDir(), "console.txt")
	capture, err := os.Create(capturePath)
	require.NoError(t, err)
	var commandErr, closeErr error
	func() {
		oldOut, oldErr := os.Stdout, os.Stderr
		os.Stdout, os.Stderr = capture, capture
		defer func() { os.Stdout, os.Stderr = oldOut, oldErr; closeErr = capture.Close() }()
		commandErr = root.Execute()
	}()
	require.NoError(t, closeErr)
	console, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	return string(console) + diagnostics.String(), output.String(), commandErr
}

type persistedCatalogRow struct {
	ID                                      uint
	Original, Replacement, MatchMode        string
	CreatedType, CreatedValue, UpdatedValue string
}

func snapshotPersistedCatalog(t *testing.T, db *database.DB) ([]persistedCatalogRow, int64) {
	t.Helper()
	var rows []persistedCatalogRow
	// Do not scan the damaged timestamp as time.Time. Inspect the native SQLite
	// storage value while preserving the custom mappings and their identities.
	require.NoError(t, db.Raw(`SELECT id, original, replacement, match_mode,
 typeof(created_at) AS created_type, quote(created_at) AS created_value,
 quote(updated_at) AS updated_value FROM word_replacements
 WHERE original LIKE 'catalog-fixture-%' ORDER BY id`).Scan(&rows).Error)
	var count int64
	require.NoError(t, db.Table("word_replacements").Count(&count).Error)
	return rows, count
}

func TestWordCLIContainsDamagedPersistedCatalog(t *testing.T) {
	for _, tc := range []struct {
		name, operation           string
		damaged                   any
		storageType, storageValue string
		jsonFailure               bool
	}{
		{name: "list_unreadable_timestamp", operation: "list", damaged: []byte{0xff}, storageType: "blob", storageValue: "X'FF'"},
		{name: "export_unreadable_timestamp", operation: "export", damaged: []byte{0xff}, storageType: "blob", storageValue: "X'FF'"},
		{name: "export_non_json_timestamp", operation: "export", damaged: int64(253402300800), storageType: "integer", storageValue: "253402300800", jsonFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "JAVINIZER_HOME"} {
				t.Setenv(key, filepath.Join(dir, key))
			}
			for _, key := range []string{"LOG_LEVEL", "UMASK", "JAVINIZER_TEMP_DIR", "JAVINIZER_DB", "JAVINIZER_LOG_DIR", "JAVINIZER_R18DEV_DUMP_PATH", "JAVINIZER_E2E_SCRAPERS"} {
				t.Setenv(key, "")
			}
			cfg := config.DefaultConfig(nil, nil)
			cfg.Database.DSN = filepath.Join(dir, "catalog.sqlite")
			cfg.System.TempDir = filepath.Join(dir, "temp")
			cfg.System.VersionCheckEnabled = false
			cfg.Logging.Output = "stdout"
			configPath := filepath.Join(dir, "config.yaml")
			require.NoError(t, config.Save(cfg, configPath))
			cfg, err := config.LoadOrCreate(configPath)
			require.NoError(t, err)
			configBefore, err := os.ReadFile(configPath)
			require.NoError(t, err)

			// Normal dependency startup supplies the real migrations and default seeds.
			// Custom originals avoid defaults that startup legitimately upserts again.
			deps, err := commandutil.NewDependencies(cfg)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, deps.Close()) })
			repo := database.NewWordReplacementRepository(deps.DB)
			timestamp := time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC)
			healthy := models.WordReplacement{Original: "catalog-fixture-healthy", Replacement: "healthy mapping", CreatedAt: timestamp, UpdatedAt: timestamp, MatchMode: models.MatchModeLiteral}
			damaged := models.WordReplacement{Original: "catalog-fixture-damaged", Replacement: "preserved mapping", CreatedAt: timestamp, UpdatedAt: timestamp, MatchMode: models.MatchModeWildcard}
			require.False(t, database.IsDefaultWordReplacement(healthy.Original))
			require.False(t, database.IsDefaultWordReplacement(damaged.Original))
			require.NoError(t, repo.Create(t.Context(), &healthy))
			require.NoError(t, repo.Create(t.Context(), &damaged))
			require.NotZero(t, healthy.ID)
			require.NotZero(t, damaged.ID)
			require.NotEqual(t, healthy.ID, damaged.ID)

			// These are damaged/externally edited catalogs, not values produced by app
			// writes. SQLite's existing non-STRICT DATETIME column admits a BLOB or Unix
			// seconds for year 10000; the real scanner / JSON encoder must reject them.
			result := deps.DB.Exec("UPDATE word_replacements SET created_at = ? WHERE id = ?", tc.damaged, damaged.ID)
			require.NoError(t, result.Error)
			require.EqualValues(t, 1, result.RowsAffected)
			before, countBefore := snapshotPersistedCatalog(t, deps.DB)
			require.Len(t, before, 2)
			require.Equal(t, damaged.ID, before[1].ID)
			require.Equal(t, tc.storageType, before[1].CreatedType)
			require.Equal(t, tc.storageValue, before[1].CreatedValue)
			if tc.jsonFailure {
				readable, err := repo.FindByID(t.Context(), damaged.ID)
				require.NoError(t, err, "the out-of-range year must be readable before JSON serialization")
				require.Equal(t, 10000, readable.CreatedAt.Year())
			}

			exportPath := filepath.Join(dir, "backup.json")
			sentinel := []byte("existing backup must survive a failed export\n")
			require.NoError(t, os.WriteFile(exportPath, sentinel, 0o600))
			attempts := [][]string{{tc.operation}}
			if tc.operation == "export" {
				// Exercise both an existing backup destination and the stdout form.
				attempts = [][]string{{"export", exportPath}, {"export"}}
			}
			for _, args := range attempts {
				console, output, err := executePersistedCatalogCLI(t, configPath, args...)
				if tc.jsonFailure {
					require.ErrorContains(t, err, "failed to marshal JSON:")
					var marshalErr *json.MarshalerError
					require.ErrorAs(t, err, &marshalErr)
					require.Equal(t, reflect.TypeOf(&time.Time{}), marshalErr.Type)
					require.ErrorContains(t, marshalErr.Err, "year outside of range")
				} else {
					require.ErrorContains(t, err, "failed to list word replacements:")
					require.ErrorContains(t, err, "Scan error")
					require.ErrorContains(t, err, "created_at")
				}
				require.NotContains(t, err.Error(), "failed to initialize dependencies")
				require.Empty(t, output, "a failed export must not emit partial JSON")
				for _, partial := range []string{`"original":`, `"replacement":`, healthy.Original, damaged.Original} {
					require.NotContains(t, console, partial, "failure must not expose a partial list or JSON through process output")
				}
				for _, success := range []string{"Exported ", "=== Word Replacements ===", "Total:", "No word replacements configured"} {
					require.NotContains(t, console, success)
				}
				got, err := os.ReadFile(exportPath)
				require.NoError(t, err)
				require.Equal(t, sentinel, got)
				after, countAfter := snapshotPersistedCatalog(t, deps.DB)
				require.Equal(t, before, after, "failures must not rewrite either custom row or its damaged storage value")
				require.Equal(t, countBefore, countAfter, "commands must not create extra catalog entries")
				got, err = os.ReadFile(configPath)
				require.NoError(t, err)
				require.Equal(t, configBefore, got)
			}

			// Repair only the timestamp, then retry through fresh public command trees.
			result = deps.DB.Exec("UPDATE word_replacements SET created_at = ? WHERE id = ?", timestamp, damaged.ID)
			require.NoError(t, result.Error)
			require.EqualValues(t, 1, result.RowsAffected)
			console, output, err := executePersistedCatalogCLI(t, configPath, "list")
			require.NoError(t, err)
			require.Empty(t, output)
			require.Contains(t, console, "=== Word Replacements ===")
			for _, mapping := range []models.WordReplacement{healthy, damaged} {
				require.Contains(t, console, mapping.Original)
				require.Contains(t, console, mapping.Replacement)
			}
			console, output, err = executePersistedCatalogCLI(t, configPath, "export", exportPath)
			require.NoError(t, err)
			require.Empty(t, output)
			require.Contains(t, console, "Exported ")
			data, err := os.ReadFile(exportPath)
			require.NoError(t, err)
			var exported []models.WordReplacement
			require.NoError(t, json.Unmarshal(data, &exported))
			require.Len(t, exported, int(countBefore))
			var custom []models.WordReplacement
			for _, mapping := range exported {
				if strings.HasPrefix(mapping.Original, "catalog-fixture-") {
					custom = append(custom, mapping)
				}
			}
			require.ElementsMatch(t, []models.WordReplacement{healthy, damaged}, custom, "repair/retry must preserve IDs, mappings, modes and healthy timestamps")
			got, err := os.ReadFile(configPath)
			require.NoError(t, err)
			require.Equal(t, configBefore, got)
		})
	}
}
