package database

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// actressSearchCondition matches names and partial decimal DMM identifiers.
const actressSearchCondition = "first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ? OR CAST(dmm_id AS TEXT) LIKE ?"

const missingActressThumbnailClause = "javinizer_missing_actress_thumbnail(COALESCE(thumb_url,'')) = 1"

// actressFilterClauses contains fixed SQL predicates, never caller-supplied SQL.
var actressFilterClauses = map[string]string{
	"missing_dmm":           "COALESCE(dmm_id, 0) <= 0",
	"has_dmm":               "COALESCE(dmm_id, 0) > 0",
	"missing_thumbnail":     missingActressThumbnailClause,
	"missing_japanese_name": "TRIM(COALESCE(japanese_name,'')) = ''",
	"japanese_name_only":    "TRIM(COALESCE(japanese_name,'')) <> '' AND TRIM(COALESCE(first_name,'')) = '' AND TRIM(COALESCE(last_name,'')) = ''",
	"missing_metadata":      "COALESCE(dmm_id, 0) > 0 AND (" + missingActressThumbnailClause + " OR TRIM(COALESCE(japanese_name,'')) = '' OR (TRIM(COALESCE(first_name,'')) = '' AND TRIM(COALESCE(last_name,'')) = ''))",
}

// ValidActressFilter returns the SQL predicate for a supported catalog filter.
func ValidActressFilter(filter string) (string, bool) {
	clause, ok := actressFilterClauses[strings.TrimSpace(filter)]
	return clause, ok
}

// resolveActressFilter maps a caller filter to its SQL clause. An empty
// filter means no constraint; an unknown non-empty filter is rejected rather
// than silently dropped — an unfiltered page would masquerade as a filtered
// one and mislead API callers.
func resolveActressFilter(filter string) (string, error) {
	if strings.TrimSpace(filter) == "" {
		return "", nil
	}
	clause, ok := ValidActressFilter(filter)
	if !ok {
		return "", wrapDBErr("filter", "actresses", ErrInvalidLookup)
	}
	return clause, nil
}

// ListFiltered returns a sorted page of verified actresses matching filter.
func (r *ActressRepository) ListFiltered(ctx context.Context, filter string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress
	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	clause, err := resolveActressFilter(filter)
	if err != nil {
		return nil, err
	}
	dbq := r.catalogQuery(ctx)
	if clause != "" {
		dbq = dbq.Where(clause)
	}
	for _, orderClause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(orderClause)
	}
	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("find", "actresses", err)
	}
	return actresses, nil
}

// SearchFiltered returns a sorted page matching both search and filter.
func (r *ActressRepository) SearchFiltered(ctx context.Context, query, filter string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress
	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	searchPattern := "%" + query + "%"
	clause, err := resolveActressFilter(filter)
	if err != nil {
		return nil, err
	}
	dbq := r.catalogQuery(ctx).Where(actressSearchCondition,
		searchPattern, searchPattern, searchPattern, searchPattern)
	if clause != "" {
		dbq = dbq.Where(clause)
	}
	for _, orderClause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(orderClause)
	}
	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

// CountFiltered counts verified actresses matching filter.
func (r *ActressRepository) CountFiltered(ctx context.Context, filter string) (int64, error) {
	var count int64
	clause, err := resolveActressFilter(filter)
	if err != nil {
		return 0, err
	}
	dbq := r.catalogQuery(ctx).Model(&models.Actress{})
	if clause != "" {
		dbq = dbq.Where(clause)
	}
	err = dbq.Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "actresses", err)
	}
	return count, nil
}

// CountSearchFiltered counts verified actresses matching both search and filter.
func (r *ActressRepository) CountSearchFiltered(ctx context.Context, query, filter string) (int64, error) {
	var count int64
	searchPattern := "%" + query + "%"
	dbq := r.catalogQuery(ctx).Model(&models.Actress{}).
		Where(actressSearchCondition,
			searchPattern, searchPattern, searchPattern, searchPattern)
	clause, err := resolveActressFilter(filter)
	if err != nil {
		return 0, err
	}
	if clause != "" {
		dbq = dbq.Where(clause)
	}
	err = dbq.Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "search actresses", err)
	}
	return count, nil
}
