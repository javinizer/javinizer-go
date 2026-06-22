package database

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// filterIdentifiableActresses removes actresses from the list that have no
// identifying information (no DMMID, no JapaneseName, no FirstName, no LastName).
// This prevents zero-value placeholders from being persisted as real actress records.
func filterIdentifiableActresses(actresses []models.Actress) []models.Actress {
	if len(actresses) == 0 {
		return actresses
	}

	filtered := make([]models.Actress, 0, len(actresses))
	for _, actress := range actresses {
		if actress.DMMID != 0 ||
			strings.TrimSpace(actress.JapaneseName) != "" ||
			strings.TrimSpace(actress.FirstName) != "" ||
			strings.TrimSpace(actress.LastName) != "" {
			filtered = append(filtered, actress)
		}
	}

	return filtered
}
