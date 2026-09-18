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
	case "id", colDMMID, colJapaneseName, colFirstName, colLastName, "created_at", colUpdatedAt:
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
	case colDMMID:
		return []string{"dmm_id " + sortOrder, "id " + sortOrder}
	case colJapaneseName:
		return []string{"japanese_name " + sortOrder, "id " + sortOrder}
	case colFirstName:
		return []string{"first_name " + sortOrder, "last_name " + sortOrder, "id " + sortOrder}
	case colLastName:
		return []string{"last_name " + sortOrder, "first_name " + sortOrder, "id " + sortOrder}
	case "created_at":
		return []string{"created_at " + sortOrder, "id " + sortOrder}
	case colUpdatedAt:
		return []string{"updated_at " + sortOrder, "id " + sortOrder}
	default:
		return []string{"last_name " + sortOrder, "first_name " + sortOrder, "japanese_name " + sortOrder, "id " + sortOrder}
	}
}
