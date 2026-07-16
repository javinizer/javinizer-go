package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubScraper struct {
	name    string
	enabled bool
	result  *models.ScraperResult
	err     error
	panic   interface{}
}

func (s *stubScraper) Name() string { return s.name }
func (s *stubScraper) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	if s.panic != nil {
		panic(s.panic)
	}
	if s.err != nil {
		return nil, s.err
	}
	r := *s.result
	r.ID = id
	return &r, nil
}
func (s *stubScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubScraper) IsEnabled() bool                                    { return s.enabled }
func (s *stubScraper) Close() error                                       { return nil }
func (s *stubScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: s.enabled}
}

func newTestEngine(t *testing.T, scraper models.Scraper) *scrape.Scraper {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(scraper)
	return scrape.NewQueryOnly(reg)
}

func TestQueryRaw_ReturnsUnmergedResult(t *testing.T) {
	expected := &models.ScraperResult{Source: "test", Title: "Test Movie"}
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, result: expected})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Test Movie", result.Title)
	assert.Equal(t, "TEST-001", result.ID)
}

func TestQueryRaw_PreservesNotFoundError(t *testing.T) {
	scraperErr := models.NewScraperNotFoundError("test", "not found")
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, err: scraperErr})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindNotFound, err.Kind)
	assert.Equal(t, 0, err.StatusCode)
	assert.False(t, err.Retryable)
	assert.False(t, err.Temporary)
}

func TestQueryRaw_PreservesBlockedError(t *testing.T) {
	scraperErr := models.NewScraperStatusError("test", 403, "forbidden")
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, err: scraperErr})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindBlocked, err.Kind)
	assert.Equal(t, 403, err.StatusCode)
	assert.False(t, err.Retryable)
}

func TestQueryRaw_PreservesTimeoutError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, result: &models.ScraperResult{}})
	result, err := engine.QueryRaw(ctx, "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindUnavailable, err.Kind)
	assert.True(t, err.Retryable)
	assert.True(t, err.Temporary)
}

func TestQueryRaw_PreservesPanic(t *testing.T) {
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, panic: "boom"})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindUnknown, err.Kind)
	assert.Contains(t, err.Message, "boom")
}

func TestQueryRaw_UnknownScraper(t *testing.T) {
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, result: &models.ScraperResult{}})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "nonexistent")
	require.Nil(t, result)
	require.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindUnknown, err.Kind)
	assert.Contains(t, err.Message, "not registered")
}

func TestScraperErrorToEnvelope(t *testing.T) {
	t.Run("nil returns unknown", func(t *testing.T) {
		e := scraperErrorToEnvelope(nil)
		assert.Equal(t, "unknown", e.Kind)
	})
	t.Run("not_found", func(t *testing.T) {
		e := scraperErrorToEnvelope(models.NewScraperNotFoundError("test", "not found"))
		assert.Equal(t, "not_found", e.Kind)
		assert.Equal(t, 0, e.StatusCode)
		assert.False(t, e.Retryable)
	})
	t.Run("blocked", func(t *testing.T) {
		e := scraperErrorToEnvelope(models.NewScraperStatusError("test", 403, "forbidden"))
		assert.Equal(t, "blocked", e.Kind)
		assert.Equal(t, 403, e.StatusCode)
	})
	t.Run("uses Error() when Message empty", func(t *testing.T) {
		e := scraperErrorToEnvelope(&models.ScraperError{Kind: models.ScraperErrorKindUnknown, StatusCode: 500, Scraper: "test"})
		assert.NotEmpty(t, e.Message)
	})
}

func TestValidation_InvalidOutput(t *testing.T) {
	cmd := NewCommand()
	cmd.SetArgs([]string{"TEST-001", "--output", "xml"})
	var errMsg string
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		if output != "text" && output != "json" && output != "" {
			return fmt.Errorf("invalid output value: must be 'text' or 'json'")
		}
		return nil
	}
	err := cmd.Execute()
	require.Error(t, err)
	errMsg = err.Error()
	assert.Contains(t, errMsg, "invalid output value")
}

func TestValidation_JSONRequiresSingleScraper(t *testing.T) {
	t.Run("no scrapers", func(t *testing.T) {
		scrapersFlag := []string{}
		err := validateJSONMode(scrapersFlag, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one scraper")
	})
	t.Run("two scrapers", func(t *testing.T) {
		scrapersFlag := []string{"r18dev", "dmm"}
		err := validateJSONMode(scrapersFlag, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one scraper")
	})
	t.Run("valid single scraper", func(t *testing.T) {
		err := validateJSONMode([]string{"r18dev"}, false)
		require.NoError(t, err)
	})
}

func TestValidation_JSONRejectsForce(t *testing.T) {
	err := validateJSONMode([]string{"r18dev"}, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
}

func TestJSONOutput_Success(t *testing.T) {
	expected := &models.ScraperResult{Source: "test", Title: "Test Movie", ID: "TEST-001"}
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, result: expected})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, err)
	require.NotNil(t, result)
	data, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)
	var parsed models.ScraperResult
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "Test Movie", parsed.Title)
	assert.Equal(t, "test", parsed.Source)
}

func TestJSONOutput_NotFoundError(t *testing.T) {
	scraperErr := models.NewScraperNotFoundError("test", "not found")
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, err: scraperErr})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	envelope := scraperErrorToEnvelope(err)
	assert.Equal(t, "not_found", envelope.Kind)
	assert.Equal(t, 0, envelope.StatusCode)
	assert.False(t, envelope.Retryable)
}

func TestJSONOutput_BlockedError(t *testing.T) {
	scraperErr := models.NewScraperStatusError("test", 403, "forbidden")
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, err: scraperErr})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	envelope := scraperErrorToEnvelope(err)
	assert.Equal(t, "blocked", envelope.Kind)
	assert.Equal(t, 403, envelope.StatusCode)
}

func TestJSONOutput_TimeoutError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, result: &models.ScraperResult{}})
	result, err := engine.QueryRaw(ctx, "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	envelope := scraperErrorToEnvelope(err)
	assert.Equal(t, "unavailable", envelope.Kind)
	assert.True(t, envelope.Retryable)
	assert.True(t, envelope.Temporary)
}

func TestJSONOutput_PanicError(t *testing.T) {
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, panic: "boom"})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	envelope := scraperErrorToEnvelope(err)
	assert.Equal(t, "unknown", envelope.Kind)
	assert.False(t, envelope.Retryable)
	assert.False(t, envelope.Temporary)
	assert.Contains(t, envelope.Message, "boom")
}

func TestJSONOutput_UnknownScraper(t *testing.T) {
	engine := newTestEngine(t, &stubScraper{name: "test", enabled: true, result: &models.ScraperResult{}})
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "nonexistent")
	require.Nil(t, result)
	require.NotNil(t, err)
	envelope := scraperErrorToEnvelope(err)
	assert.Equal(t, "unknown", envelope.Kind)
	assert.Contains(t, envelope.Message, "not registered")
}

func TestJSONOutput_StdoutContainsOnlyJSON(t *testing.T) {
	expected := &models.ScraperResult{Source: "test", Title: "Test"}
	data, _ := json.Marshal(expected)
	s := string(data)
	assert.True(t, strings.HasPrefix(s, "{"))
	assert.True(t, strings.HasSuffix(s, "}"))
	var parsed models.ScraperResult
	require.NoError(t, json.Unmarshal(data, &parsed))
}
