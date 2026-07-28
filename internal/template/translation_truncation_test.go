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
	got, err := e.ExecuteWithMaxBytes("<ID>-<TITLE>", ctx, 100)
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.LessOrEqual(t, len(got), 100)
	}
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

func TestExecuteWithMaxBytes_TruncatedRenderOnExecuteError(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 15})
	longTitle := strings.Repeat("A", 100)
	ctx := &Context{ID: "AB", Title: longTitle}
	// Full title: "AB-" + 100 A's = 103 bytes > MaxOutputBytes=10 → Execute errors
	// Truncated to maxBytes=10: "AB-" + 7 A's = 10 bytes ≤ 10 → succeeds
	got, err := e.ExecuteWithMaxBytes("<ID>-<TITLE>", ctx, 10)
	require.NoError(t, err, "truncated render should succeed")
	assert.LessOrEqual(t, len(got), 10)
}

func TestClampResult_EmojiConditionalProbeFindsFit(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "\U0001F600\U0001F600\U0001F600\U0001F600"}
	// Title = 4 emoji (16 bytes). Template: <IF:TITLE><TITLE><ELSE>12345</IF>
	// Render full title: 4 emoji = 16 bytes > maxBytes=4 → enters clampResult.
	// Binary search skips budget=4 (one emoji) because it converges to 0-3 where
	// title is empty → ELSE "12345" = 5 > 4. Probes try 0-4, budget=4 finds fit.
	result, err := e.Execute("<IF:TITLE><TITLE><ELSE>12345</IF>", ctx)
	require.NoError(t, err)
	got, err := e.clampResult("<IF:TITLE><TITLE><ELSE>12345</IF>", ctx, result, 4)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 4)
}

func TestExecuteWithMaxBytes_CJKConditionalBoundedProbe(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "XY", Title: "これは長い日本語のタイトルです"}
	// CJK title: each rune is 3 bytes. Budget 1-2 leaves title empty (<IF:TITLE> false).
	// Budget 3: first rune "こ" (3 bytes) → <IF:TITLE> true → IF branch.
	// Empty title: ELSE branch (shorter).
	got, err := e.ExecuteWithMaxBytes("<IF:TITLE><TITLE>-<ID><ELSE><ID></IF>", ctx, 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 5)
}

func TestClampResult_ProbeErrorPath(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 20})
	ctx := &Context{ID: "ABC", Title: "LongTitle"}
	// Template: empty title → ELSE branch exceeds MaxOutputBytes → probe errors at budget=0
	// Non-empty title → IF branch → "ABC-LongTitle" exceeds maxBytes=4
	// Probes at budget=0 error (ELSE too long), probes at 1-4 produce short results.
	result, err := e.Execute("<ID>-<IF:TITLE><TITLE><ELSE>VeryLongElseContentThatExceeds</IF>", ctx)
	require.NoError(t, err)
	_, err = e.clampResult("<ID>-<IF:TITLE><TITLE><ELSE>VeryLongElseContentThatExceeds</IF>", ctx, result, 4)
	require.Error(t, err, "should error when no candidate fits")
}

func TestExecuteWithMaxBytes_BoundedProbeErrorAndShortest(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 20})
	ctx := &Context{ID: "ABC", Title: "LongTitle"}
	// Template: empty title → ELSE branch exceeds MaxOutputBytes → probe errors
	// Non-empty title → IF branch → "ABC-LongTitle" exceeds maxBytes=4
	// Binary search fails, probes try budget 0-4, budget 0 errors (ELSE too long)
	_, err := e.ExecuteWithMaxBytes("<ID>-<IF:TITLE><TITLE><ELSE>VeryLongElseContentThatExceeds</IF>", ctx, 4)
	require.Error(t, err, "should error when no candidate fits")
}

func TestExecuteWithMaxBytes_BoundedProbeFindsFit(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "XY", Title: "ABCDEFGHIJ"}
	// Template with conditional: empty title uses ELSE (short), non-empty uses IF (long)
	// maxBytes=5: binary search on IF branch fails (long), probe at budget=0 hits ELSE (short)
	got, err := e.ExecuteWithMaxBytes("<IF:TITLE><TITLE>-<ID><ELSE><ID></IF>", ctx, 5)
	require.NoError(t, err, "bounded probe should find ELSE branch fits")
	assert.LessOrEqual(t, len(got), 5)
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
	// Execute: "ABC-ThirteenByteT" = 18 > 16 → error
	// clampResult binary search: all budgets error → "cannot fit"
	got, err := e.ExecuteWithMaxBytes("<ID>-<TITLE>", ctx, 10)
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.LessOrEqual(t, len(got), 10)
	}
}
