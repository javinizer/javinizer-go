package batch

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"testing"
)

func TestFinalCachedActressEqualityDoesNotHideAnExplicitEdit(t *testing.T) {
	baseline := &models.Movie{Actresses: []models.Actress{{ID: 7, FirstName: "Yui", Verified: true}}}
	for _, tc := range []struct {
		name     string
		payload  *models.Movie
		preserve bool
	}{
		{"nil payload", nil, false},
		{"omitted cast", &models.Movie{}, true},
		{"identical cast", &models.Movie{Actresses: []models.Actress{{ID: 7, FirstName: "Yui", Verified: true}}}, true},
		{"different length", &models.Movie{Actresses: []models.Actress{}}, false},
		{"explicit rename", &models.Movie{Actresses: []models.Actress{{ID: 7, FirstName: "Changed"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPreserveCachedActresses(tc.payload, baseline); got != tc.preserve {
				t.Fatalf("preserve=%v, want %v", got, tc.preserve)
			}
		})
	}
	if shouldPreserveCachedActresses(&models.Movie{Actresses: []models.Actress{{ID: 7}}}, nil) {
		t.Fatal("no baseline must not preserve")
	}
}
