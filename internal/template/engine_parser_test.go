package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeLanguageList(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "Nil input",
			input: nil,
			want:  nil,
		},
		{
			name:  "Empty input",
			input: []string{},
			want:  nil,
		},
		{
			name:  "Single valid item",
			input: []string{"en"},
			want:  []string{"en"},
		},
		{
			name:  "Single valid item uppercase",
			input: []string{"EN"},
			want:  []string{"en"},
		},
		{
			name:  "Multiple valid items",
			input: []string{"en", "ja", "zh"},
			want:  []string{"en", "ja", "zh"},
		},
		{
			name:  "Duplicates removed",
			input: []string{"en", "en", "ja"},
			want:  []string{"en", "ja"},
		},
		{
			name:  "Duplicates with different casing normalized",
			input: []string{"en", "EN", "En"},
			want:  []string{"en"},
		},
		{
			name:  "First occurrence kept for duplicates",
			input: []string{"ja", "en", "ja", "en"},
			want:  []string{"ja", "en"},
		},
		{
			name:  "Invalid codes filtered out",
			input: []string{"en", "eng", "jpn", "ja"},
			want:  []string{"en", "ja"},
		},
		{
			name:  "All invalid codes returns empty slice",
			input: []string{"eng", "jpn", "xyz"},
			want:  []string{},
		},
		{
			name:  "Empty strings filtered",
			input: []string{"", "en", ""},
			want:  []string{"en"},
		},
		{
			name:  "Whitespace trimmed and normalized",
			input: []string{"  en  ", "  ja  "},
			want:  []string{"en", "ja"},
		},
		{
			name:  "Whitespace only filtered",
			input: []string{"   ", "en", "  "},
			want:  []string{"en"},
		},
		{
			name:  "Region suffixes normalized",
			input: []string{"en-US", "ja_JP", "zh-Hant"},
			want:  []string{"en", "ja", "zh"},
		},
		{
			name:  "Duplicates after normalization",
			input: []string{"en-US", "en-GB", "en"},
			want:  []string{"en"},
		},
		{
			name:  "Mixed valid and invalid",
			input: []string{"en", "invalid", "ja", "123", "zh"},
			want:  []string{"en", "ja", "zh"},
		},
		{
			name:  "Ordering preserved",
			input: []string{"zh", "en", "ja", "ko"},
			want:  []string{"zh", "en", "ja", "ko"},
		},
		{
			name:  "Single character filtered",
			input: []string{"x", "en", "y"},
			want:  []string{"en"},
		},
		{
			name:  "Three letter codes returns empty slice",
			input: []string{"eng", "jpn", "zho"},
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLanguageList(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseModifier_LanguageSpecNormalization(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name     string
		modifier string
		want     string // Expected normalized language spec
	}{
		{"Valid lowercase en", "en", "en"},
		{"Valid lowercase ja", "ja", "ja"},
		{"Valid lowercase zh", "zh", "zh"},
		{"Valid uppercase EN normalized", "EN", "en"},
		{"Valid mixed case En normalized", "En", "en"},
		{"Valid uppercase JA normalized", "JA", "ja"},
		{"Invalid 3-letter eng", "eng", ""},
		{"Invalid 3-letter jpn", "jpn", ""},
		{"Invalid single letter", "x", ""},
		{"Invalid numeric", "123", ""},
		{"Invalid empty", "", ""},
		{"Invalid with region en-US normalized", "en-US", "en"},
		{"Invalid with region ja_JP normalized", "ja_JP", "ja"},
		{"Invalid with script zh-Hant normalized", "zh-Hant", "zh"},
		{"Whitespace trimmed and normalized", "  en  ", "en"},
		{"Invalid numeric letter mix", "1a", ""},
		{"Invalid letter numeric mix", "a1", ""},
		{"Valid de", "de", "de"},
		{"Valid fr", "fr", "fr"},
		{"Valid es", "es", "es"},
		{"Valid pt", "pt", "pt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.parseModifier("TITLE", tt.modifier)
			assert.Equal(t, tt.want, got.languageSpec)
			if tt.want != "" {
				assert.True(t, got.isLanguage)
			}
		})
	}
}

func TestParseModifier(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name           string
		tagName        string
		modifier       string
		wantIsLanguage bool
		wantLangSpec   string
		wantLegacy     string
		wantRejected   bool
	}{
		{"Empty modifier", "TITLE", "", false, "", "", false},
		{"Valid language spec en", "TITLE", "en", true, "en", "", false},
		{"Valid language spec ja", "TITLE", "ja", true, "ja", "", false},
		{"Valid language spec zh", "TITLE", "zh", true, "zh", "", false},
		{"Valid uppercase EN normalized", "TITLE", "EN", true, "en", "", false},
		{"Valid mixed case En normalized", "TITLE", "En", true, "en", "", false},
		{"Invalid 3-letter eng rejected", "TITLE", "eng", false, "", "", true},
		{"Numeric modifier for TITLE preserved", "TITLE", "50", false, "", "50", false},
		{"Numeric modifier for TITLE zero", "TITLE", "0", false, "", "0", false},
		{"Non-TITLE tag with numeric treated as legacy", "DIRECTOR", "50", false, "", "50", false},
		{"Non-TITLE tag with language spec", "DIRECTOR", "en", true, "en", "", false},
		{"MAKER tag with language spec", "MAKER", "ja", true, "ja", "", false},
		{"STUDIO tag with language spec", "STUDIO", "zh", true, "zh", "", false},
		{"LABEL tag with language spec", "LABEL", "en", true, "en", "", false},
		{"SERIES tag with language spec", "SERIES", "ja", true, "ja", "", false},
		{"DESCRIPTION tag with language spec", "DESCRIPTION", "en", true, "en", "", false},
		{"ORIGINALTITLE tag with language spec", "ORIGINALTITLE", "ja", true, "ja", "", false},
		{"ID tag with language spec (not translatable)", "ID", "en", true, "en", "", false},
		{"Valid region suffix normalized", "TITLE", "en-US", true, "en", "", false},
		{"Valid whitespace trimmed and normalized", "TITLE", "  en  ", true, "en", "", false},
		{"Case modifier uppercase", "ID", "UPPERCASE", false, "", "UPPERCASE", false},
		{"Case modifier lowercase", "ID", "LOWERCASE", false, "", "LOWERCASE", false},
		{"TITLE with case modifier treated as legacy", "TITLE", "UPPERCASE", false, "", "UPPERCASE", false},
		{"TITLE with UPPERCASE and language fallback", "TITLE", "UPPER", false, "", "UPPER", false},
		{"TITLE with valid numeric", "TITLE", "100", false, "", "100", false},
		{"TITLE with negative numeric treated as legacy", "TITLE", "-50", false, "", "-50", false},
		{"TITLE with alphanumeric treated as legacy", "TITLE", "50abc", false, "", "50abc", false},
		{"RELEASEDATE with format pattern", "RELEASEDATE", "YYYY-MM-DD", false, "", "YYYY-MM-DD", false},
		{"ACTORS with delimiter", "ACTORS", " and ", false, "", " and ", false},
		{"GENRES with delimiter", "GENRES", "|", false, "", "|", false},
		{"INDEX with padding", "INDEX", "3", false, "", "3", false},
		{"PART with padding", "PART", "2", false, "", "2", false},
		{"Valid language ko", "TITLE", "ko", true, "ko", "", false},
		{"Valid language de", "TITLE", "de", true, "de", "", false},
		{"Valid language fr", "TITLE", "fr", true, "fr", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.parseModifier(tt.tagName, tt.modifier)
			assert.Equal(t, tt.wantIsLanguage, got.isLanguage, "isLanguage mismatch")
			assert.Equal(t, tt.wantLangSpec, got.languageSpec, "languageSpec mismatch")
			assert.Equal(t, tt.wantLegacy, got.legacyModifier, "legacyModifier mismatch")
			assert.Equal(t, tt.wantRejected, got.rejectedLanguage, "rejectedLanguage mismatch")
		})
	}
}

func TestIsNumericModifier(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name     string
		modifier string
		want     bool
	}{
		{"Empty modifier", "", false},
		{"Valid positive integer 50", "50", true},
		{"Valid positive integer 1", "1", true},
		{"Valid positive integer 100", "100", true},
		{"Valid large integer 1000000", "1000000", true},
		{"Invalid zero", "0", false},
		{"Invalid negative", "-50", false},
		{"Invalid negative one", "-1", false},
		{"Invalid alphabetic", "abc", false},
		{"Invalid alphanumeric", "50abc", false},
		{"Invalid decimal", "50.5", false},
		{"Invalid whitespace", " 50 ", false},
		{"Invalid empty after trim", "   ", false},
		{"Invalid special chars", "50!", false},
		{"Invalid leading zero", "01", true},
		{"Invalid hexadecimal", "0x50", false},
		{"Valid single digit", "5", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.isNumericModifier(tt.modifier)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsTranslatableTag(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name    string
		tagName string
		want    bool
	}{
		{"TITLE is translatable", "TITLE", true},
		{"title lowercase is translatable", "title", false},
		{"ORIGINALTITLE is translatable", "ORIGINALTITLE", true},
		{"DIRECTOR is translatable", "DIRECTOR", true},
		{"MAKER is translatable", "MAKER", true},
		{"STUDIO is translatable", "STUDIO", true},
		{"LABEL is translatable", "LABEL", true},
		{"SERIES is translatable", "SERIES", true},
		{"DESCRIPTION is translatable", "DESCRIPTION", true},
		{"ID is not translatable", "ID", false},
		{"CONTENTID is not translatable", "CONTENTID", false},
		{"YEAR is not translatable", "YEAR", false},
		{"RELEASEDATE is not translatable", "RELEASEDATE", false},
		{"RUNTIME is not translatable", "RUNTIME", false},
		{"ACTORS is not translatable", "ACTORS", false},
		{"ACTRESSES is not translatable", "ACTRESSES", false},
		{"GENRES is not translatable", "GENRES", false},
		{"FILENAME is not translatable", "FILENAME", false},
		{"INDEX is not translatable", "INDEX", false},
		{"FIRSTNAME is not translatable", "FIRSTNAME", false},
		{"LASTNAME is not translatable", "LASTNAME", false},
		{"ACTORNAME is not translatable", "ACTORNAME", false},
		{"RESOLUTION is not translatable", "RESOLUTION", false},
		{"PART is not translatable", "PART", false},
		{"DISC is not translatable", "DISC", false},
		{"PARTSUFFIX is not translatable", "PARTSUFFIX", false},
		{"MULTIPART is not translatable", "MULTIPART", false},
		{"Unknown tag is not translatable", "UNKNOWN", false},
		{"Empty tag is not translatable", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.isTranslatableTag(tt.tagName)
			assert.Equal(t, tt.want, got)
		})
	}
}
