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

func TestClampResult_DifferentOriginalTitle(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC", Title: "LongTitle", OriginalTitle: "DifferentLongOriginalTitle"}
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE> (<ORIGINALTITLE>)", ctx, 30)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 30)
}

func TestClampResult_TranslationWithLongOriginalTitle(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		ID:    "ABC",
		Title: "Short",
		Translations: map[string]models.MovieTranslation{
			"en": {Title: "Short", OriginalTitle: "Very Long Original Translated Title For Coverage"},
		},
	}
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE:en> (<ORIGINALTITLE:en>)", ctx, 30)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 30)
}

func TestClampResult_HardTruncationLastResort(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890", Title: ""}
	got, err := e.ExecuteWithMaxBytes("<ID>", ctx, 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 5)
	assert.Equal(t, "ABCDE", got, "should be first 5 chars of ID")
}

func TestExecuteWithMaxBytes_DifferentOriginalTitleTruncation(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "VeryLongTitleThatExceedsBudget", OriginalTitle: "DifferentLongOriginalTitle"}
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE> (<ORIGINALTITLE>)", ctx, 20)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 20)
}

func TestExecuteWithMaxBytes_PropagateError_TitleBudgetPath(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 16})
	ctx := &Context{ID: "ABC", Title: "VeryLongTitle"}
	// Sentinel "ABC-\x00MAXBYTES\x00" = 16 bytes, 16 > 16 = false → passes
	// frameBytes = 4, titleBudget = 3 - 4 = -1 ≤ 0 → titleBudget path
	// Execute "ABC-VeryLongTitle" = 17 > 16 → error!
	_, err := e.ExecuteWithMaxBytes("<ID>-<TITLE>", ctx, 3)
	assert.Error(t, err)
}
