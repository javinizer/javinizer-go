package scraperutil_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockScraper struct {
	name    string
	enabled bool
}

func (m *mockScraper) Name() string    { return m.name }
func (m *mockScraper) IsEnabled() bool { return m.enabled }
func (m *mockScraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *mockScraper) GetURL(_ context.Context, id string) (string, error) { return "", nil }
func (m *mockScraper) Config() *models.ScraperSettings                     { return &models.ScraperSettings{} }
func (m *mockScraper) Close() error                                        { return nil }

func TestRegistry_NewScraperRegistry_CreatesBothMaps(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	require.NotNil(t, reg)

	instances := reg.GetAllInstances()
	assert.NotNil(t, instances)
	assert.Len(t, instances, 0)

	regs := reg.GetAll()
	assert.NotNil(t, regs)
}

func TestRegistry_RegisterInstance_And_GetInstance(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	s, ok := reg.GetInstance("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, s)

	ms := &mockScraper{name: "test-scraper", enabled: true}
	reg.RegisterInstance(ms)

	s, ok = reg.GetInstance("test-scraper")
	assert.True(t, ok)
	assert.NotNil(t, s)
	assert.Equal(t, "test-scraper", s.Name())
}

func TestRegistry_GetAllInstances(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.RegisterInstance(&mockScraper{name: "c-scraper", enabled: true})
	reg.RegisterInstance(&mockScraper{name: "a-scraper", enabled: false})
	reg.RegisterInstance(&mockScraper{name: "b-scraper", enabled: true})

	all := reg.GetAllInstances()
	require.Len(t, all, 3)

	assert.Equal(t, "a-scraper", all[0].Name())
	assert.Equal(t, "b-scraper", all[1].Name())
	assert.Equal(t, "c-scraper", all[2].Name())
}

func TestRegistry_GetEnabledInstances(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.RegisterInstance(&mockScraper{name: "c-scraper", enabled: true})
	reg.RegisterInstance(&mockScraper{name: "a-scraper", enabled: false})
	reg.RegisterInstance(&mockScraper{name: "b-scraper", enabled: true})

	enabled := reg.GetEnabledInstances()
	require.Len(t, enabled, 2)

	assert.Equal(t, "b-scraper", enabled[0].Name())
	assert.Equal(t, "c-scraper", enabled[1].Name())
}

func TestRegistry_GetInstancesByPriority(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.RegisterInstance(&mockScraper{name: "a", enabled: true})
	reg.RegisterInstance(&mockScraper{name: "b", enabled: false})
	reg.RegisterInstance(&mockScraper{name: "c", enabled: true})

	t.Run("nil_priority_returns_all_enabled", func(t *testing.T) {
		result := reg.GetInstancesByPriority(nil)
		require.Len(t, result, 2)
		assert.Equal(t, "a", result[0].Name())
		assert.Equal(t, "c", result[1].Name())
	})

	t.Run("empty_priority_returns_all_enabled", func(t *testing.T) {
		result := reg.GetInstancesByPriority([]string{})
		require.Len(t, result, 2)
	})

	t.Run("explicit_priority_order", func(t *testing.T) {
		result := reg.GetInstancesByPriority([]string{"c", "a", "b"})
		require.Len(t, result, 2)

		assert.Equal(t, "c", result[0].Name())
		assert.Equal(t, "a", result[1].Name())
	})
}

type queryResolverScraper struct {
	mockScraper
	resolveQuery string
	resolveMatch bool
}

func (q *queryResolverScraper) ResolveSearchQuery(input string) (string, bool) {
	return q.resolveQuery, q.resolveMatch
}

func TestRegistry_GetInstancesByPriorityForInput(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.RegisterInstance(&mockScraper{name: "a", enabled: true})
	reg.RegisterInstance(&queryResolverScraper{
		mockScraper:  mockScraper{name: "b", enabled: true},
		resolveQuery: "resolved-b", resolveMatch: true,
	})
	reg.RegisterInstance(&mockScraper{name: "c", enabled: true})

	t.Run("input_matches_resolver_moves_to_front", func(t *testing.T) {
		result := reg.GetInstancesByPriorityForInput([]string{"a", "b", "c"}, "some-input")
		require.Len(t, result, 3)

		assert.Equal(t, "b", result[0].Name())
	})

	t.Run("empty_input_no_reorder", func(t *testing.T) {
		result := reg.GetInstancesByPriorityForInput([]string{"a", "b", "c"}, "")
		require.Len(t, result, 3)

		assert.Equal(t, "a", result[0].Name())
	})

	t.Run("no_matching_resolver_original_order", func(t *testing.T) {
		reg2 := scraperutil.NewScraperRegistry()
		reg2.RegisterInstance(&mockScraper{name: "a", enabled: true})
		reg2.RegisterInstance(&mockScraper{name: "c", enabled: true})

		result := reg2.GetInstancesByPriorityForInput([]string{"a", "c"}, "some-input")
		require.Len(t, result, 2)
		assert.Equal(t, "a", result[0].Name())
	})
}

func TestRegistry_RegisterInstance_NilGuard(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.RegisterInstance(nil)

	assert.Len(t, reg.GetAllInstances(), 0)
}

func TestRegistry_InitInstances_ValidConfig(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.Register(scraperutil.ScraperRegistration{
		Name:        "test-scraper",
		Description: "Test scraper",
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &mockScraper{name: "test-scraper", enabled: true}, nil
		},
		Priority: 50,
	})

	err := reg.InitInstances(map[string]scraperutil.ScraperDeps{
		"test-scraper": {Settings: models.ScraperSettings{}},
	})
	require.NoError(t, err)

	instances := reg.GetAllInstances()
	require.Len(t, instances, 1)
	assert.Equal(t, "test-scraper", instances[0].Name())
}

func TestRegistry_InitInstances_NilDepsMap(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	err := reg.InitInstances(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "depsMap cannot be nil")
}

func TestRegistry_InitInstances_NilConstructor(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.Register(scraperutil.ScraperRegistration{
		Name:        "nil-constructor-scraper",
		Description: "Nil constructor",
		Constructor: nil,
		Priority:    50,
	})

	err := reg.InitInstances(map[string]scraperutil.ScraperDeps{
		"nil-constructor-scraper": {Settings: models.ScraperSettings{}},
	})
	require.NoError(t, err)

	assert.Len(t, reg.GetAllInstances(), 0)
}

func TestRegistry_InitInstances_MissingOverride(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.Register(scraperutil.ScraperRegistration{
		Name: "missing-override-scraper",
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			t.Fatal("constructor should not be called when override is missing")
			return nil, nil
		},
		Priority: 50,
	})

	err := reg.InitInstances(map[string]scraperutil.ScraperDeps{
		"other-scraper": {Settings: models.ScraperSettings{}},
	})
	require.NoError(t, err)
	assert.Len(t, reg.GetAllInstances(), 0)
}

func TestRegistry_InitInstances_ConstructorError(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.Register(scraperutil.ScraperRegistration{
		Name: "failing-scraper",
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return nil, fmt.Errorf("intentional constructor failure")
		},
		Priority: 50,
	})
	reg.Register(scraperutil.ScraperRegistration{
		Name: "good-scraper",
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &mockScraper{name: "good-scraper", enabled: true}, nil
		},
		Priority: 50,
	})

	err := reg.InitInstances(map[string]scraperutil.ScraperDeps{
		"failing-scraper": {Settings: models.ScraperSettings{}},
		"good-scraper":    {Settings: models.ScraperSettings{}},
	})
	require.NoError(t, err)

	instances := reg.GetAllInstances()
	require.Len(t, instances, 1)
	assert.Equal(t, "good-scraper", instances[0].Name())
}

func TestRegistry_InitInstances_NilReturn(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.Register(scraperutil.ScraperRegistration{
		Name: "nil-return-scraper",
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return nil, nil
		},
		Priority: 50,
	})

	err := reg.InitInstances(map[string]scraperutil.ScraperDeps{
		"nil-return-scraper": {Settings: models.ScraperSettings{}},
	})
	require.NoError(t, err)
	assert.Len(t, reg.GetAllInstances(), 0)
}

func TestRegistry_ThreadSafety_Race(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.RegisterInstance(&mockScraper{name: "a", enabled: true})

	var wg sync.WaitGroup
	const numGoroutines = 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = reg.GetInstance("a")
			_ = reg.GetAllInstances()
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("scraper-%d", idx)
			reg.RegisterInstance(&mockScraper{name: name, enabled: true})
		}(i)
	}

	wg.Wait()
}
