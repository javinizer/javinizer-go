package system

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nameOverrideURL = "/api/v1/scrapers/javlibrary/name-overrides/"

var originalNameRule = scraperconfig.ActressNameOverride{SourceName: "Yorimoto Shiori", FirstName: "Shiori", LastName: "Yorimoto"}

// No repositories or DB are provided: this route must only edit configuration,
// never canonical identities, existing movie credits, or existing scrape results.
func nameOverrideFixture(t *testing.T, cfg *config.Config) (*javLibraryNameOverrides, *gin.Engine) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, config.Save(cfg, path))
	registry := scraperutil.NewScraperRegistry()
	registry.Register(scraperutil.ScraperRegistration{Name: "javlibrary", Defaults: models.ScraperSettings{Enabled: false, Language: "en"}})
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry}, ConfigFile: path}
	rt := core.NewAPIRuntime(deps)
	rt.EnsureRuntime()
	t.Cleanup(func() { rt.GetRuntime().Shutdown() })
	rt.SetInitialConfigs(cfg.Clone(), cfg)
	h := newJavLibraryNameOverrides(rt)
	h.updates.reload = func(*core.APIRuntime, *core.APIDeps, *config.Config) error { return nil }
	router := gin.New()
	router.GET(nameOverrideURL+":sourceID", h.get)
	router.PUT(nameOverrideURL+":sourceID", h.put)
	return h, router
}

func nameOverrideRequest(t *testing.T, router http.Handler, method, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, nameOverrideURL+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func nameOverrideBody(t *testing.T, rule scraperconfig.ActressNameOverride, expected *scraperconfig.ActressNameOverride) string {
	t.Helper()
	body, err := json.Marshal(JavLibraryNameOverrideRequest{SourceName: rule.SourceName, FirstName: rule.FirstName, LastName: rule.LastName, ExpectedRule: expected})
	require.NoError(t, err)
	return string(body)
}

func TestJavLibraryNameOverrides_CreateReadReplace(t *testing.T) {
	h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
	w := nameOverrideRequest(t, router, http.MethodGet, "aa6sc", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"rule":null}`, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	w = nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response JavLibraryNameOverrideResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, &originalNameRule, response.Rule)
	cfg, err := config.Load(h.updates.configFile)
	require.NoError(t, err)
	require.Equal(t, &originalNameRule, savedNameOverride(cfg, "aa6sc"))
	require.Equal(t, &originalNameRule, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
	require.Equal(t, &originalNameRule, savedNameOverride(h.updates.deps.CoreDeps.GetConfig(), "aa6sc"))

	reversed := originalNameRule
	reversed.FirstName, reversed.LastName = reversed.LastName, reversed.FirstName
	w = nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, reversed, &originalNameRule))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = nameOverrideRequest(t, router, http.MethodGet, "aa6sc", "")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, &reversed, response.Rule)
	// Fresh runtime after restart sees the durable rule without the old snapshot.
	h.updates.rt.SetInitialConfigs(cfg.Clone(), nil)
	w = nameOverrideRequest(t, router, http.MethodGet, "aa6sc", "")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, &reversed, response.Rule)
}

func TestJavLibraryNameOverrides_StrictRequest(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
	}{
		{"missing expected", `{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto"}`, 400},
		{"missing source", `{"first_name":"Shiori","last_name":"Yorimoto","expected_rule":null}`, 400},
		{"unknown field", `{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto","expected_rule":null,"enabled":true}`, 400},
		{"unknown nested field", `{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto","expected_rule":{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto","secret":"x"}}`, 400},
		{"invalid expected type", `{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto","expected_rule":false}`, 400},
		{"empty expected", `{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto","expected_rule":{}}`, 400},
		{"null", `null`, 400},
		{"array", `[]`, 400},
		{"malformed", `{`, 400},
		{"trailing json", nameOverrideBody(t, originalNameRule, nil) + `{}`, 400},
		{"oversized", strings.Repeat(" ", nameOverrideBodyLimit) + nameOverrideBody(t, originalNameRule, nil), 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
			before, err := os.ReadFile(h.updates.configFile)
			require.NoError(t, err)
			w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", tt.body)
			require.Equal(t, tt.status, w.Code, w.Body.String())
			after, err := os.ReadFile(h.updates.configFile)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Nil(t, savedNameOverride(h.updates.deps.CoreDeps.GetConfig(), "aa6sc"))
		})
	}
}

func TestJavLibraryNameOverrides_Validation(t *testing.T) {
	for _, id := range []string{"aa-6sc", "aa_6sc", "a.b", strings.Repeat("x", 65), "%20", "%E6%97%A5"} {
		t.Run("id "+id, func(t *testing.T) {
			_, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				w := nameOverrideRequest(t, router, method, id, nameOverrideBody(t, originalNameRule, nil))
				require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			}
		})
	}
	rules := []scraperconfig.ActressNameOverride{
		{SourceName: "水卜 さくら", FirstName: "さくら", LastName: "水卜"},
		{SourceName: "Tiara", FirstName: "Tiara"},
		{SourceName: "Tia ra", FirstName: "Tia", LastName: "Other"},
		{SourceName: "Yorimoto Shiori", FirstName: "shiori", LastName: "Yorimoto"},
		{SourceName: "Yorimoto Shiori", FirstName: " ", LastName: "Yorimoto"},
		{SourceName: "Yori\x00moto Shiori", FirstName: "Shiori", LastName: "Yori\x00moto"},
		{SourceName: strings.Repeat("x", nameOverrideFieldLimit) + " Shiori", FirstName: "Shiori", LastName: strings.Repeat("x", nameOverrideFieldLimit)},
	}
	for _, rule := range rules {
		t.Run(rule.SourceName, func(t *testing.T) {
			h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
			w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, rule, nil))
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Nil(t, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
		})
	}
	for _, id := range []string{"A", strings.Repeat("9", 64)} {
		t.Run("valid id "+id, func(t *testing.T) {
			_, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
			w := nameOverrideRequest(t, router, http.MethodPut, id, nameOverrideBody(t, originalNameRule, nil))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		})
	}
}

func TestJavLibraryNameOverrides_PreservesSourceAndOnlyNormalizesComponents(t *testing.T) {
	for _, source := range []string{"  Mary   &amp; Jane   Doe  ", "Mary & Jane Doe"} {
		t.Run(source, func(t *testing.T) {
			h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
			first := "  Mary   & Jane  "
			if strings.Contains(source, "&amp;") {
				first = "  Mary   &amp; Jane  "
			}
			rule := scraperconfig.ActressNameOverride{SourceName: source, FirstName: first, LastName: " Doe "}
			w := nameOverrideRequest(t, router, http.MethodPut, "encoded", nameOverrideBody(t, rule, nil))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			saved := savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "encoded")
			require.Equal(t, source, saved.SourceName)
			require.Equal(t, strings.Join(strings.Fields(first), " "), saved.FirstName)
			require.Equal(t, "Doe", saved.LastName)
		})
	}
}

func TestJavLibraryNameOverrides_PreservesUnrelatedAndDisabledSettings(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	require.NoError(t, json.Unmarshal([]byte(`{"scrapers":{"javlibrary":{"enabled":false,"base_url":"https://example.test","cookies":{"session":"private-cookie"},"actress_name_overrides":{"keep":{"source_name":"Jane Doe","first_name":"Jane","last_name":"Doe"}}},"other":{"enabled":false,"api_key":"private-key"}}}`), cfg))
	cfg.Metadata.Translation.OpenAI.APIKey = "disk-secret"
	cfg.Logging.Level = "warn"
	cfg.Server.Port = 9001
	t.Setenv("OPENAI_API_KEY", "env-only-secret")
	t.Setenv("LOG_LEVEL", "debug")
	h, router := nameOverrideFixture(t, cfg)
	live := cfg.Clone()
	config.ApplyEnvironmentOverrides(live)
	h.updates.rt.SetInitialConfigs(live, cfg)
	beforeRule := savedNameOverride(cfg, "keep")
	w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	disk, err := config.Load(h.updates.configFile)
	require.NoError(t, err)
	require.Equal(t, "disk-secret", disk.Metadata.Translation.OpenAI.APIKey)
	require.Equal(t, "env-only-secret", h.updates.deps.CoreDeps.GetConfig().Metadata.Translation.OpenAI.APIKey)
	require.Equal(t, 9001, disk.Server.Port)
	require.Equal(t, "warn", disk.Logging.Level)
	require.Equal(t, "debug", h.updates.deps.CoreDeps.GetConfig().Logging.Level)
	require.Equal(t, cfg.Scrapers.Overrides["other"], disk.Scrapers.Overrides["other"])
	settings := disk.Scrapers.Overrides["javlibrary"]
	require.False(t, settings.Enabled)
	require.False(t, h.updates.deps.CoreDeps.GetConfig().Scrapers.ResolvedSettings("javlibrary").Enabled)
	require.Equal(t, "https://example.test", settings.BaseURL)
	require.Equal(t, map[string]string{"session": "private-cookie"}, settings.Cookies)
	require.Equal(t, beforeRule, savedNameOverride(disk, "keep"))
	// Removing only our new entry must recover the same effective config.
	delete(settings.ActressNameOverrides, "aa6sc")
	expectedJSON, err := json.Marshal(cfg.Scrapers.Overrides["javlibrary"])
	require.NoError(t, err)
	actualJSON, err := json.Marshal(settings)
	require.NoError(t, err)
	var expectedFields, actualFields map[string]any
	require.NoError(t, json.Unmarshal(expectedJSON, &expectedFields))
	require.NoError(t, json.Unmarshal(actualJSON, &actualFields))
	delete(expectedFields, "enabled") // Sparse storage can omit the unchanged module default.
	delete(actualFields, "enabled")
	require.Equal(t, expectedFields, actualFields)
	for _, secret := range []string{"disk-secret", "env-only-secret", "private-cookie", "private-key", "9001"} {
		require.NotContains(t, w.Body.String(), secret)
	}
}

func TestJavLibraryNameOverrides_StaleAndConcurrentWrites(t *testing.T) {
	h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
	body := nameOverrideBody(t, originalNameRule, nil)
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodPut, nameOverrideURL+"aa6sc", strings.NewReader(body))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			statuses <- w.Code
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	got := []int{}
	for code := range statuses {
		got = append(got, code)
	}
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, got)
	stale := originalNameRule
	stale.SourceName = "Shiori Yorimoto" // Same field split but a different source guard.
	w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, &stale))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	w = nameOverrideRequest(t, router, http.MethodPut, "missing", nameOverrideBody(t, originalNameRule, &originalNameRule))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Equal(t, &originalNameRule, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
}

func TestJavLibraryNameOverrides_ConcurrentDifferentRulesPreserved(t *testing.T) {
	h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
	body := nameOverrideBody(t, originalNameRule, nil)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, id := range []string{"first", "second"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPut, nameOverrideURL+id, strings.NewReader(body))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	for code := range statuses {
		require.Equal(t, http.StatusOK, code)
	}
	saved, err := config.Load(h.updates.configFile)
	require.NoError(t, err)
	require.Equal(t, &originalNameRule, savedNameOverride(saved, "first"))
	require.Equal(t, &originalNameRule, savedNameOverride(saved, "second"))
}

func TestJavLibraryNameOverrides_UsesLatestFullConfigSnapshot(t *testing.T) {
	h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
	newer := h.updates.rt.DiskConfigSnapshot()
	newer.Server.Port = 9999
	require.NoError(t, h.updates.ValidateAndApply(h.updates.deps.CoreDeps.GetConfig(), newer, nil))
	w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	saved, err := config.Load(h.updates.configFile)
	require.NoError(t, err)
	require.Equal(t, 9999, saved.Server.Port)
}

func TestJavLibraryNameOverrides_FailureRollbackAndSafeErrors(t *testing.T) {
	for _, failure := range []string{"persist", "reload", "validation", "rollback"} {
		t.Run(failure, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			h, router := nameOverrideFixture(t, cfg)
			originalBytes, err := os.ReadFile(h.updates.configFile)
			require.NoError(t, err)
			save := h.updates.saveSparse
			saves, reloads := 0, 0
			secretError := errors.New("private-password /private/config.yaml")
			h.updates.saveSparse = func(cfg *config.Config, path string, ctx config.SparseSaveContext) error {
				saves++
				if failure == "persist" || (failure == "rollback" && saves == 2) {
					return secretError
				}
				return save(cfg, path, ctx)
			}
			h.updates.reload = func(*core.APIRuntime, *core.APIDeps, *config.Config) error { reloads++; return secretError }
			if failure == "validation" {
				h.updates.finalize = func(*config.ScrapersConfig, models.ScraperConfigResolverInterface) error { return secretError }
			}
			w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
			status := http.StatusInternalServerError
			if failure == "validation" {
				status = http.StatusBadRequest
			}
			require.Equal(t, status, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "private-password")
			require.NotContains(t, w.Body.String(), "/private/config.yaml")
			require.Nil(t, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
			require.Nil(t, savedNameOverride(h.updates.deps.CoreDeps.GetConfig(), "aa6sc"))
			switch failure {
			case "persist", "validation":
				require.Zero(t, reloads)
				after, err := os.ReadFile(h.updates.configFile)
				require.NoError(t, err)
				require.Equal(t, originalBytes, after)
			case "reload":
				require.Equal(t, 2, saves)
				require.Equal(t, 1, reloads)
				disk, err := config.Load(h.updates.configFile)
				require.NoError(t, err)
				require.Nil(t, savedNameOverride(disk, "aa6sc"))
				require.Contains(t, w.Body.String(), "NAME_OVERRIDE_RELOAD_FAILED")
			case "rollback":
				require.Equal(t, 2, saves)
				require.Contains(t, w.Body.String(), "NAME_OVERRIDE_ROLLBACK_FAILED")
			}
		})
	}
}

func TestJavLibraryNameOverrides_ReadFailureAndBodyReadFailure(t *testing.T) {
	h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
	h.updates.rt.SetInitialConfigs(h.updates.deps.CoreDeps.GetConfig(), nil)
	h.updates.configFile = t.TempDir() // A directory cannot be read as YAML.
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		w := nameOverrideRequest(t, router, method, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
		require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "NAME_OVERRIDE_CONFIG_UNAVAILABLE")
		require.NotContains(t, w.Body.String(), h.updates.configFile)
	}
	req := httptest.NewRequest(http.MethodPut, nameOverrideURL+"aa6sc", &failedNameOverrideBody{})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

type failedNameOverrideBody struct{}

func (*failedNameOverrideBody) Read([]byte) (int, error) {
	return 0, errors.New("private read failure")
}

func TestJavLibraryNameOverrides_RequestSizeBoundary(t *testing.T) {
	_, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
	body := nameOverrideBody(t, originalNameRule, nil)
	body += strings.Repeat(" ", nameOverrideBodyLimit-len(body))
	w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", body)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestJavLibraryNameOverrides_OnlyNarrowResponse(t *testing.T) {
	_, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
	w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	decoder := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	decoder.DisallowUnknownFields()
	var response JavLibraryNameOverrideResponse
	require.NoError(t, decoder.Decode(&response))
	require.Equal(t, &originalNameRule, response.Rule)
}

// Covers real reload/config publication plus the real parser over loopback HTTP.
// Network destination injection is test-only; production network wiring and
// allowed-host validation remain unchanged.
func TestJavLibraryNameOverrides_ReloadsActiveScraper(t *testing.T) {
	var requests atomic.Int32
	var unexpected atomic.Bool
	var fixture *httptest.Server
	fixture = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Host != fixture.Listener.Addr().String() || r.Method != http.MethodGet || r.URL.IsAbs() || r.URL.Path != "/en/vl_searchbyid.php" || r.URL.Query().Get("keyword") != "TEST-001" {
			unexpected.Store(true)
			http.Error(w, "unexpected request, including performer-profile fetch", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>TEST-001 Fixture - JAVLibrary</title></head><body><div id="video_info"></div><span class="star"><a href="vl_star.php?s=aa6sc">Yorimoto Shiori</a></span></body></html>`))
	}))
	fixture.Start()
	t.Cleanup(fixture.Close)
	cfg := config.DefaultConfig(nil, nil)
	require.NoError(t, json.Unmarshal([]byte(`{"scrapers":{"priority":["javlibrary"],"javlibrary":{"enabled":true,"language":"en","base_url":"http://www.javlibrary.com","rate_limit":0,"retry_count":0,"timeout":2}}}`), cfg))
	h, router := nameOverrideFixture(t, cfg)
	h.updates.reload = reloadComponents
	w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	checkScrape := func() {
		t.Helper()
		registry := h.updates.deps.CoreDeps.GetRegistry()
		loaded, ok := registry.GetInstance("javlibrary")
		require.True(t, ok)
		require.Equal(t, originalNameRule, loaded.Config().ActressNameOverrides["aa6sc"])
		// Keep the real configuration validation and reload above. At the
		// existing constructor boundary, copy the published scraper settings
		// and point only the HTTP destination at this loopback fixture. A local
		// proxy is insufficient: the offline guard rejects external ultimate
		// URLs before proxy selection, even when no upstream would be contacted.
		settings := loaded.Config().Clone()
		settings.BaseURL = fixture.URL
		constructor, ok := registry.GetScraperConstructor("javlibrary")
		require.True(t, ok)
		instance, err := constructor(scraperutil.ScraperDeps{Settings: settings})
		require.NoError(t, err)
		registry.RegisterInstance(instance)
		require.NoError(t, loaded.Close())
		require.Equal(t, originalNameRule, instance.Config().ActressNameOverrides["aa6sc"])
		result, err := instance.Search(t.Context(), "TEST-001")
		require.NoError(t, err)
		require.Equal(t, []models.ActressInfo{{FirstName: "Shiori", LastName: "Yorimoto"}}, result.Actresses)
		require.Equal(t, []models.CastNameProvenance{{Source: "javlibrary", SourceID: "aa6sc", DisplayName: "Yorimoto Shiori", State: models.CastNameUserCorrected}}, result.CastNameProvenance)
		// Own each generation, including the old registry replaced below.
		instances := h.updates.deps.CoreDeps.GetRegistry().GetAllInstances()
		t.Cleanup(func() {
			for _, s := range instances {
				_ = s.Close()
			}
		})
	}
	checkScrape()
	// Rebuild directly from the saved file, as on restart, not the live config.
	disk, err := config.Load(h.updates.configFile)
	require.NoError(t, err)
	require.Equal(t, &originalNameRule, savedNameOverride(disk, "aa6sc"))
	require.NoError(t, h.updates.rt.ReloadConfig(disk))
	checkScrape()
	require.False(t, unexpected.Load(), "no performer-profile or other requests")
	require.EqualValues(t, 2, requests.Load(), "exactly one detail request per scrape")
}

func TestJavLibraryNameOverrides_RejectsDuplicateFields(t *testing.T) {
	for _, body := range []string{
		`{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto","expected_rule":null,"expected_rule":null}`,
		`{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto","expected_rule":{"source_name":"Yorimoto Shiori","source_name":"Shiori Yorimoto","first_name":"Shiori","last_name":"Yorimoto"}}`,
	} {
		_, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
		w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestJavLibraryNameOverrides_AllowsExactLargerPriorRule(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	old := scraperconfig.ActressNameOverride{SourceName: strings.Repeat("Long ", 110) + "Name", FirstName: strings.Repeat("Long ", 110), LastName: "Name"}
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{"javlibrary": {ActressNameOverrides: scraperconfig.ActressNameOverrides{"aa6sc": old}}}
	h, router := nameOverrideFixture(t, cfg)
	w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, &old))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, &originalNameRule, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
}

func TestJavLibraryNameOverrides_PreservesInheritedEnabledState(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
		h.updates.deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{Name: "javlibrary", Defaults: models.ScraperSettings{Enabled: enabled, Language: "en"}})
		w := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, enabled, h.updates.deps.CoreDeps.GetConfig().Scrapers.ResolvedSettings("javlibrary").Enabled)
		saved, err := config.Load(h.updates.configFile)
		require.NoError(t, err)
		require.NoError(t, saved.Scrapers.Finalize(h.updates.deps.CoreDeps.ScraperRegistry))
		require.Equal(t, enabled, saved.Scrapers.ResolvedSettings("javlibrary").Enabled)
	}
}
