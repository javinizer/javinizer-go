package system

import (
	"net/http"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/stretchr/testify/require"
)

func TestJavLibraryNameOverrides_DecodeFailuresPreservePublishedRule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "recognized field has wrong value type",
			body:    `{"source_name":42,"first_name":"Shiori","last_name":"Yorimoto","expected_rule":{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto"}}`,
			message: "Invalid name correction format",
		},
		{
			name:    "recognized field has truncated value",
			body:    `{"source_name":"Yorimoto Shiori","first_name":`,
			message: "Provide source_name, first_name, last_name and explicit expected_rule only",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
				"javlibrary": {ActressNameOverrides: scraperconfig.ActressNameOverrides{"aa6sc": originalNameRule}},
			}
			h, router := nameOverrideFixture(t, cfg)
			before, err := os.ReadFile(h.updates.configFile)
			require.NoError(t, err)
			beforeSnapshot := h.updates.rt.DiskConfigSnapshot().Clone()
			beforeRuntime := h.updates.deps.CoreDeps.GetConfig().Clone()

			response := nameOverrideRequest(t, router, http.MethodPut, "aa6sc", tc.body)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "NAME_OVERRIDE_INVALID")
			require.Contains(t, response.Body.String(), tc.message)
			after, err := os.ReadFile(h.updates.configFile)
			require.NoError(t, err)
			require.Equal(t, before, after, "malformed requests must not rewrite even unrelated settings")
			require.Equal(t, beforeSnapshot, h.updates.rt.DiskConfigSnapshot())
			require.Equal(t, beforeRuntime, h.updates.deps.CoreDeps.GetConfig())

			response = nameOverrideRequest(t, router, http.MethodGet, "aa6sc", "")
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.JSONEq(t, `{"rule":{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto"}}`, response.Body.String())
		})
	}
}
