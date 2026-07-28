package template

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClampResult_TranslationTruncation(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		ID:    "ABC",
		Title: "Short",
		Translations: map[string]models.MovieTranslation{
			"en": {Title: "Very Long Translated Title That Exceeds The Budget"},
		},
	}
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE:en>", ctx, 20)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 20, "result must not exceed maxBytes")
	assert.Contains(t, got, "ABC -", "ID prefix should be preserved")
}

func TestClampResult_HardTruncationLastResort(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890", Title: ""}
	got, err := e.ExecuteWithMaxBytes("<ID>", ctx, 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 5)
	assert.Equal(t, "ABCDE", got, "should be first 5 chars of ID")
}
