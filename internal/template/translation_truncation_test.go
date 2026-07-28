package template

import (
	"strings"
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
	_, err := e.ExecuteWithMaxBytes("<ID>", ctx, 5)
	require.Error(t, err, "should error when ID alone exceeds maxBytes")
}

func TestExecuteWithMaxBytes_PropagateError_TruncatedTitlePath(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 16})
	longTitle := strings.Repeat("A", 97)
	ctx := &Context{ID: "ABC", Title: longTitle}
	// Sentinel = 16 ≤ 16 → passes. frameBytes=4. titleBudget=100-4=96.
	// titleBytes=97 > 96 → truncation path.
	// TruncateTitleBytes(97-byte-title, 96) = 96 bytes. Execute = 100 > 16 → error!
	_, err := e.ExecuteWithMaxBytes("<ID>-<TITLE>", ctx, 100)
	assert.Error(t, err)
}

func TestExecuteWithMaxBytes_TruncationDifferentOriginalTitle(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "VeryLongTitleThatExceedsBudget", OriginalTitle: "DifferentLongOriginalTitle"}
	// titleBytes=34 > titleBudget → truncation path
	// OriginalTitle != Title → else branch (line 166-168)
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE> (<ORIGINALTITLE>)", ctx, 20)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 20)
}

func TestExecuteWithMaxBytes_DifferentOriginalTitleTruncation(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "VeryLongTitleThatExceedsBudget", OriginalTitle: "DifferentLongOriginalTitle"}
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE> (<ORIGINALTITLE>)", ctx, 20)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 20)
}

func TestExecuteWithMaxBytes_RejectNonPositiveMaxBytes(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC", Title: "T"}
	_, err := e.ExecuteWithMaxBytes("<ID>", ctx, 0)
	assert.Error(t, err, "should reject maxBytes=0")
	_, err = e.ExecuteWithMaxBytes("<ID>", ctx, -1)
	assert.Error(t, err, "should reject negative maxBytes")
}

func TestExecuteWithMaxBytes_LinearScanExecuteError(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 5})
	ctx := &Context{ID: "ABC", Title: "LongTitle"}
	// Sentinel frame "ABC-\x00MAXBYTES\x00" = 16 > 5 → frame error → executeOrClamp
	// executeOrClamp: Execute "ABC-LongTitle" = 14 > 5 → error
	_, err := e.ExecuteWithMaxBytes("<ID>-<TITLE>", ctx, 10)
	assert.Error(t, err)
}

func TestExecuteWithMaxBytes_ConditionalElseExceedsMaxOutput(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 20})
	ctx := &Context{ID: "ABC", Title: "CD"}
	// When TITLE is present: IF branch renders "ABC-CD" (6 bytes ≤ 20 → passes)
	// When TITLE is empty: ELSE branch renders "ABC-VeryLongElseContent" (22 > 20 → error)
	// The render loop's 'continue' catches the error and tries the next state.
	_, err := e.ExecuteWithMaxBytes("<ID>-<IF:TITLE><TITLE><ELSE>VeryLongElseContent</IF>", ctx, 4)
	require.Error(t, err, "should error when no candidate fits within budget")
}

func TestExecuteWithMaxBytes_ScanLoopDifferentOriginalTitle(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "VeryLongTitle", OriginalTitle: "DifferentLongOriginal"}
	// maxBytes=10: "AB - VeryLongTitle (DifferentLongOriginal)" exceeds → clampResult
	// Empty title: "AB -  ()" fits ≤ 10 → scan runs
	// Scan tries budgets, OriginalTitle != Title → else branch
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE> (<ORIGINALTITLE>)", ctx, 10)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 10)
}

func TestExecuteWithMaxBytes_PropagateError_TitleBudgetPath(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 16})
	ctx := &Context{ID: "ABC", Title: "VeryLongTitle"}
	_, err := e.ExecuteWithMaxBytes("<ID>-<TITLE>", ctx, 3)
	assert.Error(t, err)
}

func TestExecuteWithMaxBytes_PropagateError_TitleFitsPath(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 16})
	ctx := &Context{ID: "ABC", Title: "ThirteenByteT"}
	_, err := e.ExecuteWithMaxBytes("<ID>-<TITLE>", ctx, 100)
	assert.Error(t, err)
}
