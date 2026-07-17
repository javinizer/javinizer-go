package commandutil

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/models"
)

type memoryContentIDRepository struct {
	mu       sync.RWMutex
	mappings map[string]models.ContentIDMapping
}

func newMemoryContentIDRepository() models.ContentIDMappingRepositoryInterface {
	return &memoryContentIDRepository{mappings: make(map[string]models.ContentIDMapping)}
}

func (r *memoryContentIDRepository) FindBySearchID(_ context.Context, searchID string) (*models.ContentIDMapping, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mapping, ok := r.mappings[strings.ToUpper(searchID)]
	if !ok {
		return nil, fmt.Errorf("content ID mapping not found")
	}
	copied := mapping
	return &copied, nil
}

func (r *memoryContentIDRepository) Create(_ context.Context, mapping *models.ContentIDMapping) error {
	if mapping == nil {
		return fmt.Errorf("content ID mapping cannot be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *mapping
	copied.SearchID = strings.ToUpper(copied.SearchID)
	r.mappings[copied.SearchID] = copied
	return nil
}

func (r *memoryContentIDRepository) Delete(_ context.Context, searchID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mappings, strings.ToUpper(searchID))
	return nil
}

func (r *memoryContentIDRepository) GetAllPaginated(ctx context.Context, limit, offset int) ([]models.ContentIDMapping, error) {
	all, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	if offset >= len(all) {
		return []models.ContentIDMapping{}, nil
	}
	end := len(all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return all[offset:end], nil
}

func (r *memoryContentIDRepository) GetAll(ctx context.Context) ([]models.ContentIDMapping, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.mappings))
	for key := range r.mappings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]models.ContentIDMapping, 0, len(keys))
	for _, key := range keys {
		result = append(result, r.mappings[key])
	}
	return result, nil
}

func (r *memoryContentIDRepository) GetAllChunked(ctx context.Context, _ int) ([]models.ContentIDMapping, error) {
	return r.GetAll(ctx)
}
