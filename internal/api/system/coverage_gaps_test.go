package system

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

type unknownScraper struct {
	name string
}

func (s *unknownScraper) Name() string { return s.name }
func (s *unknownScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *unknownScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *unknownScraper) IsEnabled() bool                                { return true }
func (s *unknownScraper) Config() *models.ScraperSettings                { return nil }
func (s *unknownScraper) Close() error                                   { return nil }
func (s *unknownScraper) SupportsMovieSearch() bool                      { return false }
func (s *unknownScraper) ResolveActressMetadata(_ context.Context, a models.ActressInfo) (models.ActressInfo, error) {
	return a, nil
}

func TestSystemGaps_SupportsActressMetadataNonAllowlist(t *testing.T) {
	s := &unknownScraper{name: "custom-scraper"}
	assert.False(t, supportsActressMetadata(s))
}

func TestSystemGaps_ValidatePriorityActressOnlyRejected(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&unknownScraper{name: "custom-actress-only"})
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: reg}}
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Priority.Fields = map[string][]string{"actress": {"custom-actress-only"}}
	err := validatePriorityFieldCapabilities(deps, cfg)
	assert.Error(t, err)
}

func TestSystemGaps_ProxyTestInvalidMode(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)
	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))
	body := bytes.NewBufferString(`{"mode":"invalid","target_url":"http://example.com"}`)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/proxy/test", body))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
