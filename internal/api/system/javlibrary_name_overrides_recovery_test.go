package system

import (
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/stretchr/testify/require"
)

func TestJavLibraryNameOverrides_UncertainRollbackFailureFailsClosed(t *testing.T) {
	h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
	save := h.updates.saveSparse
	saves := 0
	h.updates.saveSparse = func(cfg *config.Config, path string, ctx config.SparseSaveContext) error {
		saves++
		if saves == 2 {
			return errors.New("private rollback failure")
		}
		return save(cfg, path, ctx)
	}
	h.updates.reload = func(*core.APIRuntime, *core.APIDeps, *config.Config) error {
		return errors.New("private reload failure")
	}
	body := nameOverrideBody(t, originalNameRule, nil)
	failed := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", body)
	require.Equal(t, http.StatusInternalServerError, failed.Code)
	require.Contains(t, failed.Body.String(), "NAME_OVERRIDE_ROLLBACK_FAILED")
	disk, err := config.Load(h.updates.configFile)
	require.NoError(t, err)
	require.Equal(t, &originalNameRule, savedNameOverride(disk, "aa6sc"), "the first save succeeded but rollback failed")
	require.Nil(t, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
	persisted, err := os.ReadFile(h.updates.configFile)
	require.NoError(t, err)

	// If the failed PUT response never reached the client, an authoritative GET
	// or direct retry must not claim the stale null rule is the saved state.
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := nameOverrideRequest(t, router, method, "aa6sc", body)
		require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "NAME_OVERRIDE_CONFIG_UNAVAILABLE")
		require.NotContains(t, response.Body.String(), `"rule"`)
		require.NotContains(t, response.Body.String(), "private")
		require.NotContains(t, response.Body.String(), h.updates.configFile)
	}
	// Reading B is still truthful, but saving B from the old snapshot would
	// otherwise replace the correction map and erase disk-only A.
	other := nameOverrideRequest(t, router, http.MethodGet, "other", "")
	require.Equal(t, http.StatusOK, other.Code, other.Body.String())
	require.JSONEq(t, `{"rule":null}`, other.Body.String())
	other = nameOverrideRequest(t, router, http.MethodPut, "other", body)
	require.Equal(t, http.StatusInternalServerError, other.Code, other.Body.String())
	require.Contains(t, other.Body.String(), "NAME_OVERRIDE_CONFIG_UNAVAILABLE")
	require.Equal(t, 2, saves, "uncertain state must not cause any additional save attempt")
	unchanged, err := os.ReadFile(h.updates.configFile)
	require.NoError(t, err)
	require.Equal(t, persisted, unchanged)
	require.Nil(t, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"), "do not reconcile or mutate snapshot")
	require.Nil(t, savedNameOverride(h.updates.deps.CoreDeps.GetConfig(), "aa6sc"), "do not publish unverified disk state")
}

func TestJavLibraryNameOverrides_ReadAfterLostResponse(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful save", true: "successful rollback"}[rollback], func(t *testing.T) {
			h, router := nameOverrideFixture(t, config.DefaultConfig(nil, nil))
			if rollback {
				h.updates.reload = func(*core.APIRuntime, *core.APIDeps, *config.Config) error { return errors.New("reload failed") }
			}
			response := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
			if rollback {
				require.Equal(t, http.StatusInternalServerError, response.Code)
			} else {
				require.Equal(t, http.StatusOK, response.Code)
			}
			response = nameOverrideRequest(t, router, http.MethodGet, "aa6sc", "")
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			if rollback {
				require.JSONEq(t, `{"rule":null}`, response.Body.String())
			} else {
				require.JSONEq(t, `{"rule":{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto"}}`, response.Body.String())
			}
		})
	}
}

func TestJavLibraryNameOverrides_VerifiesExactTargetRuleOnDisk(t *testing.T) {
	for _, change := range []string{"split", "source guard", "removed", "malformed", "unreadable"} {
		t.Run(change, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{"javlibrary": {ActressNameOverrides: scraperconfig.ActressNameOverrides{"aa6sc": originalNameRule}}}
			h, router := nameOverrideFixture(t, cfg)
			disk := cfg.Clone()
			changed := originalNameRule
			switch change {
			case "split":
				changed.FirstName, changed.LastName = changed.LastName, changed.FirstName
				disk.Scrapers.Overrides["javlibrary"].ActressNameOverrides["aa6sc"] = changed
				require.NoError(t, config.Save(disk, h.updates.configFile))
			case "source guard":
				// Whitespace-equivalent labels are still distinct full CAS values.
				changed.SourceName = " Yorimoto Shiori "
				disk.Scrapers.Overrides["javlibrary"].ActressNameOverrides["aa6sc"] = changed
				require.NoError(t, config.Save(disk, h.updates.configFile))
			case "removed":
				require.NoError(t, os.Remove(h.updates.configFile))
			case "malformed":
				require.NoError(t, os.WriteFile(h.updates.configFile, []byte("private: [invalid yaml"), 0600))
			case "unreadable":
				h.updates.configFile = t.TempDir()
			}
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				response := nameOverrideRequest(t, router, method, "aa6sc", nameOverrideBody(t, originalNameRule, &originalNameRule))
				require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), "NAME_OVERRIDE_CONFIG_UNAVAILABLE")
				require.NotContains(t, response.Body.String(), "private")
				require.NotContains(t, response.Body.String(), h.updates.configFile)
			}
			require.Equal(t, &originalNameRule, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
			require.Equal(t, &originalNameRule, savedNameOverride(h.updates.deps.CoreDeps.GetConfig(), "aa6sc"))
		})
	}
}

func TestJavLibraryNameOverrides_DiskFallbackAndBoundedVerification(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{"javlibrary": {ActressNameOverrides: scraperconfig.ActressNameOverrides{"aa6sc": originalNameRule}}}
	h, router := nameOverrideFixture(t, cfg)
	// A change outside the correction map must not publish or reconcile unrelated
	// disk settings. GET checks its target; PUT checks only the correction map.
	disk := cfg.Clone()
	disk.Server.Port = 9999
	require.NoError(t, config.Save(disk, h.updates.configFile))
	response := nameOverrideRequest(t, router, http.MethodGet, "aa6sc", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, cfg.Server.Port, h.updates.rt.DiskConfigSnapshot().Server.Port)
	require.Equal(t, cfg.Server.Port, h.updates.deps.CoreDeps.GetConfig().Server.Port)

	h.updates.rt.SetInitialConfigs(cfg.Clone(), nil)
	response = nameOverrideRequest(t, router, http.MethodGet, "aa6sc", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"source_name":"Yorimoto Shiori"`)
	require.Nil(t, h.updates.rt.DiskConfigSnapshot(), "GET must not manufacture a published snapshot")
	changed := originalNameRule
	changed.FirstName, changed.LastName = changed.LastName, changed.FirstName
	response = nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, changed, &originalNameRule))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, &changed, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
	require.Equal(t, 9999, h.updates.rt.DiskConfigSnapshot().Server.Port, "without a snapshot, use disk as the pre-env mutation base")
}

func TestJavLibraryNameOverrides_PutTreatsAbsentAndEmptyMapsEqually(t *testing.T) {
	for _, snapshotEmpty := range []bool{false, true} {
		cfg := config.DefaultConfig(nil, nil)
		if snapshotEmpty {
			cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{"javlibrary": {ActressNameOverrides: scraperconfig.ActressNameOverrides{}}}
		}
		h, router := nameOverrideFixture(t, cfg)
		// config.Save omits the empty map, so reading disk yields a nil map.
		response := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", nameOverrideBody(t, originalNameRule, nil))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, &originalNameRule, savedNameOverride(h.updates.rt.DiskConfigSnapshot(), "aa6sc"))
	}
}
