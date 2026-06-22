package models

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryOperation_String(t *testing.T) {
	assert.Equal(t, "scrape", HistoryOpScrape.String())
	assert.Equal(t, "organize", HistoryOpOrganize.String())
	assert.Equal(t, "download", HistoryOpDownload.String())
	assert.Equal(t, "nfo", HistoryOpNFO.String())
}

func TestHistoryOperation_Scan(t *testing.T) {
	var op HistoryOperation
	err := op.Scan([]byte("scrape"))
	require.NoError(t, err)
	assert.Equal(t, HistoryOpScrape, op)
}

func TestHistoryOperation_ScanString(t *testing.T) {
	var op HistoryOperation
	err := op.Scan("nfo")
	require.NoError(t, err)
	assert.Equal(t, HistoryOpNFO, op)
}

func TestHistoryOperation_ScanInvalid(t *testing.T) {
	var op HistoryOperation
	op.Scan(123) // no-op for unsupported types
	assert.Equal(t, HistoryOperation(""), op)
}

func TestHistoryOperation_Value(t *testing.T) {
	v, err := HistoryOpScrape.Value()
	require.NoError(t, err)
	assert.Equal(t, "scrape", v)
}

func TestHistoryStatus_String(t *testing.T) {
	assert.Equal(t, "success", HistoryStatusSuccess.String())
	assert.Equal(t, "failed", HistoryStatusFailed.String())
	assert.Equal(t, "reverted", HistoryStatusReverted.String())
}

func TestHistoryStatus_Scan(t *testing.T) {
	var s HistoryStatus
	err := s.Scan([]byte("success"))
	require.NoError(t, err)
	assert.Equal(t, HistoryStatusSuccess, s)
}

func TestHistoryStatus_ScanString(t *testing.T) {
	var s HistoryStatus
	err := s.Scan("failed")
	require.NoError(t, err)
	assert.Equal(t, HistoryStatusFailed, s)
}

func TestHistoryStatus_ScanInvalid(t *testing.T) {
	var s HistoryStatus
	s.Scan(42) // no-op for unsupported types
	assert.Equal(t, HistoryStatus(""), s)
}

func TestHistoryStatus_Value(t *testing.T) {
	v, err := HistoryStatusSuccess.Value()
	require.NoError(t, err)
	assert.Equal(t, "success", v)
}

func TestFormatActressName_FullName(t *testing.T) {
	a := Actress{
		FirstName:    "John",
		LastName:     "Doe",
		JapaneseName: "山田",
	}
	result := FormatActressName(a, FormatActressNameOptions{})
	assert.Contains(t, result, "Doe")
}

func TestFormatActressName_OnlyLastName(t *testing.T) {
	a := Actress{LastName: "Doe"}
	result := FormatActressName(a, FormatActressNameOptions{})
	assert.Contains(t, result, "Doe")
}

func TestFormatActressName_OnlyJapaneseName(t *testing.T) {
	a := Actress{JapaneseName: "山田"}
	result := FormatActressName(a, FormatActressNameOptions{JapaneseNames: true})
	assert.Equal(t, "山田", result)
}

func TestFormatActressName_Empty(t *testing.T) {
	a := Actress{}
	result := FormatActressName(a, FormatActressNameOptions{})
	assert.Equal(t, "Unknown", result)
}

func TestFormatActressName_FirstNameOrder(t *testing.T) {
	a := Actress{FirstName: "John", LastName: "Doe"}
	result := FormatActressName(a, FormatActressNameOptions{FirstNameOrder: true})
	assert.Equal(t, "John Doe", result)
}

func TestFormatActressName_SkipUnknown(t *testing.T) {
	a := Actress{}
	result := FormatActressName(a, FormatActressNameOptions{UnknownActressMode: UnknownActressModeSkip})
	assert.Equal(t, "", result)
}

func TestBrowserConfigValidate_Disabled(t *testing.T) {
	cfg := BrowserConfig{Enabled: false}
	err := cfg.Validate("browser")
	assert.NoError(t, err)
}

func TestBrowserConfigValidate_EnabledValid(t *testing.T) {
	cfg := BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 3, WindowWidth: 1280, WindowHeight: 720, SlowMo: 0}
	err := cfg.Validate("browser")
	assert.NoError(t, err)
}

func TestBrowserConfigValidate_TimeoutTooLow(t *testing.T) {
	cfg := BrowserConfig{Enabled: true, Timeout: 0}
	err := cfg.Validate("browser")
	assert.Error(t, err)
}

func TestBrowserConfigValidate_MaxRetriesTooHigh(t *testing.T) {
	cfg := BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 11, WindowWidth: 1280, WindowHeight: 720}
	err := cfg.Validate("browser")
	assert.Error(t, err)
}

func TestBrowserConfigValidate_WindowWidthTooLow(t *testing.T) {
	cfg := BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 3, WindowWidth: 100, WindowHeight: 720}
	err := cfg.Validate("browser")
	assert.Error(t, err)
}

func TestBrowserConfigValidate_WindowHeightTooHigh(t *testing.T) {
	cfg := BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 3, WindowWidth: 1280, WindowHeight: 9999}
	err := cfg.Validate("browser")
	assert.Error(t, err)
}

func TestFlareSolverrConfigValidate_Disabled(t *testing.T) {
	cfg := FlareSolverrConfig{Enabled: false}
	err := cfg.Validate("flaresolverr")
	assert.NoError(t, err)
}

func TestFlareSolverrConfigValidate_EnabledValid(t *testing.T) {
	cfg := FlareSolverrConfig{Enabled: true, URL: "http://localhost:8191", Timeout: 60, MaxRetries: 3, SessionTTL: 300}
	err := cfg.Validate("flaresolverr")
	assert.NoError(t, err)
}

func TestFlareSolverrConfigValidate_EnabledNoURL(t *testing.T) {
	cfg := FlareSolverrConfig{Enabled: true}
	err := cfg.Validate("flaresolverr")
	assert.Error(t, err)
}

func TestFlareSolverrConfigValidate_TimeoutTooHigh(t *testing.T) {
	cfg := FlareSolverrConfig{Enabled: true, URL: "http://localhost:8191", Timeout: 301}
	err := cfg.Validate("flaresolverr")
	assert.Error(t, err)
}

func TestFlareSolverrConfigValidate_SessionTTLTooLow(t *testing.T) {
	cfg := FlareSolverrConfig{Enabled: true, URL: "http://localhost:8191", Timeout: 60, SessionTTL: 10}
	err := cfg.Validate("flaresolverr")
	assert.Error(t, err)
}

func TestOrchestrationState_Clone(t *testing.T) {
	errMsg := "poster failed"
	orig := OrchestrationState{
		DisplayTitleApplied: true,
		PosterGenerated:     false,
		Persisted:           true,
		PosterError:         &errMsg,
	}
	cloned := orig.Clone()
	assert.Equal(t, orig.DisplayTitleApplied, cloned.DisplayTitleApplied)
	assert.Equal(t, orig.Persisted, cloned.Persisted)
	assert.Equal(t, *orig.PosterError, *cloned.PosterError)
	// Verify it's a deep copy
	*cloned.PosterError = "changed"
	assert.NotEqual(t, *orig.PosterError, *cloned.PosterError)
}

func TestOrchestrationState_CloneNil(t *testing.T) {
	var orig *OrchestrationState
	cloned := orig.Clone()
	assert.Equal(t, OrchestrationState{}, cloned)
}

func TestRescrapeStatus_Scan(t *testing.T) {
	var rs RescrapeStatus
	err := rs.Scan([]byte(`"needed"`))
	require.NoError(t, err)
}

func TestRescrapeStatus_ScanString(t *testing.T) {
	var rs RescrapeStatus
	err := rs.Scan("needed")
	require.NoError(t, err)
}

func TestRescrapeStatus_ScanInvalid(t *testing.T) {
	var rs RescrapeStatus
	err := rs.Scan(123)
	assert.Error(t, err)
}

func TestRescrapeStatus_ScanNil(t *testing.T) {
	var rs RescrapeStatus
	err := rs.Scan(nil)
	require.NoError(t, err)
	assert.Equal(t, RescrapeStatus(""), rs)
}

func TestRescrapeStatus_Value(t *testing.T) {
	rs := RescrapeStatusSuccess
	v, err := rs.Value()
	require.NoError(t, err)
	assert.Equal(t, "success", v)
}

func TestResolveSearchQueryForScraper_NoResolver(t *testing.T) {
	// A scraper that doesn't implement ScraperQueryResolver returns input unchanged, false
	s := &mockNoResolverScraper{}
	result, ok := ResolveSearchQueryForScraper(s, "ABC-001")
	assert.Equal(t, "", result)
	assert.False(t, ok)
}

// mockNoResolverScraper is a minimal Scraper that doesn't implement ScraperQueryResolver
type mockNoResolverScraper struct{}

func (m *mockNoResolverScraper) Name() string { return "mock" }
func (m *mockNoResolverScraper) Search(_ context.Context, _ string) (*ScraperResult, error) {
	return nil, nil
}
func (m *mockNoResolverScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockNoResolverScraper) IsEnabled() bool                                    { return true }
func (m *mockNoResolverScraper) Close() error                                       { return nil }
func (m *mockNoResolverScraper) Config() *ScraperSettings                           { return &ScraperSettings{Enabled: true} }

func TestHistoryOperation_ImplementsInterfaces(t *testing.T) {
	var _ driver.Valuer = HistoryOpScrape
}

func TestHistoryStatus_ImplementsInterfaces(t *testing.T) {
	var _ driver.Valuer = HistoryStatusSuccess
}
