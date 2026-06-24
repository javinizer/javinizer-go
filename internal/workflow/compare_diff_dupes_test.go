package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestActressSlicesEqual_DuplicatesNotEqual verifies that count-based equality
// catches duplicate-mismatch that the old set-based check missed:
// a=[X, Y], b=[X, X] would falsely report equal with sets (both length 2,
// every key in b found in a's set), but differ as multisets.
func TestActressSlicesEqual_DuplicatesNotEqual(t *testing.T) {
	a := []models.Actress{
		{DMMID: 1},
		{DMMID: 2},
	}
	b := []models.Actress{
		{DMMID: 1},
		{DMMID: 1},
	}
	assert.False(t, actressSlicesEqual(a, b), "a=[dmm:1,dmm:2] != b=[dmm:1,dmm:1] as multisets")
}

func TestActressSlicesEqual_SameDuplicatesEqual(t *testing.T) {
	a := []models.Actress{{DMMID: 1}, {DMMID: 1}}
	b := []models.Actress{{DMMID: 1}, {DMMID: 1}}
	assert.True(t, actressSlicesEqual(a, b))
}

func TestGenreSlicesEqual_DuplicatesNotEqual(t *testing.T) {
	a := []models.Genre{{Name: "X"}, {Name: "Y"}}
	b := []models.Genre{{Name: "X"}, {Name: "X"}}
	assert.False(t, genreSlicesEqual(a, b), "a=[X,Y] != b=[X,X] as multisets")
}

func TestGenreSlicesEqual_SameDuplicatesEqual(t *testing.T) {
	a := []models.Genre{{Name: "X"}, {Name: "X"}}
	b := []models.Genre{{Name: "X"}, {Name: "X"}}
	assert.True(t, genreSlicesEqual(a, b))
}
