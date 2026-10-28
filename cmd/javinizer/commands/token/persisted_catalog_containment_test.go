package token_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/token"
	tokenservice "github.com/javinizer/javinizer-go/internal/api/token"
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
	root.AddCommand(token.NewCommand())
	var output, diagnostics bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&diagnostics)
	root.SetArgs(append([]string{"--config", configPath, "token"}, args...))
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

type persistedTokenRow struct {
	ID, Name, TokenHash, TokenPrefix                       string
	CreatedType, CreatedValue, LastUsedValue, RevokedValue string
}

func snapshotPersistedTokens(t *testing.T, db *database.DB) []persistedTokenRow {
	t.Helper()
	var rows []persistedTokenRow
	require.NoError(t, db.Raw(`SELECT id, name, token_hash, token_prefix,
 typeof(created_at) AS created_type, quote(created_at) AS created_value,
 quote(last_used_at) AS last_used_value, quote(revoked_at) AS revoked_value
 FROM api_tokens ORDER BY id`).Scan(&rows).Error)
	return rows
}

func TestTokenCLIContainsDamagedPersistedCatalog(t *testing.T) {
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

	repo := database.NewApiTokenRepository(deps.DB)
	svc := tokenservice.NewTokenService(repo)
	var fixtures []*models.ApiToken
	var plaintexts []string
	for _, name := range []string{"catalog-healthy", "catalog-damaged", "catalog-revoked"} {
		record, plaintext, err := svc.Create(t.Context(), name)
		require.NoError(t, err)
		fixtures = append(fixtures, record)
		// Only newly generated synthetic credentials exist here. Keep plaintext in
		// memory solely for negative leakage assertions; never print or save it.
		plaintexts = append(plaintexts, plaintext)
	}
	healthy, damaged, revoked := fixtures[0], fixtures[1], fixtures[2]
	require.NoError(t, repo.UpdateLastUsed(t.Context(), healthy.ID))
	require.NoError(t, svc.Revoke(t.Context(), revoked.ID))
	healthy, err = repo.FindByID(t.Context(), healthy.ID)
	require.NoError(t, err)
	require.NotNil(t, healthy.LastUsedAt)
	revoked, err = repo.FindByID(t.Context(), revoked.ID)
	require.NoError(t, err)
	require.NotNil(t, revoked.RevokedAt)
	require.Nil(t, healthy.RevokedAt)
	require.Nil(t, damaged.RevokedAt)
	require.NotEqual(t, healthy.ID, damaged.ID)
	require.NotEqual(t, healthy.ID, revoked.ID)
	require.NotEqual(t, damaged.ID, revoked.ID)
	pristine := snapshotPersistedTokens(t, deps.DB)
	require.Len(t, pristine, 3)
	// Externally damaged DATETIME storage in a normally migrated SQLite database,
	// not an ordinary token-service write or a substituted repository/driver.
	result := deps.DB.Exec("UPDATE api_tokens SET created_at = ? WHERE id = ?", []byte{0xff}, damaged.ID)
	require.NoError(t, result.Error)
	require.EqualValues(t, 1, result.RowsAffected)
	before := snapshotPersistedTokens(t, deps.DB)
	foundDamaged := false
	for _, row := range before {
		if row.ID == damaged.ID {
			foundDamaged = true
			require.Equal(t, "blob", row.CreatedType)
			require.Equal(t, "X'FF'", row.CreatedValue)
		}
	}
	require.True(t, foundDamaged)
	for _, args := range [][]string{{"list"}, {"list", "--json"}} {
		console, output, err := executePersistedCatalogCLI(t, configPath, args...)
		require.ErrorContains(t, err, "failed to list tokens:")
		require.ErrorContains(t, err, "Scan error")
		require.ErrorContains(t, err, "created_at")
		require.NotContains(t, err.Error(), "failed to initialize dependencies")
		require.Empty(t, output, "failure must not emit a partial table, JSON or an empty-success result")
		for _, marker := range []string{"No active tokens found", "PREFIX", `"token_prefix":`, `"token_hash":`, `"token":`} {
			require.NotContains(t, console, marker)
		}
		for i, record := range fixtures {
			for _, secret := range []string{record.TokenHash, plaintexts[i]} {
				require.False(t, strings.Contains(console+output+err.Error(), secret), "failed listing leaked synthetic credential material")
			}
			require.NotContains(t, console, record.Name)
			require.NotContains(t, console, record.ID)
			require.NotContains(t, console, record.TokenPrefix)
		}
		require.Equal(t, before, snapshotPersistedTokens(t, deps.DB), "listing must not rotate/revoke tokens or alter names, usage or damaged storage")
		got, err := os.ReadFile(configPath)
		require.NoError(t, err)
		require.Equal(t, configBefore, got)
	}

	result = deps.DB.Exec("UPDATE api_tokens SET created_at = ? WHERE id = ?", damaged.CreatedAt, damaged.ID)
	require.NoError(t, result.Error)
	require.EqualValues(t, 1, result.RowsAffected)
	for _, args := range [][]string{{"list"}, {"list", "--json"}} {
		console, output, err := executePersistedCatalogCLI(t, configPath, args...)
		require.NoError(t, err)
		require.NotContains(t, output, revoked.ID)
		require.NotContains(t, output, revoked.TokenPrefix)
		require.NotContains(t, output, revoked.Name)
		for i, record := range fixtures {
			for _, secret := range []string{record.TokenHash, plaintexts[i]} {
				require.False(t, strings.Contains(console+output, secret), "successful listing leaked synthetic credential material")
			}
		}
		if len(args) == 2 {
			var entries []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(output), &entries))
			require.Len(t, entries, 2)
			for _, entry := range entries {
				require.Len(t, entry, 5)
				for _, key := range []string{"id", "name", "token_prefix", "created_at", "last_used_at"} {
					require.Contains(t, entry, key)
				}
			}
			var listed []struct {
				ID          string     `json:"id"`
				Name        string     `json:"name"`
				TokenPrefix string     `json:"token_prefix"`
				CreatedAt   time.Time  `json:"created_at"`
				LastUsedAt  *time.Time `json:"last_used_at"`
			}
			require.NoError(t, json.Unmarshal([]byte(output), &listed))
			got := make(map[string]models.ApiToken)
			for _, entry := range listed {
				got[entry.ID] = models.ApiToken{ID: entry.ID, Name: entry.Name, TokenPrefix: entry.TokenPrefix, CreatedAt: entry.CreatedAt, LastUsedAt: entry.LastUsedAt}
			}
			for _, expected := range []*models.ApiToken{healthy, damaged} {
				public := *expected
				public.TokenHash = ""
				require.Equal(t, public, got[expected.ID])
			}
		} else {
			require.Contains(t, output, "PREFIX")
			require.Contains(t, output, healthy.Name)
			require.Contains(t, output, damaged.Name)
		}
		require.Equal(t, pristine, snapshotPersistedTokens(t, deps.DB), "repair and retry must preserve all token state, including the revoked control")
		got, err := os.ReadFile(configPath)
		require.NoError(t, err)
		require.Equal(t, configBefore, got)
	}
}
