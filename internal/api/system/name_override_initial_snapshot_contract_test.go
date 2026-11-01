package system_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/system"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/stretchr/testify/require"
)

// SetInitialConfigs accepts pre-environment snapshots from startup adapters.
// An absent override map is a valid empty snapshot, even though config.Load
// normally materializes defaults. Exercise the registered route and real reload,
// not a private service callback or a claim about loader-produced nil maps.
func TestNameOverrideInitialSnapshotAllocatesAbsentMap(t *testing.T) {
	// Real reload changes the process logger; keep this root serial and restore
	// the package's default logger, not an invented copy of an unknown logger.
	t.Cleanup(func() { logging.CloseLogger(); require.NoError(t, logging.InitLogger(nil)) })
	// Isolate every public environment override before any real load/reload.
	for _, key := range []string{"LOG_LEVEL", "UMASK", "JAVINIZER_TEMP_DIR", "JAVINIZER_DB", "JAVINIZER_R18DEV_DUMP_PATH", "JAVINIZER_LOG_DIR", "TRANSLATION_PROVIDER", "TRANSLATION_SOURCE_LANGUAGE", "TRANSLATION_TARGET_LANGUAGE", "OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL", "DEEPL_API_KEY", "GOOGLE_TRANSLATE_API_KEY", "OPENAI_COMPATIBLE_API_KEY", "ANTHROPIC_API_KEY", "METADATA_TRANSLATION_PROVIDER", "METADATA_TRANSLATION_SOURCE_LANGUAGE", "METADATA_TRANSLATION_TARGET_LANGUAGE", "METADATA_TRANSLATION_TIMEOUT_SECONDS", "METADATA_TRANSLATION_APPLY_TO_PRIMARY", "METADATA_TRANSLATION_OVERWRITE_EXISTING_TARGET"} {
		t.Setenv(key, "")
	}
	t.Setenv("OPENAI_API_KEY", "runtime-only-secret")
	t.Setenv("LOG_LEVEL", "debug")
	for _, absent := range []bool{true, false} {
		name := "nil map"
		if !absent {
			name = "empty map control"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.yaml")
			disk := config.DefaultConfig(nil, nil)
			disk.Logging.Output = "stdout"
			disk.Logging.Level = "warn"
			disk.Server.Port = 9031
			disk.System.TempDir = filepath.Join(root, "scratch")
			disk.Database.DSN = filepath.Join(root, "unused.db")
			disk.Metadata.R18DevDump.Enabled = false
			disk.Metadata.R18DevDump.Path = filepath.Join(root, "unused-dump.db")
			disk.Metadata.Translation.OpenAI.APIKey = "disk-only-secret"
			if absent {
				disk.Scrapers.Overrides = nil
			} else {
				disk.Scrapers.Overrides = map[string]*models.ScraperSettings{}
			}
			require.NoError(t, config.Save(disk, path))
			before := disk.Clone()
			live := disk.Clone()
			config.ApplyEnvironmentOverrides(live)
			deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{}, ConfigFile: path}
			rt := core.NewAPIRuntime(deps)
			rt.EnsureRuntime()
			t.Cleanup(func() { rt.Shutdown(); rt.GetRuntime().Shutdown(); require.NoError(t, deps.CoreDeps.Close()) })
			require.NoError(t, rt.ReloadConfig(live))
			rt.SetInitialConfigs(live, disk)
			if absent {
				require.Nil(t, rt.DiskConfigSnapshot().Scrapers.Overrides)
			}
			initialRegistry := deps.CoreDeps.GetRegistry()
			router := gin.New()
			system.RegisterRoutes(router.Group("/api/v1"), rt)
			rule := scraperconfig.ActressNameOverride{SourceName: "Yorimoto Shiori", FirstName: "Shiori", LastName: "Yorimoto"}
			request := func(method string, expected *scraperconfig.ActressNameOverride, next scraperconfig.ActressNameOverride) *httptest.ResponseRecorder {
				t.Helper()
				body, err := json.Marshal(system.JavLibraryNameOverrideRequest{SourceName: next.SourceName, FirstName: next.FirstName, LastName: next.LastName, ExpectedRule: expected})
				require.NoError(t, err)
				req := httptest.NewRequest(method, "/api/v1/scrapers/javlibrary/name-overrides/aa6sc", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				out := httptest.NewRecorder()
				router.ServeHTTP(out, req)
				return out
			}
			out := request(http.MethodPut, nil, rule)
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			var response system.JavLibraryNameOverrideResponse
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &response))
			require.Equal(t, &rule, response.Rule)
			require.NotSame(t, initialRegistry, deps.CoreDeps.GetRegistry(), "real reload replaces registry")
			require.Equal(t, before, disk, "caller-owned startup snapshot remains unchanged")
			loaded, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, rule, loaded.Scrapers.Overrides["javlibrary"].ActressNameOverrides["aa6sc"])
			require.Equal(t, "disk-only-secret", loaded.Metadata.Translation.OpenAI.APIKey)
			require.Equal(t, "warn", loaded.Logging.Level)
			require.Equal(t, 9031, loaded.Server.Port)
			require.Equal(t, "runtime-only-secret", deps.CoreDeps.GetConfig().Metadata.Translation.OpenAI.APIKey)
			require.Equal(t, "debug", deps.CoreDeps.GetConfig().Logging.Level)
			require.Equal(t, rule, deps.CoreDeps.GetConfig().Scrapers.Overrides["javlibrary"].ActressNameOverrides["aa6sc"])
			persisted, err := os.ReadFile(path)
			require.NoError(t, err)
			published := rt.DiskConfigSnapshot()
			active := deps.CoreDeps.GetConfig().Clone()
			reversed := rule
			reversed.FirstName, reversed.LastName = rule.LastName, rule.FirstName
			out = request(http.MethodPut, nil, reversed)
			require.Equal(t, http.StatusConflict, out.Code, out.Body.String())
			require.Contains(t, out.Body.String(), "NAME_OVERRIDE_CONFLICT")
			afterConflict, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, persisted, afterConflict)
			require.Equal(t, published, rt.DiskConfigSnapshot())
			require.Equal(t, active, deps.CoreDeps.GetConfig())
			out = request(http.MethodPut, &rule, reversed)
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			reloaded, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, reversed, reloaded.Scrapers.Overrides["javlibrary"].ActressNameOverrides["aa6sc"])
			out = request(http.MethodGet, nil, rule)
			require.Equal(t, http.StatusOK, out.Code)
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &response))
			require.Equal(t, &reversed, response.Rule)
			require.Equal(t, before, disk)
			require.NoFileExists(t, disk.Database.DSN)
			require.NoFileExists(t, disk.Metadata.R18DevDump.Path)
		})
	}
}
