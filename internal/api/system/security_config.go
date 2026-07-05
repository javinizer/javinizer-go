package system

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
)

// SecurityUpdateRequest carries the operator-editable subset of the api.security
// block. The frontend Security settings section PUTs this narrow payload (rather
// than the whole config) so a single-section save never risks clobbering
// unrelated fields with a stale GET snapshot. All other security fields
// (rate_limit, trusted_proxies, force_secure_cookies, allowed_origins,
// max_files_per_scan, scan_timeout_seconds) are preserved from the running config.
type SecurityUpdateRequest struct {
	AllowedDirectories []string `json:"allowed_directories"`
	DeniedDirectories  []string `json:"denied_directories"`
	AllowUNC           bool     `json:"allow_unc"`
	AllowedUNCServers  []string `json:"allowed_unc_servers"`
}

// securityResponse echoes the persisted security block so the frontend can
// confirm what landed on disk after Prepare/normalization runs.
type securityResponse struct {
	Security config.SecurityConfig `json:"security"`
}

// updateSecurityConfig godoc
// @Summary Update security settings
// @Description Update the operator-editable api.security block (allowed_directories, denied_directories, allow_unc, allowed_unc_servers) and hot-reload the server. Other security fields are preserved. Requires an authenticated operator session.
// @Tags system
// @Accept json
// @Produce json
// @Param security body SecurityUpdateRequest true "Security block fields to persist"
// @Success 200 {object} securityResponse "Persisted security block after reload"
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/config/security [put]
func updateSecurityConfig(rt *core.APIRuntime) gin.HandlerFunc {
	deps := rt.Deps()
	svc := NewConfigUpdateService(rt, deps.ConfigFile)

	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Invalid security configuration format"})
			return
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Invalid security configuration format"})
			return
		}

		for _, key := range []string{"allowed_directories", "denied_directories", "allow_unc", "allowed_unc_servers"} {
			if _, ok := raw[key]; !ok {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Missing required field: " + key})
				return
			}
		}

		var req SecurityUpdateRequest
		if err := json.Unmarshal(body, &req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Invalid security configuration format"})
			return
		}

		rt.GetRuntime().ConfigUpdateMu.Lock()
		defer rt.GetRuntime().ConfigUpdateMu.Unlock()

		oldCfg := deps.CoreDeps.GetConfig()
		newCfg := oldCfg.Clone()
		newCfg.API.Security.AllowedDirectories = req.AllowedDirectories
		newCfg.API.Security.DeniedDirectories = req.DeniedDirectories
		newCfg.API.Security.AllowUNC = req.AllowUNC
		newCfg.API.Security.AllowedUNCServers = req.AllowedUNCServers

		if err := svc.ValidateAndApply(oldCfg, newCfg, nil); err != nil {
			status, msg := mapConfigErrorToHTTP(err)
			c.JSON(status, contracts.ErrorResponse{Error: msg})
			return
		}

		c.JSON(http.StatusOK, securityResponse{
			Security: deps.CoreDeps.GetConfig().API.Security,
		})
	}
}
