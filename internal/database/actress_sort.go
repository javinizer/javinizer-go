package database

import (
	"fmt"
	"strings"
)

func normalizeActressSort(sortBy, sortOrder string) (string, string, error) {
	sortBy = strings.TrimSpace(strings.ToLower(sortBy))
	sortOrder = strings.TrimSpace(strings.ToLower(sortOrder))

	if sortOrder != "desc" {
		sortOrder = "asc"
	}

	switch sortBy {
	case "id", "dmm_id", "japanese_name", "first_name", "last_name", "created_at", "updated_at":
		return sortBy, sortOrder, nil
	case "name", "":
		return "name", sortOrder, nil
	default:
		return "", "", fmt.Errorf("invalid sort_by value: %q", sortBy)
	}
}

// actressOrderClauses returns GORM order clauses for actress sorting.
// Builds multi-column ORDER BY clauses with consistent tiebreaking on id.
func actressOrderClauses(sortBy, sortOrder string) []string {
	switch sortBy {
	case "id":
		return []string{"id " + sortOrder}
	case "dmm_id":
		return []string{"dmm_id " + sortOrder, "id " + sortOrder}
	case "japanese_name":
		return []string{"japanese_name " + sortOrder, "id " + sortOrder}
	case "first_name":
		return []string{"first_name " + sortOrder, "last_name " + sortOrder, "id " + sortOrder}
	case "last_name":
		return []string{"last_name " + sortOrder, "first_name " + sortOrder, "id " + sortOrder}
	case "created_at":
		return []string{"created_at " + sortOrder, "id " + sortOrder}
	case "updated_at":
		return []string{"updated_at " + sortOrder, "id " + sortOrder}
	default:
		return []string{"last_name " + sortOrder, "first_name " + sortOrder, "japanese_name " + sortOrder, "id " + sortOrder}
	}
}
