package template

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEngineWithOptions_Uncovered(t *testing.T) {
	t.Run("applies default limits when zero", func(t *testing.T) {
		e := newEngineWithOptions(engineOptions{})
		assert.Equal(t, DefaultMaxTemplateBytes, e.options.MaxTemplateBytes)
		assert.Equal(t, DefaultMaxOutputBytes, e.options.MaxOutputBytes)
		assert.Equal(t, DefaultMaxConditionalDepth, e.options.MaxConditionalDepth)
	})

	t.Run("respects custom limits", func(t *testing.T) {
		e := newEngineWithOptions(engineOptions{
			MaxTemplateBytes:    1024,
			MaxOutputBytes:      2048,
			MaxConditionalDepth: 10,
		})
		assert.Equal(t, 1024, e.options.MaxTemplateBytes)
		assert.Equal(t, 2048, e.options.MaxOutputBytes)
		assert.Equal(t, 10, e.options.MaxConditionalDepth)
	})

	t.Run("normalizes default language", func(t *testing.T) {
		e := newEngineWithOptions(engineOptions{DefaultLanguage: "EN"})
		assert.Equal(t, "en", e.options.DefaultLanguage)
	})

	t.Run("normalizes fallback languages", func(t *testing.T) {
		e := newEngineWithOptions(engineOptions{FallbackLanguages: []string{"EN", "ZH"}})
		assert.Equal(t, []string{"en", "zh"}, e.options.FallbackLanguages)
	})
}

func TestEngine_ExecuteWithContext_NilContext_Uncovered(t *testing.T) {
	e := NewEngine()
	_, err := e.ExecuteWithContext(nil, "<ID>", &Context{ID: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "execution context cannot be nil")
}

func TestEngine_Validate_Uncovered(t *testing.T) {
	e := NewEngine()

	t.Run("template too large", func(t *testing.T) {
		small := newEngineWithOptions(engineOptions{MaxTemplateBytes: 10})
		err := small.Validate("this is more than ten bytes")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum")
	})

	t.Run("conditional depth exceeds max", func(t *testing.T) {
		shallow := newEngineWithOptions(engineOptions{MaxConditionalDepth: 1})
		err := shallow.Validate("<IF:A><IF:B>x</IF></IF>")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conditional depth")
	})

	t.Run("unexpected closing tag", func(t *testing.T) {
		err := e.Validate("</IF>")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected closing")
	})

	t.Run("unclosed IF block", func(t *testing.T) {
		err := e.Validate("<IF:TAG>content")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unclosed")
	})

	t.Run("valid template passes", func(t *testing.T) {
		err := e.Validate("<IF:TAG>content</IF>")
		assert.NoError(t, err)
	})
}

func TestEngine_ResolveTag_ContentTypeID_Uncovered(t *testing.T) {
	e := NewEngine()

	t.Run("CONTENTID tag", func(t *testing.T) {
		result, err := e.Execute("<CONTENTID>", &Context{ContentID: "ipx001"})
		require.NoError(t, err)
		assert.Equal(t, "ipx001", result)
	})

	t.Run("CONTENTID with uppercase modifier", func(t *testing.T) {
		result, err := e.Execute("<CONTENTID:UPPER>", &Context{ContentID: "ipx001"})
		require.NoError(t, err)
		assert.Equal(t, "IPX001", result)
	})

	t.Run("CONTENTID with lowercase modifier", func(t *testing.T) {
		result, err := e.Execute("<CONTENTID:LOWER>", &Context{ContentID: "IPX001"})
		require.NoError(t, err)
		assert.Equal(t, "ipx001", result)
	})
}

func TestEngine_ResolveTag_PartSuffix_Uncovered(t *testing.T) {
	e := NewEngine()

	result, err := e.Execute("<PARTSUFFIX>", &Context{PartSuffix: "-pt1"})
	require.NoError(t, err)
	assert.Equal(t, "-pt1", result)
}

func TestEngine_ResolveTag_Rating_Uncovered(t *testing.T) {
	e := NewEngine()

	t.Run("with rating", func(t *testing.T) {
		result, err := e.Execute("<RATING>", &Context{Rating: 7.5})
		require.NoError(t, err)
		assert.Equal(t, "7.5", result)
	})

	t.Run("zero rating returns empty", func(t *testing.T) {
		result, err := e.Execute("<RATING>", &Context{Rating: 0})
		require.NoError(t, err)
		assert.Equal(t, "", result)
	})
}

func TestEngine_ResolveTag_MultiPart_Uncovered(t *testing.T) {
	e := NewEngine()

	t.Run("is multipart", func(t *testing.T) {
		result, err := e.Execute("<MULTIPART>", &Context{IsMultiPart: true})
		require.NoError(t, err)
		assert.Equal(t, "true", result)
	})

	t.Run("not multipart", func(t *testing.T) {
		result, err := e.Execute("<MULTIPART>", &Context{IsMultiPart: false})
		require.NoError(t, err)
		assert.Equal(t, "", result)
	})
}

func TestEngine_ResolveTag_Filename_Uncovered(t *testing.T) {
	e := NewEngine()

	t.Run("FILENAME strips extension", func(t *testing.T) {
		result, err := e.Execute("<FILENAME>", &Context{OriginalFilename: "video.mp4"})
		require.NoError(t, err)
		assert.Equal(t, "video", result)
	})

	t.Run("FILENAME_EXT keeps extension", func(t *testing.T) {
		result, err := e.Execute("<FILENAME_EXT>", &Context{OriginalFilename: "video.mp4"})
		require.NoError(t, err)
		assert.Equal(t, "video.mp4", result)
	})
}

func TestEngine_ResolveTag_UnknownTag_Uncovered(t *testing.T) {
	e := NewEngine()

	_, err := e.resolveTag("UNKNOWN_TAG", "", &Context{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tag")
}

func TestEngine_ResolveTag_IDModifier_Uncovered(t *testing.T) {
	e := NewEngine()

	t.Run("ID with UPPER modifier", func(t *testing.T) {
		val, err := e.resolveTag("ID", "UPPER", &Context{ID: "ipx-001"})
		require.NoError(t, err)
		assert.Equal(t, "IPX-001", val)
	})

	t.Run("ID with LOWER modifier", func(t *testing.T) {
		val, err := e.resolveTag("ID", "LOWER", &Context{ID: "IPX-001"})
		require.NoError(t, err)
		assert.Equal(t, "ipx-001", val)
	})

	t.Run("ID with unknown modifier returns as-is", func(t *testing.T) {
		val, err := e.resolveTag("ID", "unknown", &Context{ID: "IPX-001"})
		require.NoError(t, err)
		assert.Equal(t, "IPX-001", val)
	})
}

func TestEngine_ExecuteWithContext_Cancellation_Uncovered(t *testing.T) {
	e := NewEngine()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.ExecuteWithContext(ctx, "<ID>", &Context{ID: "test"})
	assert.Error(t, err)
}

func TestEngine_EnsureOutputWithinLimit_Uncovered(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 5})
	err := e.ensureOutputWithinLimit("this is more than 5 bytes")
	assert.Error(t, err)
}

func TestEngine_LooksLikeLanguageSpec_Uncovered(t *testing.T) {
	e := NewEngine()

	assert.True(t, e.translationResolver.looksLikeLanguageSpec("en"))
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("zh"))
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("ja|en"))
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("en-US"))
	assert.False(t, e.translationResolver.looksLikeLanguageSpec(""))
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("123"))
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("a"))
}
