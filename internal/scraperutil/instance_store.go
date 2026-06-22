package scraperutil

import (
	"sort"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/models"
)

// instanceStore holds runtime scraper instances.
// It is populated at startup by InitInstances and read thereafter.
// All methods are thread-safe via its own sync.RWMutex.
type instanceStore struct {
	mu        sync.RWMutex
	instances map[string]models.Scraper
}

// newInstanceStore creates a new instanceStore with an empty instance map.
func newInstanceStore() *instanceStore {
	return &instanceStore{
		instances: make(map[string]models.Scraper),
	}
}

// RegisterInstance adds a scraper instance to the store.
// Passing nil is a safe no-op.
func (s *instanceStore) RegisterInstance(scraper models.Scraper) {
	if scraper == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[scraper.Name()] = scraper
}

// GetInstance retrieves a scraper instance by name.
// Returns (nil, false) if the name is not found.
func (s *instanceStore) GetInstance(name string) (models.Scraper, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	scraper, exists := s.instances[name]
	return scraper, exists
}

// GetAllInstances returns all registered scraper instances in sorted key order
// for deterministic iteration. Nil entries are skipped.
func (s *instanceStore) GetAllInstances() []models.Scraper {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.instances))
	for k := range s.instances {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	scrapers := make([]models.Scraper, 0, len(s.instances))
	for _, k := range keys {
		if s.instances[k] != nil {
			scrapers = append(scrapers, s.instances[k])
		}
	}
	return scrapers
}

// GetEnabledInstances returns all enabled scraper instances in sorted key order
// for deterministic iteration.
func (s *instanceStore) GetEnabledInstances() []models.Scraper {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.instances))
	for k := range s.instances {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	scrapers := make([]models.Scraper, 0, len(s.instances))
	for _, k := range keys {
		scraper := s.instances[k]
		if scraper != nil && scraper.IsEnabled() {
			scrapers = append(scrapers, scraper)
		}
	}
	return scrapers
}

// GetInstancesByPriority returns enabled scraper instances in the specified priority order.
// If the priority list is empty or nil, returns all enabled instances in sorted key order.
// Only returns instances that are both in the priority list AND enabled.
func (s *instanceStore) GetInstancesByPriority(priority []string) []models.Scraper {
	if len(priority) == 0 {
		return s.GetEnabledInstances()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	scrapers := make([]models.Scraper, 0, len(s.instances))

	for _, name := range priority {
		if scraper, exists := s.instances[name]; exists {
			if scraper != nil && scraper.IsEnabled() {
				scrapers = append(scrapers, scraper)
			}
		}
	}

	return scrapers
}

// GetInstancesByPriorityForInput returns enabled instances in priority order, but moves
// instances with matching query resolvers to the front for the provided input.
//
// If no instance resolver matches, the original GetInstancesByPriority ordering is
// returned unchanged.
func (s *instanceStore) GetInstancesByPriorityForInput(priority []string, input string) []models.Scraper {
	scrapers := s.GetInstancesByPriority(priority)
	input = strings.TrimSpace(input)
	if input == "" || len(scrapers) == 0 {
		return scrapers
	}

	matching := make([]models.Scraper, 0, len(scrapers))
	nonMatching := make([]models.Scraper, 0, len(scrapers))

	for _, scraper := range scrapers {
		if _, ok := models.ResolveSearchQueryForScraper(scraper, input); ok {
			matching = append(matching, scraper)
			continue
		}
		nonMatching = append(nonMatching, scraper)
	}

	if len(matching) == 0 {
		return scrapers
	}

	return append(matching, nonMatching...)
}
