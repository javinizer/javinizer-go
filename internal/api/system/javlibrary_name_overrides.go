package system

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
)

const (
	nameOverrideBodyLimit   = 4 << 10
	nameOverrideFieldLimit  = 512
	nameOverrideInvalidCode = "NAME_OVERRIDE_INVALID"
)

var nameOverrideSourceID = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
var errNameOverrideConfigDiverged = errors.New("saved name correction differs from the last successfully applied configuration")

// JavLibraryNameOverrideRequest edits only one source-specific name split.
// ExpectedRule must be explicitly null for a new rule, or the exact rule most
// recently read. It includes the original source label, not just its field split.
type JavLibraryNameOverrideRequest struct {
	SourceName   string                             `json:"source_name" binding:"required"`
	FirstName    string                             `json:"first_name" binding:"required"`
	LastName     string                             `json:"last_name" binding:"required"`
	ExpectedRule *scraperconfig.ActressNameOverride `json:"expected_rule" binding:"required" extensions:"x-nullable"`
}

// JavLibraryNameOverrideResponse never includes unrelated config or credentials.
type JavLibraryNameOverrideResponse struct {
	Rule *scraperconfig.ActressNameOverride `json:"rule" extensions:"x-nullable"`
}

type javLibraryNameOverrides struct {
	updates *ConfigUpdateService
}

func newJavLibraryNameOverrides(rt *core.APIRuntime) *javLibraryNameOverrides {
	return &javLibraryNameOverrides{updates: NewConfigUpdateService(rt, rt.Deps().ConfigFile)}
}

// diskConfig verifies saved rules against disk before returning an isolated
// pre-environment snapshot. A failed rollback can leave disk newer than the last
// successfully applied snapshot. Neither GET nor a retry may claim success from
// that stale snapshot. GET checks its target only; PUT checks the whole correction
// map so saving B cannot erase a disk-only A after a failed rollback. This does not
// reconcile other settings or publish unverified disk changes to the runtime.
// Callers hold ConfigUpdateMu across reading, comparing, saving and reloading.
func (h *javLibraryNameOverrides) diskConfig(sourceID string, verifyAllRules bool) (*config.Config, error) {
	disk, err := config.Load(h.updates.configFile)
	if err != nil {
		return nil, err
	}
	if cfg := h.updates.rt.DiskConfigSnapshot(); cfg != nil {
		matches := sameNameOverride(savedNameOverride(cfg, sourceID), savedNameOverride(disk, sourceID))
		if verifyAllRules {
			// maps.Equal intentionally treats absent and empty maps alike.
			matches = maps.Equal(savedNameOverrides(cfg), savedNameOverrides(disk))
		}
		if !matches {
			return nil, errNameOverrideConfigDiverged
		}
		return cfg, nil
	}
	return disk, nil
}

func sameNameOverride(a, b *scraperconfig.ActressNameOverride) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func savedNameOverrides(cfg *config.Config) scraperconfig.ActressNameOverrides {
	settings := cfg.Scrapers.Overrides["javlibrary"]
	if settings == nil {
		return nil
	}
	return settings.ActressNameOverrides
}

func savedNameOverride(cfg *config.Config, sourceID string) *scraperconfig.ActressNameOverride {
	rule, found := savedNameOverrides(cfg)[sourceID]
	if !found {
		return nil
	}
	return &rule
}

func nameOverrideError(c *gin.Context, status int, code, message string) {
	c.JSON(status, contracts.ErrorResponse{Error: message, Code: code})
}

func validNameOverrideSourceID(c *gin.Context) bool {
	if nameOverrideSourceID.MatchString(c.Param("sourceID")) {
		return true
	}
	nameOverrideError(c, http.StatusBadRequest, nameOverrideInvalidCode, "Source ID must contain 1 to 64 letters or digits")
	return false
}

// get godoc
// @Summary Get a saved JavLibrary name correction
// @Description Get the durable name-split rule for one JavLibrary performer ID, or null if none exists. Requires authentication. Does not return unrelated settings or update historical credits.
// @Tags system
// @Produce json
// @Param sourceID path string true "JavLibrary performer ID (1-64 alphanumeric characters)"
// @Success 200 {object} JavLibraryNameOverrideResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 401 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/scrapers/javlibrary/name-overrides/{sourceID} [get]
func (h *javLibraryNameOverrides) get(c *gin.Context) {
	if !validNameOverrideSourceID(c) {
		return
	}
	h.updates.rt.GetRuntime().ConfigUpdateMu.Lock()
	defer h.updates.rt.GetRuntime().ConfigUpdateMu.Unlock()
	cfg, err := h.diskConfig(c.Param("sourceID"), false)
	if err != nil {
		nameOverrideError(c, http.StatusInternalServerError, "NAME_OVERRIDE_CONFIG_UNAVAILABLE", "Could not verify saved name corrections. Check configuration before retrying")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, JavLibraryNameOverrideResponse{Rule: savedNameOverride(cfg, c.Param("sourceID"))})
}

func decodeNameOverrideRequest(c *gin.Context) (*JavLibraryNameOverrideRequest, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, nameOverrideBodyLimit)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			nameOverrideError(c, http.StatusRequestEntityTooLarge, nameOverrideInvalidCode, "Name correction request is too large")
		} else {
			nameOverrideError(c, http.StatusBadRequest, nameOverrideInvalidCode, "Could not read name correction request")
		}
		return nil, false
	}
	fields, valid := nameOverrideObjectFields(body, "source_name", "first_name", "last_name", "expected_rule")
	if !valid {
		nameOverrideError(c, http.StatusBadRequest, nameOverrideInvalidCode, "Provide source_name, first_name, last_name and explicit expected_rule only")
		return nil, false
	}
	if !bytes.Equal(bytes.TrimSpace(fields["expected_rule"]), []byte("null")) {
		if _, valid := nameOverrideObjectFields(fields["expected_rule"], "source_name", "first_name", "last_name"); !valid {
			nameOverrideError(c, http.StatusBadRequest, nameOverrideInvalidCode, "expected_rule must be null or the complete previously saved rule")
			return nil, false
		}
	}
	var req JavLibraryNameOverrideRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		nameOverrideError(c, http.StatusBadRequest, nameOverrideInvalidCode, "Invalid name correction format")
		return nil, false
	}
	return &req, true
}

// Require exact field names and reject duplicate keys, including in the prior
// rule. Last-key-wins decoding would make the compare-and-swap ambiguous.
func nameOverrideObjectFields(body []byte, required ...string) (map[string]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		key, ok := token.(string)
		if !ok || !slices.Contains(required, key) {
			return nil, false
		}
		if _, exists := fields[key]; exists {
			return nil, false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil || len(fields) != len(required) {
		return nil, false
	}
	var trailing any
	return fields, errors.Is(decoder.Decode(&trailing), io.EOF)
}

func validNameOverrideFields(rule scraperconfig.ActressNameOverride) bool {
	for _, field := range []string{rule.SourceName, rule.FirstName, rule.LastName} {
		if len(field) > nameOverrideFieldLimit || !utf8.ValidString(field) || strings.ContainsFunc(field, func(r rune) bool {
			return unicode.IsControl(r) && !unicode.IsSpace(r)
		}) {
			return false
		}
	}
	return rule.Valid()
}

// put godoc
// @Summary Save a JavLibrary name correction
// @Description Save one guarded name split for future scrapes, preserving all other settings and historical credits. The source label must reconstruct from the two nonempty fields in either order; native-script and single-stage labels are not eligible. expected_rule is required: null means no prior rule, otherwise provide the complete rule returned by GET. A stale prior rule returns 409. Saving does not enable a disabled scraper. Maximum body size is 4 KiB; each new-rule field is limited to 512 bytes. The decoded source label is preserved verbatim; only component whitespace is normalized.
// @Tags system
// @Accept json
// @Produce json
// @Param sourceID path string true "JavLibrary performer ID (1-64 alphanumeric characters)"
// @Param correction body JavLibraryNameOverrideRequest true "Guarded name correction"
// @Success 200 {object} JavLibraryNameOverrideResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 401 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 413 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/scrapers/javlibrary/name-overrides/{sourceID} [put]
func (h *javLibraryNameOverrides) put(c *gin.Context) {
	if !validNameOverrideSourceID(c) {
		return
	}
	req, ok := decodeNameOverrideRequest(c)
	if !ok {
		return
	}
	// Labels have already been decoded by the scraper. Do not HTML-unescape
	// again, case-fold, transliterate, or change the semantic source guard.
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	rule := scraperconfig.ActressNameOverride{
		SourceName: req.SourceName, FirstName: req.FirstName, LastName: req.LastName,
	}
	if !validNameOverrideFields(rule) {
		nameOverrideError(c, http.StatusBadRequest, nameOverrideInvalidCode, "Provide matching first and last names for a non-native, multi-part source label; fields must use supported text and be at most 512 bytes each")
		return
	}
	rule.FirstName = normalize(rule.FirstName)
	rule.LastName = normalize(rule.LastName)

	h.updates.rt.GetRuntime().ConfigUpdateMu.Lock()
	defer h.updates.rt.GetRuntime().ConfigUpdateMu.Unlock()
	cfg, err := h.diskConfig(c.Param("sourceID"), true)
	if err != nil {
		nameOverrideError(c, http.StatusInternalServerError, "NAME_OVERRIDE_CONFIG_UNAVAILABLE", "Could not verify saved name corrections. Check configuration before retrying")
		return
	}
	current := savedNameOverride(cfg, c.Param("sourceID"))
	if !sameNameOverride(current, req.ExpectedRule) {
		nameOverrideError(c, http.StatusConflict, "NAME_OVERRIDE_CONFLICT", "This saved correction changed. Reload it before saving again")
		return
	}
	if cfg.Scrapers.Overrides == nil {
		cfg.Scrapers.Overrides = make(map[string]*models.ScraperSettings)
	}
	settings := cfg.Scrapers.Overrides["javlibrary"]
	if settings == nil {
		settings = &models.ScraperSettings{}
		// This narrow request has no enabled field. Preserve inherited module
		// defaults rather than turning an absent block into explicit false.
		settings.SetEnabledPresence(false)
		cfg.Scrapers.Overrides["javlibrary"] = settings
	}
	if settings.ActressNameOverrides == nil {
		settings.ActressNameOverrides = make(scraperconfig.ActressNameOverrides)
	}
	settings.ActressNameOverrides[c.Param("sourceID")] = rule
	if err := h.updates.ValidateAndApply(h.updates.deps.CoreDeps.GetConfig(), cfg, nil); err != nil {
		h.writeSaveError(c, err)
		return
	}
	c.JSON(http.StatusOK, JavLibraryNameOverrideResponse{Rule: &rule})
}

// Never expose general config errors here: they can contain unrelated settings,
// filesystem paths or credentials. The service records diagnostic details.
func (h *javLibraryNameOverrides) writeSaveError(c *gin.Context, err error) {
	var invalid *validationError
	var reloaded *reloadError
	var rollback *rollbackError
	switch {
	case errors.As(err, &invalid):
		nameOverrideError(c, http.StatusBadRequest, "NAME_OVERRIDE_CONFIG_INVALID", "The current configuration is invalid. Review Settings before saving this correction")
	case errors.As(err, &rollback):
		nameOverrideError(c, http.StatusInternalServerError, "NAME_OVERRIDE_ROLLBACK_FAILED", "Could not reload or restore configuration. Check the saved configuration before retrying")
	case errors.As(err, &reloaded):
		nameOverrideError(c, http.StatusInternalServerError, "NAME_OVERRIDE_RELOAD_FAILED", "Could not reload the correction; the previous configuration was restored")
	default:
		nameOverrideError(c, http.StatusInternalServerError, "NAME_OVERRIDE_SAVE_FAILED", "Could not save the name correction")
	}
}
