package scraperutil

import (
	"sort"
	"sync"

	"github.com/javinizer/javinizer-go/internal/models"
)

// registrationCatalog holds write-once scraper registration metadata.
// It is populated during init() by scraper plugins and read thereafter.
// All methods are thread-safe via its own sync.RWMutex.
type registrationCatalog struct {
	mu       sync.RWMutex
	scrapers map[string]ScraperRegistration
}

// newregistrationCatalog creates a new registrationCatalog with an empty registry.
func newregistrationCatalog() *registrationCatalog {
	return &registrationCatalog{
		scrapers: make(map[string]ScraperRegistration),
	}
}

// Register adds a scraper registration to the catalog.
func (c *registrationCatalog) Register(reg ScraperRegistration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scrapers[reg.Name] = reg
}

// Get retrieves a scraper registration by name.
func (c *registrationCatalog) Get(name string) (ScraperRegistration, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	reg, ok := c.scrapers[name]
	return reg, ok
}

// GetAll returns a copy of the catalog map.
// Options and Defaults are cloned to prevent mutations from affecting the original.
func (c *registrationCatalog) GetAll() map[string]ScraperRegistration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]ScraperRegistration, len(c.scrapers))
	for k, v := range c.scrapers {
		v.Options = cloneScraperOptions(v.Options)
		v.Defaults = v.Defaults.Clone()
		result[k] = v
	}
	return result
}

// cloneScraperOptions returns a deep copy of a scraper-options slice. A plain
// append([]ScraperOption{}, opts...) copies only the top-level slice header:
// nested Choices slices and the Min/Max pointers are shared with the source,
// so a caller mutating a returned choice or a *int would corrupt the
// registered defaults. This copies Choices element-wise and clones the
// Min/Max pointers so the returned options are fully independent.
func cloneScraperOptions(options []models.ScraperOption) []models.ScraperOption {
	if options == nil {
		return nil
	}
	cloned := make([]models.ScraperOption, len(options))
	for i := range options {
		cloned[i] = options[i]
		if options[i].Min != nil {
			min := *options[i].Min
			cloned[i].Min = &min
		}
		if options[i].Max != nil {
			max := *options[i].Max
			cloned[i].Max = &max
		}
		if options[i].Choices != nil {
			cloned[i].Choices = append([]models.ScraperChoice(nil), options[i].Choices...)
		}
	}
	return cloned
}

// Names returns all registered scraper names in arbitrary order.
func (c *registrationCatalog) Names() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.scrapers))
	for name := range c.scrapers {
		names = append(names, name)
	}
	return names
}

// Priorities returns scraper names sorted by priority (highest first),
// with ties broken alphabetically by name.
func (c *registrationCatalog) Priorities() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.scrapers) == 0 {
		return nil
	}

	type pair struct {
		name     string
		priority int
	}
	pairs := make([]pair, 0, len(c.scrapers))
	for name, reg := range c.scrapers {
		pairs = append(pairs, pair{name: name, priority: reg.Priority})
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[j].priority != pairs[i].priority {
			return pairs[j].priority < pairs[i].priority
		}
		return pairs[i].name < pairs[j].name
	})

	result := make([]string, len(pairs))
	for i, p := range pairs {
		result[i] = p.name
	}
	return result
}

// IsRegistered reports whether a scraper with the given name is registered.
func (c *registrationCatalog) IsRegistered(name string) bool {
	_, ok := c.Get(name)
	return ok
}

// GetOptions returns scraper options for the named scraper.
func (c *registrationCatalog) GetOptions(name string) (ScraperOptionsProvider, bool) {
	reg, ok := c.Get(name)
	if !ok {
		return ScraperOptionsProvider{}, false
	}
	return ScraperOptionsProvider{
		DisplayTitle: reg.Description,
		Options:      reg.Options,
	}, true
}

// GetScraperConstructors returns a map of all non-nil scraper constructors.
func (c *registrationCatalog) GetScraperConstructors() map[string]ScraperConstructor {
	all := c.GetAll()
	result := make(map[string]ScraperConstructor, len(all))
	for name, reg := range all {
		if reg.Constructor != nil {
			result[name] = reg.Constructor
		}
	}
	return result
}

// GetScraperConstructor returns the constructor for the named scraper.
func (c *registrationCatalog) GetScraperConstructor(name string) (ScraperConstructor, bool) {
	reg, ok := c.Get(name)
	if !ok {
		return nil, false
	}
	if reg.Constructor != nil {
		return reg.Constructor, true
	}
	return nil, false
}
