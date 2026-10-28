package actress_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/actress"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// The commands use fmt.Printf as well as Cobra's writer. These nonparallel tests
// capture both and the cached application logger, using a regular file so
// large lists cannot fill a pipe buffer.
func executePersistedCatalogCLI(t *testing.T, configPath string, args ...string) (string, string, error) {
	t.Helper()
	root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "config file")
	root.AddCommand(actress.NewCommand())
	var output, diagnostics bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&diagnostics)
	root.SetArgs(append([]string{"--config", configPath, "actress"}, args...))
	capturePath := filepath.Join(t.TempDir(), "console.txt")
	capture, err := os.Create(capturePath)
	require.NoError(t, err)
	var commandErr, closeErr error
	func() {
		oldOut, oldErr := os.Stdout, os.Stderr
		os.Stdout, os.Stderr = capture, capture
		defer func() { os.Stdout, os.Stderr = oldOut, oldErr; closeErr = capture.Close() }()
		// The logger retains its writer independently of the process handles.
		restoreLog := logging.SetOutput(capture)
		defer restoreLog()
		commandErr = root.Execute()
	}()
	require.NoError(t, closeErr)
	console, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	return string(console) + diagnostics.String(), output.String(), commandErr
}

type persistedActressRow struct {
	ID                                                                    uint
	DMMID                                                                 int
	FirstName, LastName, JapaneseName, ThumbURL, Aliases, Origin, NameKey string
	Verified, AmbiguityQuarantined                                        bool
	CreatedType, CreatedValue, UpdatedValue                               string
}

type persistedActressLink struct {
	MovieContentID string
	ActressID      uint
}

type persistedActressState struct {
	Rows    []persistedActressRow
	Links   []persistedActressLink
	Credits []models.MovieCredit
}

func snapshotPersistedActresses(t *testing.T, db *database.DB) persistedActressState {
	t.Helper()
	var state persistedActressState
	// Read native SQLite storage rather than scanning the damaged time.Time.
	require.NoError(t, db.Raw(`SELECT id, dmm_id, first_name, last_name, japanese_name,
 thumb_url, aliases, verified, origin, name_key, ambiguity_quarantined,
 typeof(created_at) AS created_type, quote(created_at) AS created_value,
 quote(updated_at) AS updated_value FROM actresses ORDER BY id`).Scan(&state.Rows).Error)
	require.NoError(t, db.Table("movie_actresses").Order("movie_content_id, actress_id").Find(&state.Links).Error)
	require.NoError(t, db.Order("id").Find(&state.Credits).Error)
	return state
}

func TestActressCLIContainsDamagedPersistedCatalog(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		damaged                   any
		storageType, storageValue string
		jsonFailure               bool
	}{
		{name: "export_unreadable_timestamp", damaged: []byte{0xff}, storageType: "blob", storageValue: "X'FF'"},
		{name: "export_non_json_timestamp", damaged: int64(253402300800), storageType: "integer", storageValue: "253402300800", jsonFailure: true},
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
			// Complete startup before snapshots; built-in word timestamps may change normally.
			deps, err := commandutil.NewDependencies(cfg)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, deps.Close()) })

			repo := database.NewActressRepository(deps.DB)
			timestamp := time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC)
			healthy := models.Actress{DMMID: 72001, FirstName: "Healthy", LastName: "CatalogFixture", JapaneseName: "健全試験", Aliases: "healthy fixture alias", Verified: true, Origin: database.ActressOriginUser, CreatedAt: timestamp, UpdatedAt: timestamp}
			damaged := models.Actress{DMMID: 72002, FirstName: "Damaged", LastName: "CatalogFixture", JapaneseName: "破損試験", Aliases: "damaged fixture alias", Verified: true, Origin: database.ActressOriginUser, CreatedAt: timestamp, UpdatedAt: timestamp}
			require.NoError(t, repo.Create(t.Context(), &healthy))
			require.NoError(t, repo.Create(t.Context(), &damaged))
			require.NotZero(t, healthy.ID)
			require.NotZero(t, damaged.ID)
			require.NotEqual(t, healthy.ID, damaged.ID)
			// Verified user identities reach export without entering startup's candidate
			// quarantine backfill. Links/credits must survive failed read-only exports.
			movie := models.Movie{ContentID: "identity-catalog-fixture", ID: "IDENTITY-CATALOG-FIXTURE", Title: "Owned catalog fixture", Actresses: []models.Actress{healthy, damaged}}
			require.NoError(t, deps.DB.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: damaged.ID, CreditedName: damaged.FullName(), CreditedJapaneseName: damaged.JapaneseName, Origin: string(models.CreditOriginUser)}
			require.NoError(t, deps.DB.Create(&credit).Error)
			pristine := snapshotPersistedActresses(t, deps.DB)
			require.Len(t, pristine.Rows, 2)
			for _, row := range pristine.Rows {
				require.True(t, row.Verified, "startup must not treat these identities as unverified candidates")
				require.Equal(t, database.ActressOriginUser, row.Origin)
			}
			var candidates int64
			require.NoError(t, deps.DB.Model(&models.Actress{}).Where("verified = ?", false).Count(&candidates).Error)
			require.Zero(t, candidates)
			require.Len(t, pristine.Links, 2)
			require.Len(t, pristine.Credits, 1)

			// Damaged/externally edited data, not app-generated timestamps. The existing
			// non-STRICT DATETIME column admits BLOBs and Unix seconds for year 10000.
			result := deps.DB.Exec("UPDATE actresses SET created_at = ? WHERE id = ?", tc.damaged, damaged.ID)
			require.NoError(t, result.Error)
			require.EqualValues(t, 1, result.RowsAffected)
			before := snapshotPersistedActresses(t, deps.DB)
			require.Equal(t, damaged.ID, before.Rows[1].ID)
			require.Equal(t, tc.storageType, before.Rows[1].CreatedType)
			require.Equal(t, tc.storageValue, before.Rows[1].CreatedValue)
			if tc.jsonFailure {
				readable, err := repo.FindByID(t.Context(), damaged.ID)
				require.NoError(t, err, "the year must be readable before export serialization")
				require.Equal(t, 10000, readable.CreatedAt.Year())
			}
			exportPath := filepath.Join(dir, "backup.json")
			sentinel := []byte("existing identity backup must survive a failed export\n")
			require.NoError(t, os.WriteFile(exportPath, sentinel, 0o600))
			for _, args := range [][]string{{"export", exportPath}, {"export"}} {
				console, output, err := executePersistedCatalogCLI(t, configPath, args...)
				if tc.jsonFailure {
					require.ErrorContains(t, err, "failed to marshal JSON:")
					var marshalErr *json.MarshalerError
					require.ErrorAs(t, err, &marshalErr)
					require.Equal(t, reflect.TypeOf(&time.Time{}), marshalErr.Type)
					require.ErrorContains(t, marshalErr.Err, "year outside of range")
				} else {
					require.ErrorContains(t, err, "failed to list actresses:")
					require.ErrorContains(t, err, "Scan error")
					require.ErrorContains(t, err, "created_at")
				}
				require.NotContains(t, err.Error(), "failed to initialize dependencies")
				require.Empty(t, output, "failed export must not emit partial JSON")
				for _, partial := range []string{`"first_name":`, `"dmm_id":`, healthy.JapaneseName, damaged.JapaneseName, healthy.Aliases, damaged.Aliases, "CatalogFixture", "Exported "} {
					require.NotContains(t, console, partial)
				}
				got, err := os.ReadFile(exportPath)
				require.NoError(t, err)
				require.Equal(t, sentinel, got)
				require.Equal(t, before, snapshotPersistedActresses(t, deps.DB), "failure must preserve all identities, aliases, links, credits and damaged storage")
				got, err = os.ReadFile(configPath)
				require.NoError(t, err)
				require.Equal(t, configBefore, got)
			}

			// Repair only the timestamp and retry both destinations with fresh commands.
			result = deps.DB.Exec("UPDATE actresses SET created_at = ? WHERE id = ?", timestamp, damaged.ID)
			require.NoError(t, result.Error)
			require.EqualValues(t, 1, result.RowsAffected)
			for _, args := range [][]string{{"export", exportPath}, {"export"}} {
				console, output, err := executePersistedCatalogCLI(t, configPath, args...)
				require.NoError(t, err)
				require.Contains(t, console, "Exported 2 actress(es)")
				data := []byte(output)
				if len(args) == 2 {
					require.Empty(t, output, "file export must not also write JSON to stdout")
					data, err = os.ReadFile(exportPath)
					require.NoError(t, err)
				}
				var exported []models.Actress
				require.NoError(t, json.Unmarshal(data, &exported))
				require.ElementsMatch(t, []models.Actress{healthy, damaged}, exported, "retry must retain complete identities and their original timestamps")
				require.Equal(t, pristine, snapshotPersistedActresses(t, deps.DB), "repair and export must not duplicate or merge identities")
				got, err := os.ReadFile(configPath)
				require.NoError(t, err)
				require.Equal(t, configBefore, got)
			}
		})
	}
}
