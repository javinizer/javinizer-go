package info_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/info"
	"github.com/stretchr/testify/require"
)

func TestUpdateStatusContractInfoCacheFailure(t *testing.T) {
	for _, fixture := range []string{"malformed cache", "blocked cache parent"} {
		for _, failurePrefix := range []string{"", "  - Last checked: never", "  - Latest version: (unknown)"} {
			t.Run(fixture+"/"+failurePrefix, func(t *testing.T) {
				dir := t.TempDir()
				cachePath := filepath.Join(dir, "update_cache.json")
				preservedPath := cachePath
				preserved := []byte("{invalid cache")
				if fixture == "blocked cache parent" {
					dir = filepath.Join(dir, "not-a-directory")
					preservedPath = dir
					preserved = []byte("keep cache obstruction")
				}
				require.NoError(t, os.WriteFile(preservedPath, preserved, 0600))
				t.Setenv("JAVINIZER_DATA_DIR", dir)
				t.Setenv("JAVINIZER_DB", "")
				t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
				// The application's database remains valid and independent of the cache
				// obstruction, so the command reaches the actual GetStatus consumer.
				configPath, _ := createTestConfig(t, WithVersionCheckEnabled(true))
				writeErr := errors.New("status output unavailable")
				writer := &updateLineFailWriter{err: writeErr}
				writer.prefix = failurePrefix
				var stderr bytes.Buffer
				cmd := info.NewCommand()
				cmd.Flags().String("config", configPath, "config file")
				cmd.SetOut(writer)
				cmd.SetErr(&stderr)
				cmd.SilenceUsage, cmd.SilenceErrors = true, true
				cmd.SetArgs(nil)
				err := cmd.ExecuteContext(t.Context())
				if failurePrefix != "" {
					require.ErrorIs(t, err, writeErr)
					require.Equal(t, 1, writer.failures)
					require.NotContains(t, writer.String(), "Latest version: (unknown)")
					if failurePrefix == "  - Latest version: (unknown)" {
						require.Contains(t, writer.String(), "  - Last checked: never\n")
					} else {
						require.NotContains(t, writer.String(), "Last checked: never")
					}
				} else {
					require.NoError(t, err)
					require.Contains(t, writer.String(), "  - Last checked: never\n  - Latest version: (unknown)\n")
				}
				require.Contains(t, writer.String(), "Update:\n")
				require.Empty(t, stderr.String(), "cache errors are status fields, not the unreachable Go-error diagnostic")
				after, err := os.ReadFile(preservedPath)
				require.NoError(t, err)
				require.Equal(t, preserved, after)
			})
		}
	}
}
