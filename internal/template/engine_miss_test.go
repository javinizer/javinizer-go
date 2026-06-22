package template

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ExecuteWithMaxBytes: title needs truncation and OriginalTitle differs ---

func TestExecuteWithMaxBytes_Miss_TitleAndOriginalDiffer(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		ID:               "TEST-001",
		Title:            "Very Long Movie Title That Needs Truncation For Path Length",
		OriginalTitle:    "Very Different Original Title That Also Needs Truncation",
		Actresses:        []string{"Actress1"},
		ReleaseDate:      parseTestDate("2024-01-15"),
		OriginalFilename: "test.mp4",
	}

	// Use a template that includes both Title and other fixed text
	result, err := e.ExecuteWithMaxBytes("/path/<TITLE>/test.mp4", ctx, 50)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result), 60) // Some margin for path separators
}

// --- ExecuteWithContext: nil execution context ---

func TestExecuteWithContext_Miss_NilExecCtx(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "TEST-001"}

	_, err := e.ExecuteWithContext(nil, "<ID>", ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution context cannot be nil")
}

// --- ExecuteWithContext: nil context ---

func TestExecuteWithContext_Miss_NilContext(t *testing.T) {
	e := NewEngine()

	_, err := e.ExecuteWithContext(context.Background(), "<ID>", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cannot be nil")
}

// --- ExecuteWithContext: cancelled context ---

func TestExecuteWithContext_Miss_CancelledContext(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "TEST-001"}

	ctx2, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.ExecuteWithContext(ctx2, "<ID>", ctx)
	require.Error(t, err)
}

// --- resolveTag: ORIGINALTITLE with translations ---

func TestResolveTag_Miss_OriginalTitleWithTranslation(t *testing.T) {
	e := newEngineWithOptions(engineOptions{
		DefaultLanguage: "ja",
	})

	ctx := &Context{
		OriginalTitle: "English Title",
		Translations: map[string]models.MovieTranslation{
			"ja": {OriginalTitle: "Japanese Title"},
		},
	}

	result, err := e.resolveTag("ORIGINALTITLE", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Japanese Title", result)
}

// --- resolveTag: TITLE with rejected language falls back to base ---

func TestResolveTag_Miss_TitleRejectedLanguage(t *testing.T) {
	e := newEngineWithOptions(engineOptions{
		DefaultLanguage: "ja",
	})

	ctx := &Context{
		Title: "English Title",
		Translations: map[string]models.MovieTranslation{
			"ja": {Title: "Japanese Title"},
		},
	}

	// Use an invalid language modifier that looks like a language but isn't valid
	result, err := e.resolveTag("TITLE", "zzz_invalid", ctx)
	require.NoError(t, err)
	// Should fall back to base field since the language spec is rejected
	assert.Equal(t, "English Title", result)
}

// --- TruncateTitle: CJK text with maxLen <= 3 returns as-is ---

func TestTruncateTitle_Miss_CJKMaxLen3(t *testing.T) {
	e := NewEngine()
	// When isCJK and maxLen <= 3, CJK branch returns title unchanged
	result := e.TruncateTitle("テスト映画", 3)
	assert.Equal(t, "テスト映画", result)
}

// --- TruncateTitle: CJK text shorter than maxLen ---

func TestTruncateTitle_Miss_CJKShorterThanMaxLen(t *testing.T) {
	e := NewEngine()
	result := e.TruncateTitle("テスト", 10)
	assert.Equal(t, "テスト", result)
}

// --- TruncateTitle: non-CJK with maxLen <= 3 ---

func TestTruncateTitle_Miss_NonCJKMaxLen3(t *testing.T) {
	e := NewEngine()
	result := e.TruncateTitle("Hello", 3)
	assert.Equal(t, "Hel", result)
}

// --- TruncateTitle: zero maxLen returns as-is ---

func TestTruncateTitle_Miss_ZeroMaxLen(t *testing.T) {
	e := NewEngine()
	result := e.TruncateTitle("Hello World", 0)
	assert.Equal(t, "Hello World", result)
}

// --- parseModifier: fallback chain (pipe-separated) ---

func TestParseModifier_Miss_FallbackChain(t *testing.T) {
	e := NewEngine()
	parsed := e.translationResolver.parseModifier("TITLE", "ja|en")
	assert.True(t, parsed.isLanguage)
	assert.Equal(t, "ja|en", parsed.languageSpec)
}

// --- parseModifier: invalid fallback chain ---

func TestParseModifier_Miss_InvalidFallbackChain(t *testing.T) {
	e := NewEngine()
	parsed := e.translationResolver.parseModifier("TITLE", "ja|invalid_spec")
	assert.False(t, parsed.isLanguage)
}

// --- looksLikeLanguageSpec: various inputs ---

func TestLooksLikeLanguageSpec_Miss(t *testing.T) {
	e := NewEngine()

	// Empty
	assert.False(t, e.translationResolver.looksLikeLanguageSpec(""))

	// Pipe-separated
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("ja|en"))

	// 2-letter code
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("ja"))

	// 3-letter code
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("jpn"))

	// Too long
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("japanese"))

	// With hyphen/underscore (locale-like)
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("ja-JP"))

	// Non-alpha prefix
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("12-JP"))
}

// --- TruncateTitleBytes: very small budget (can't fit any rune) ---

func TestTruncateTitleBytes_Miss_VerySmallBudget(t *testing.T) {
	e := NewEngine()

	// maxBytes = 1, can't fit "..." or any multi-byte rune
	result := e.TruncateTitleBytes("テスト", 1)
	assert.Empty(t, result)

	// maxBytes = 2, same
	result = e.TruncateTitleBytes("テスト", 2)
	assert.Empty(t, result)
}

// --- TruncateTitleBytes: zero maxBytes ---

func TestTruncateTitleBytes_Miss_ZeroMaxBytes(t *testing.T) {
	e := NewEngine()
	result := e.TruncateTitleBytes("Hello", 0)
	assert.Empty(t, result)
}

// --- TruncateTitleBytes: negative maxBytes ---

func TestTruncateTitleBytes_Miss_NegativeMaxBytes(t *testing.T) {
	e := NewEngine()
	result := e.TruncateTitleBytes("Hello", -1)
	assert.Empty(t, result)
}

// --- ValidatePathLength: path within limit ---

func TestValidatePathLength_Miss_WithinLimit(t *testing.T) {
	e := NewEngine()
	err := e.ValidatePathLength("/short/path", 100)
	require.NoError(t, err)
}

// --- ValidatePathLength: path exceeds limit ---

func TestValidatePathLength_Miss_ExceedsLimit(t *testing.T) {
	e := NewEngine()
	err := e.ValidatePathLength("/very/long/path", 5)
	require.Error(t, err)
}

// --- ValidatePathLength: zero or negative limit ---

func TestValidatePathLength_Miss_ZeroLimit(t *testing.T) {
	e := NewEngine()
	err := e.ValidatePathLength("/any/path", 0)
	require.NoError(t, err)
}

// --- resolveTag: YEAR with ReleaseYear field ---

func TestResolveTag_Miss_YearFromReleaseYear(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ReleaseYear: 2024}
	result, err := e.resolveTag("YEAR", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "2024", result)
}

// --- resolveTag: YEAR with no date ---

func TestResolveTag_Miss_YearNoDate(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}
	result, err := e.resolveTag("YEAR", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

// --- resolveTag: RUNTIME with value ---

func TestResolveTag_Miss_Runtime(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Runtime: 120}
	result, err := e.resolveTag("RUNTIME", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "120", result)
}

// --- resolveTag: ACTRESS with ActressName ---

func TestResolveTag_Miss_ActressName(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ActressName: "Jane Doe"}
	result, err := e.resolveTag("ACTRESS", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Jane Doe", result)
}

// --- resolveTag: ACTRESS with ActressDetails ---

func TestResolveTag_Miss_ActressDetails(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		ActressDetails: []ActressDetail{
			{FirstName: "Jane", LastName: "Doe", JapaneseName: "田中"},
		},
	}
	result, err := e.resolveTag("ACTRESS", "", ctx)
	require.NoError(t, err)
	assert.Contains(t, result, "Jane")
}

// --- resolveTag: PART with modifier ---

func TestResolveTag_Miss_PartWithModifier(t *testing.T) {
	e := NewEngine()
	ctx := &Context{PartNumber: 3}
	result, err := e.resolveTag("PART", "2", ctx)
	require.NoError(t, err)
	assert.Equal(t, "03", result)
}

// --- resolveTag: RATING with value ---

func TestResolveTag_Miss_Rating(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Rating: 7.5}
	result, err := e.resolveTag("RATING", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "7.5", result)
}

// --- resolveTag: MULTIPART true ---

func TestResolveTag_Miss_MultiPartTrue(t *testing.T) {
	e := NewEngine()
	ctx := &Context{IsMultiPart: true}
	result, err := e.resolveTag("MULTIPART", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "true", result)
}

// --- resolveTag: unknown tag ---

func TestResolveTag_Miss_UnknownTag(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}
	_, err := e.resolveTag("UNKNOWNTAG", "", ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tag")
}

// --- applyCaseModifier: UPPERCASE ---

func TestApplyCaseModifier_Miss_Uppercase(t *testing.T) {
	e := NewEngine()
	result := e.applyCaseModifier("hello", "UPPERCASE")
	assert.Equal(t, "HELLO", result)
}

// --- applyCaseModifier: LOWERCASE ---

func TestApplyCaseModifier_Miss_Lowercase(t *testing.T) {
	e := NewEngine()
	result := e.applyCaseModifier("HELLO", "lowercase")
	assert.Equal(t, "hello", result)
}

// --- applyCaseModifier: unknown modifier returns as-is ---

func TestApplyCaseModifier_Miss_Unknown(t *testing.T) {
	e := NewEngine()
	result := e.applyCaseModifier("Hello", "rot13")
	assert.Equal(t, "Hello", result)
}

// --- Helper ---

func parseTestDate(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}
