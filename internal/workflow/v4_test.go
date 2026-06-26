package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/assert"
)

func TestIdentifyDifferencesV4(t *testing.T) {
	old := &models.Movie{ID: "ABC-123", Title: "Old Title"}
	new := &models.Movie{ID: "ABC-123", Title: "New Title"}
	merged := &models.Movie{ID: "ABC-123", Title: "Merged Title"}

	diff := identifyDifferences(old, new, merged)
	assert.NotNil(t, diff)
}

func TestIdentifyDifferencesEqualV4(t *testing.T) {
	same := &models.Movie{ID: "ABC-123", Title: "Same Title"}
	diff := identifyDifferences(same, same, same)
	assert.NotNil(t, diff)
}

func TestTrimPreviewPathV4(t *testing.T) {
	result := organizer.TrimTrailingSeparators("/very/long/path/that/exceeds/limit")
	assert.NotEmpty(t, result)
}

func TestFormatTimePtrV4(t *testing.T) {
	assert.Equal(t, "<nil>", formatTimePtr(nil))
}

func TestResolveOperationModeV4(t *testing.T) {
	_, err := ResolveOperationMode("organize")
	assert.NoError(t, err)
	_, err = ResolveOperationMode("in-place")
	assert.NoError(t, err)
	_, err = ResolveOperationMode("preview")
	assert.NoError(t, err)
	_, err = ResolveOperationMode("invalid")
	assert.Error(t, err)
}

func TestResolveLinkModeV4(t *testing.T) {
	_, err := ResolveLinkMode("none")
	assert.NoError(t, err)
	_, err = ResolveLinkMode("hard")
	assert.NoError(t, err)
	_, err = ResolveLinkMode("soft")
	assert.NoError(t, err)
	_, err = ResolveLinkMode("invalid")
	assert.Error(t, err)
}

func TestResolvePresetV4(t *testing.T) {
	_, err := ResolvePreset("conservative")
	assert.NoError(t, err)
	_, err = ResolvePreset("gap-fill")
	assert.NoError(t, err)
	_, err = ResolvePreset("aggressive")
	assert.NoError(t, err)
	_, err = ResolvePreset("invalid")
	assert.Error(t, err)
}

func TestActressKeyV4(t *testing.T) {
	a := models.Actress{JapaneseName: "Test Actress"}
	key := actressKey(a)
	assert.NotEmpty(t, key)
}

func TestActressSlicesEqualV4(t *testing.T) {
	a := []models.Actress{{JapaneseName: "Actress A"}}
	b := []models.Actress{{JapaneseName: "Actress A"}}
	assert.True(t, actressSlicesEqual(a, b))

	c := []models.Actress{{JapaneseName: "Actress B"}}
	assert.False(t, actressSlicesEqual(a, c))
}

func TestGenreSlicesEqualV4(t *testing.T) {
	a := []models.Genre{{Name: "Drama"}}
	b := []models.Genre{{Name: "Drama"}}
	assert.True(t, genreSlicesEqual(a, b))

	c := []models.Genre{{Name: "Action"}}
	assert.False(t, genreSlicesEqual(a, c))
}
