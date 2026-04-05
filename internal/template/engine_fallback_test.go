package template

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestResolveTranslatedTagFallback(t *testing.T) {
	tests := []struct {
		name         string
		engineOpts   EngineOptions
		ctx          *Context
		tagName      string
		explicitLang string
		want         string
	}{
		{
			name:       "No translations - fallback to base field",
			engineOpts: EngineOptions{},
			ctx: &Context{
				Title:        "Base Title",
				Translations: map[string]models.MovieTranslation{},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Base Title",
		},
		{
			name:       "Explicit language takes priority",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
					"zh": {Language: "zh", Title: "Chinese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "ja",
			want:         "Japanese Title",
		},
		{
			name:       "Context default language takes priority over engine",
			engineOpts: EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"zh"}},
			ctx: &Context{
				Title:           "Base Title",
				DefaultLanguage: "ja",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
					"zh": {Language: "zh", Title: "Chinese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Japanese Title",
		},
		{
			name:       "Engine default language used when context default empty",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			ctx: &Context{
				Title:           "Base Title",
				DefaultLanguage: "",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "English Title",
		},
		{
			name:       "Fallback languages used when default not found",
			engineOpts: EngineOptions{DefaultLanguage: "ko", FallbackLanguages: []string{"ja", "en"}},
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Japanese Title",
		},
		{
			name:       "Multiple fallback languages - first match wins",
			engineOpts: EngineOptions{DefaultLanguage: "ko", FallbackLanguages: []string{"zh", "ja", "en"}},
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Japanese Title",
		},
		{
			name:       "Fallback to base field when no translation matches",
			engineOpts: EngineOptions{DefaultLanguage: "ko", FallbackLanguages: []string{"fr", "de"}},
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Base Title",
		},
		{
			name:       "Explicit language overrides all defaults",
			engineOpts: EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"ja"}},
			ctx: &Context{
				Title:           "Base Title",
				DefaultLanguage: "zh",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
					"zh": {Language: "zh", Title: "Chinese Title"},
					"ko": {Language: "ko", Title: "Korean Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "ko",
			want:         "Korean Title",
		},
		{
			name:       "Empty translation value - continue to next candidate",
			engineOpts: EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"ja"}},
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: ""},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Japanese Title",
		},
		{
			name:       "All translations empty - fallback to base",
			engineOpts: EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"ja"}},
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: ""},
					"ja": {Language: "ja", Title: ""},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Base Title",
		},
		{
			name:       "Nil translations map - fallback to base",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			ctx: &Context{
				Title:        "Base Title",
				Translations: nil,
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Base Title",
		},
		{
			name:       "DIRECTOR tag with translation",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			ctx: &Context{
				Director: "Base Director",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Director: "English Director"},
					"ja": {Language: "ja", Director: "Japanese Director"},
				},
			},
			tagName:      "DIRECTOR",
			explicitLang: "",
			want:         "English Director",
		},
		{
			name:       "MAKER tag with translation",
			engineOpts: EngineOptions{DefaultLanguage: "ja"},
			ctx: &Context{
				Maker: "Base Maker",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Maker: "English Maker"},
					"ja": {Language: "ja", Maker: "Japanese Maker"},
				},
			},
			tagName:      "MAKER",
			explicitLang: "",
			want:         "Japanese Maker",
		},
		{
			name:       "STUDIO tag synonym uses same translation",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			ctx: &Context{
				Maker: "Base Maker",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Maker: "English Studio"},
				},
			},
			tagName:      "STUDIO",
			explicitLang: "",
			want:         "English Studio",
		},
		{
			name:       "LABEL tag with translation",
			engineOpts: EngineOptions{DefaultLanguage: "ja"},
			ctx: &Context{
				Label: "Base Label",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Label: "English Label"},
					"ja": {Language: "ja", Label: "Japanese Label"},
				},
			},
			tagName:      "LABEL",
			explicitLang: "",
			want:         "Japanese Label",
		},
		{
			name:       "SERIES tag with translation",
			engineOpts: EngineOptions{DefaultLanguage: "zh"},
			ctx: &Context{
				Series: "Base Series",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Series: "English Series"},
					"zh": {Language: "zh", Series: "Chinese Series"},
				},
			},
			tagName:      "SERIES",
			explicitLang: "",
			want:         "Chinese Series",
		},
		{
			name:       "DESCRIPTION tag with translation",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			ctx: &Context{
				Description: "Base Description",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Description: "English Description"},
				},
			},
			tagName:      "DESCRIPTION",
			explicitLang: "",
			want:         "English Description",
		},
		{
			name:       "ORIGINALTITLE tag with translation",
			engineOpts: EngineOptions{DefaultLanguage: "ja"},
			ctx: &Context{
				OriginalTitle: "Base OriginalTitle",
				Translations: map[string]models.MovieTranslation{
					"ja": {Language: "ja", OriginalTitle: "Japanese OriginalTitle"},
				},
			},
			tagName:      "ORIGINALTITLE",
			explicitLang: "",
			want:         "Japanese OriginalTitle",
		},
		{
			name:       "No engine default language - skips to fallbacks",
			engineOpts: EngineOptions{FallbackLanguages: []string{"ja", "en"}},
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Japanese Title",
		},
		{
			name:       "No engine settings at all - fallback to base",
			engineOpts: EngineOptions{},
			ctx: &Context{
				Title:        "Base Title",
				Translations: map[string]models.MovieTranslation{},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Base Title",
		},
		{
			name:       "Context default overrides engine fallbacks order",
			engineOpts: EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"zh"}},
			ctx: &Context{
				Title:           "Base Title",
				DefaultLanguage: "ja",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"zh": {Language: "zh", Title: "Chinese Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			tagName:      "TITLE",
			explicitLang: "",
			want:         "Japanese Title",
		},
		{
			name:       "Partial translation - only some fields translated",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			ctx: &Context{
				Title:    "Base Title",
				Director: "Base Director",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			tagName:      "DIRECTOR",
			explicitLang: "",
			want:         "Base Director",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngineWithOptions(tt.engineOpts)
			got := engine.resolveTranslatedTag(tt.tagName, tt.explicitLang, tt.ctx)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLanguageCandidatesPrecedence(t *testing.T) {
	tests := []struct {
		name           string
		engineOpts     EngineOptions
		ctx            *Context
		explicitLang   string
		wantCandidates []string
	}{
		{
			name:           "Empty explicit, empty context, empty engine",
			engineOpts:     EngineOptions{},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: nil,
		},
		{
			name:           "Explicit language only",
			engineOpts:     EngineOptions{},
			ctx:            &Context{},
			explicitLang:   "en",
			wantCandidates: []string{"en"},
		},
		{
			name:           "Context default only",
			engineOpts:     EngineOptions{},
			ctx:            &Context{DefaultLanguage: "ja"},
			explicitLang:   "",
			wantCandidates: []string{"ja"},
		},
		{
			name:           "Engine default only",
			engineOpts:     EngineOptions{DefaultLanguage: "zh"},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"zh"},
		},
		{
			name:           "Engine fallbacks only",
			engineOpts:     EngineOptions{FallbackLanguages: []string{"en", "ja"}},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"en", "ja"},
		},
		{
			name:           "All sources - explicit takes priority",
			engineOpts:     EngineOptions{DefaultLanguage: "zh", FallbackLanguages: []string{"ko", "en"}},
			ctx:            &Context{DefaultLanguage: "ja"},
			explicitLang:   "fr",
			wantCandidates: []string{"fr", "ja", "zh", "ko", "en"},
		},
		{
			name:           "Context default takes priority over engine",
			engineOpts:     EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"zh"}},
			ctx:            &Context{DefaultLanguage: "ja"},
			explicitLang:   "",
			wantCandidates: []string{"ja", "en", "zh"},
		},
		{
			name:           "Engine default after context",
			engineOpts:     EngineOptions{DefaultLanguage: "en"},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"en"},
		},
		{
			name:           "Fallbacks after engine default",
			engineOpts:     EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"ja", "zh"}},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"en", "ja", "zh"},
		},
		{
			name:           "Duplicate handling - explicit same as context (deduped)",
			engineOpts:     EngineOptions{DefaultLanguage: "en"},
			ctx:            &Context{DefaultLanguage: "ja"},
			explicitLang:   "ja",
			wantCandidates: []string{"ja", "en"},
		},
		{
			name:           "Duplicate handling - explicit same as engine default (deduped)",
			engineOpts:     EngineOptions{DefaultLanguage: "en"},
			ctx:            &Context{DefaultLanguage: "ja"},
			explicitLang:   "en",
			wantCandidates: []string{"en", "ja"},
		},
		{
			name:           "Duplicate handling - context same as engine default (deduped)",
			engineOpts:     EngineOptions{DefaultLanguage: "en"},
			ctx:            &Context{DefaultLanguage: "en"},
			explicitLang:   "",
			wantCandidates: []string{"en"},
		},
		{
			name:           "Fallback duplicates - all deduped including DefaultLanguage",
			engineOpts:     EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"en", "ja", "en"}},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"en", "ja"},
		},
		{
			name:           "Multiple fallbacks with ordering preserved",
			engineOpts:     EngineOptions{FallbackLanguages: []string{"zh", "ko", "ja", "en"}},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"zh", "ko", "ja", "en"},
		},
		{
			name:           "Explicit language normalized at runtime",
			engineOpts:     EngineOptions{},
			ctx:            &Context{},
			explicitLang:   "EN",
			wantCandidates: []string{"en"},
		},
		{
			name:           "Context default normalized at runtime",
			engineOpts:     EngineOptions{},
			ctx:            &Context{DefaultLanguage: "JA"},
			explicitLang:   "",
			wantCandidates: []string{"ja"},
		},
		{
			name:           "Engine default normalized at creation",
			engineOpts:     EngineOptions{DefaultLanguage: "ZH"},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"zh"},
		},
		{
			name:           "Engine fallbacks normalized at creation",
			engineOpts:     EngineOptions{FallbackLanguages: []string{"EN", "JA"}},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"en", "ja"},
		},
		{
			name:           "Invalid explicit language filtered at runtime",
			engineOpts:     EngineOptions{DefaultLanguage: "en"},
			ctx:            &Context{},
			explicitLang:   "eng",
			wantCandidates: []string{"en"},
		},
		{
			name:           "Invalid context default filtered at runtime",
			engineOpts:     EngineOptions{DefaultLanguage: "en"},
			ctx:            &Context{DefaultLanguage: "jpn"},
			explicitLang:   "",
			wantCandidates: []string{"en"},
		},
		{
			name:           "Invalid engine default filtered at creation",
			engineOpts:     EngineOptions{DefaultLanguage: "eng"},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: nil,
		},
		{
			name:           "Invalid fallbacks filtered at creation",
			engineOpts:     EngineOptions{FallbackLanguages: []string{"eng", "jpn", "en"}},
			ctx:            &Context{},
			explicitLang:   "",
			wantCandidates: []string{"en"},
		},
		{
			name:           "Empty strings filtered from fallbacks at creation",
			engineOpts:     EngineOptions{DefaultLanguage: "", FallbackLanguages: []string{"", "en"}},
			ctx:            &Context{DefaultLanguage: ""},
			explicitLang:   "",
			wantCandidates: []string{"en"},
		},
		{
			name:           "Region suffixes normalized in all sources",
			engineOpts:     EngineOptions{DefaultLanguage: "en-US", FallbackLanguages: []string{"ja_JP"}},
			ctx:            &Context{DefaultLanguage: "zh-Hant"},
			explicitLang:   "ko-KR",
			wantCandidates: []string{"ko", "zh", "en", "ja"},
		},
		{
			name:           "Complex scenario with all sources",
			engineOpts:     EngineOptions{DefaultLanguage: "en-US", FallbackLanguages: []string{"ja_JP", "zh-Hant", "ko"}},
			ctx:            &Context{DefaultLanguage: "fr"},
			explicitLang:   "de",
			wantCandidates: []string{"de", "fr", "en", "ja", "zh", "ko"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngineWithOptions(tt.engineOpts)
			got := engine.languageCandidates(tt.explicitLang, tt.ctx)
			assert.Equal(t, tt.wantCandidates, got)
		})
	}
}

func TestResolveBaseTag(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name    string
		tagName string
		ctx     *Context
		want    string
	}{
		{"TITLE base field", "TITLE", &Context{Title: "Base Title"}, "Base Title"},
		{"ORIGINALTITLE base field", "ORIGINALTITLE", &Context{OriginalTitle: "Base OriginalTitle"}, "Base OriginalTitle"},
		{"DIRECTOR base field", "DIRECTOR", &Context{Director: "Base Director"}, "Base Director"},
		{"MAKER base field", "MAKER", &Context{Maker: "Base Maker"}, "Base Maker"},
		{"STUDIO base field uses Maker", "STUDIO", &Context{Maker: "Base Studio"}, "Base Studio"},
		{"LABEL base field", "LABEL", &Context{Label: "Base Label"}, "Base Label"},
		{"SERIES base field", "SERIES", &Context{Series: "Base Series"}, "Base Series"},
		{"DESCRIPTION base field", "DESCRIPTION", &Context{Description: "Base Description"}, "Base Description"},
		{"Empty TITLE", "TITLE", &Context{Title: ""}, ""},
		{"Empty fields", "DIRECTOR", &Context{}, ""},
		{"Unknown tag returns empty", "UNKNOWN", &Context{Title: "Title"}, ""},
		{"ID not in translatable list returns empty", "ID", &Context{ID: "ABC-123"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.resolveBaseTag(tt.tagName, tt.ctx)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTranslationFieldValue(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name    string
		tagName string
		lang    string
		ctx     *Context
		want    string
	}{
		{
			name:    "TITLE from translation",
			tagName: "TITLE",
			lang:    "en",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want: "English Title",
		},
		{
			name:    "DIRECTOR from translation",
			tagName: "DIRECTOR",
			lang:    "ja",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"ja": {Language: "ja", Director: "Japanese Director"},
				},
			},
			want: "Japanese Director",
		},
		{
			name:    "MAKER from translation",
			tagName: "MAKER",
			lang:    "zh",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"zh": {Language: "zh", Maker: "Chinese Maker"},
				},
			},
			want: "Chinese Maker",
		},
		{
			name:    "STUDIO uses Maker field",
			tagName: "STUDIO",
			lang:    "en",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Maker: "English Studio"},
				},
			},
			want: "English Studio",
		},
		{
			name:    "LABEL from translation",
			tagName: "LABEL",
			lang:    "ja",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"ja": {Language: "ja", Label: "Japanese Label"},
				},
			},
			want: "Japanese Label",
		},
		{
			name:    "SERIES from translation",
			tagName: "SERIES",
			lang:    "en",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Series: "English Series"},
				},
			},
			want: "English Series",
		},
		{
			name:    "DESCRIPTION from translation",
			tagName: "DESCRIPTION",
			lang:    "zh",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"zh": {Language: "zh", Description: "Chinese Description"},
				},
			},
			want: "Chinese Description",
		},
		{
			name:    "ORIGINALTITLE from translation",
			tagName: "ORIGINALTITLE",
			lang:    "ja",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"ja": {Language: "ja", OriginalTitle: "Japanese OriginalTitle"},
				},
			},
			want: "Japanese OriginalTitle",
		},
		{
			name:    "Language not found returns empty",
			tagName: "TITLE",
			lang:    "fr",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want: "",
		},
		{
			name:    "Nil translations returns empty",
			tagName: "TITLE",
			lang:    "en",
			ctx:     &Context{Translations: nil},
			want:    "",
		},
		{
			name:    "Empty translations returns empty",
			tagName: "TITLE",
			lang:    "en",
			ctx:     &Context{Translations: map[string]models.MovieTranslation{}},
			want:    "",
		},
		{
			name:    "Empty field value returns empty",
			tagName: "TITLE",
			lang:    "en",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: ""},
				},
			},
			want: "",
		},
		{
			name:    "Unknown tag returns empty",
			tagName: "UNKNOWN",
			lang:    "en",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want: "",
		},
		{
			name:    "Multiple translations - correct language selected",
			tagName: "TITLE",
			lang:    "ja",
			ctx: &Context{
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
					"zh": {Language: "zh", Title: "Chinese Title"},
				},
			},
			want: "Japanese Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.translationFieldValue(tt.tagName, tt.lang, tt.ctx)
			assert.Equal(t, tt.want, got)
		})
	}
}
