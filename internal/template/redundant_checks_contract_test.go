package template

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestOutputLimitValidationContract(t *testing.T) {
	for _, tc := range []struct {
		name, tmpl, title, series, want string
		limit, exceededBytes            int
	}{
		{name: "literal at byte boundary", tmpl: "日本", limit: 6, want: "日本"},
		{name: "literal exceeds byte boundary", tmpl: "日本", limit: 5, exceededBytes: 6},
		{name: "conditional selected literal", tmpl: "<IF:SERIES>日本<ELSE>x</IF>", series: "present", limit: 5, exceededBytes: 6},
		{name: "conditional discards oversized branch", tmpl: "<IF:SERIES>日本<ELSE>x</IF>", limit: 1, want: "x"},
		{name: "conditional else exceeds limit", tmpl: "<IF:SERIES>x<ELSE>日本</IF>", limit: 5, exceededBytes: 6},
		{name: "tag expansion at byte boundary", tmpl: "<TITLE>", title: "日本語", limit: 9, want: "日本語"},
		{name: "tag expansion exceeds limit", tmpl: "<TITLE>", title: "日本語", limit: 8, exceededBytes: 9},
		{name: "conditional tag expansion exceeds limit", tmpl: "<IF:SERIES><TITLE><ELSE>x</IF>", series: "present", title: "日本語", limit: 8, exceededBytes: 9},
		{name: "repeated tag expansion exceeds limit", tmpl: "<TITLE><TITLE>", title: strings.Repeat("x", 8), limit: 14, exceededBytes: 16},
		// The conditional-stage limit intentionally precedes tag substitution, even when substitution would shrink output.
		{name: "pretag limit retained", tmpl: "<TITLE>", title: "x", limit: 6, exceededBytes: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := newEngineWithOptions(engineOptions{MaxOutputBytes: tc.limit})
			ctx := &Context{Title: tc.title, Series: tc.series}
			for name, execute := range map[string]func() (string, error){
				"Execute":            func() (string, error) { return engine.Execute(tc.tmpl, ctx) },
				"ExecuteWithContext": func() (string, error) { return engine.ExecuteWithContext(context.Background(), tc.tmpl, ctx) },
			} {
				t.Run(name, func(t *testing.T) {
					got, err := execute()
					if tc.exceededBytes == 0 {
						require.NoError(t, err)
						require.Equal(t, tc.want, got)
						return
					}
					require.Empty(t, got)
					require.EqualError(t, err, fmt.Sprintf("rendered template size %d exceeds maximum %d bytes: output exceeds maximum", tc.exceededBytes, tc.limit))
					require.ErrorIs(t, err, errOutputLimit)
					require.Same(t, errOutputLimit, errors.Unwrap(err))
					require.IsType(t, fmt.Errorf("wrapped: %w", errOutputLimit), err)
				})
			}
		})
	}
}

func TestTitleModifierParsingAndRenderingContract(t *testing.T) {
	engine := newEngineWithOptions(engineOptions{DefaultLanguage: "en"})
	ctx := &Context{Title: "基本作品題名", Translations: map[string]models.MovieTranslation{
		"en": {Language: "en", Title: "English title"},
		"ja": {Language: "ja", Title: "日本語作品名"},
	}}
	for _, tc := range []struct {
		modifier, want, language, truncation string
		numeric, rejected                    bool
	}{
		{modifier: "3", want: "Eng", truncation: "3", numeric: true},
		{modifier: "+3", want: "Eng", truncation: "+3", numeric: true},
		{modifier: "03", want: "Eng", truncation: "03", numeric: true},
		{modifier: "0", want: "English title", truncation: "0"},
		{modifier: "-3", want: "English title", truncation: "-3"},
		{modifier: "999999999999999999999999999999", want: "English title", truncation: "999999999999999999999999999999"},
		{modifier: " 3 ", want: "Eng", truncation: " 3 "},
		{modifier: "３", want: "English title", truncation: "３"},
		{modifier: "ja", want: "日本語作品名", language: "ja"},
		{modifier: "fr|ja", want: "日本語作品名", language: "fr|ja"},
		{modifier: "fr", want: "English title", language: "fr"},
		{modifier: "ja|3", want: "基本作品題名", rejected: true},
	} {
		t.Run(tc.modifier, func(t *testing.T) {
			resolver := newTranslationResolver(engineOptions{})
			require.Equal(t, tc.numeric, resolver.isNumericModifier(tc.modifier))
			parsed := resolver.parseModifier("TITLE", tc.modifier)
			require.Equal(t, tc.language != "", parsed.isLanguage)
			require.Equal(t, tc.language, parsed.languageSpec)
			require.Equal(t, tc.truncation, parsed.truncationModifier)
			require.Equal(t, tc.rejected, parsed.rejectedLanguage)
			got, err := engine.Execute("<TITLE:"+tc.modifier+">", ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestMissingTitleTranslationFallsBackToBaseContract(t *testing.T) {
	got, err := NewEngine().Execute("<TITLE:fr>", &Context{Title: "基本作品題名"})
	require.NoError(t, err)
	require.Equal(t, "基本作品題名", got)
}
