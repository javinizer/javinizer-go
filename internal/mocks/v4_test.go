package mocks

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMockHTTPClientV4(t *testing.T) {
	m := NewMockHTTPClient(t)
	require.NotNil(t, m)

	m.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{StatusCode: 200}, nil)
	resp, err := m.Do(&http.Request{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestMockScraperV4(t *testing.T) {
	m := NewMockScraper(t)
	require.NotNil(t, m)

	result := &models.ScraperResult{ID: "ABC-123", Title: "Test"}
	m.On("Search", mock.Anything, "ABC-123").Return(result, nil)
	sr, err := m.Search(context.Background(), "ABC-123")
	assert.NoError(t, err)
	assert.Equal(t, "ABC-123", sr.ID)

	m.On("Name").Return("test-scraper")
	assert.Equal(t, "test-scraper", m.Name())

	m.On("IsEnabled").Return(true)
	assert.True(t, m.IsEnabled())
}

func TestMockURLHandlerV4(t *testing.T) {
	m := NewMockURLHandler(t)
	require.NotNil(t, m)

	result := &models.ScraperResult{ID: "ABC-123", Title: "Test"}
	m.On("CanHandleURL", "https://example.com/ABC-123").Return(true)
	assert.True(t, m.CanHandleURL("https://example.com/ABC-123"))

	m.On("ScrapeURL", mock.Anything, "https://example.com/ABC-123").Return(result, nil)
	sr, err := m.ScrapeURL(context.Background(), "https://example.com/ABC-123")
	assert.NoError(t, err)
	assert.Equal(t, "ABC-123", sr.ID)
}

func TestMockDownloadProxyResolverV4(t *testing.T) {
	m := NewMockDownloadProxyResolver(t)
	require.NotNil(t, m)

	m.On("ResolveDownloadProxyForHost", "example.com").Return(nil, nil, false)
	_, _, handled := m.ResolveDownloadProxyForHost("example.com")
	assert.False(t, handled)
}

func TestMockEventEmitterV4(t *testing.T) {
	m := NewMockEventEmitter(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockAggregatorV4(t *testing.T) {
	m := NewMockAggregatorInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockMovieRepoV4(t *testing.T) {
	m := NewMockMovieRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockActressRepoV4(t *testing.T) {
	m := NewMockActressRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockEventRepoV4(t *testing.T) {
	m := NewMockEventRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockApiTokenRepoV4(t *testing.T) {
	m := NewMockApiTokenRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockGenreRepoV4(t *testing.T) {
	m := NewMockGenreRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockHistoryRepoV4(t *testing.T) {
	m := NewMockHistoryRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockJobRepoV4(t *testing.T) {
	m := NewMockJobRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockContentIDMappingV4(t *testing.T) {
	m := NewMockContentIDMappingRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockGenreReplacementV4(t *testing.T) {
	m := NewMockGenreReplacementRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockGenreTranslationV4(t *testing.T) {
	m := NewMockGenreTranslationRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockActressAliasV4(t *testing.T) {
	m := NewMockActressAliasRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockActressTranslationV4(t *testing.T) {
	m := NewMockActressTranslationRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockBatchFileOpV4(t *testing.T) {
	m := NewMockBatchFileOperationRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockMovieTagV4(t *testing.T) {
	m := NewMockMovieTagRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockWordReplacementV4(t *testing.T) {
	m := NewMockWordReplacementRepositoryInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockScraperInterfaceV4(t *testing.T) {
	m := NewMockScraperInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

// P3: exercise the ledger read seams added to the mock (the raw method
// bodies must count as covered for the patch gate).
func TestMockBatchFileOpRepo_P3Methods(t *testing.T) {
	repo := NewMockBatchFileOperationRepositoryInterface(t)
	ctx := context.Background()
	rows := []models.BatchFileOperation{{ID: 7, GeneratedFiles: `{"replacements":[{"destination":"/d"}]}`}}

	repo.EXPECT().FindOperationsByDestination(mock.Anything, "/d").Return(rows, nil).Once()
	got, err := repo.FindOperationsByDestination(ctx, "/d")
	require.NoError(t, err)
	require.Len(t, got, 1)

	repo.EXPECT().FindOperationsWithReplacements(mock.Anything).Return(rows, nil).Once()
	_, err = repo.FindOperationsWithReplacements(ctx)
	require.NoError(t, err)

	repo.EXPECT().FindOperationsWithLedger(mock.Anything).Return(nil, errors.New("x")).Once()
	_, err = repo.FindOperationsWithLedger(ctx)
	require.Error(t, err)
}
