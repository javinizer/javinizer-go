package scraperutil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCleanString(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"already clean", "hello world", "hello world"},
		{"leading/trailing whitespace", "  hello world  ", "hello world"},
		{"multiple spaces", "hello   world", "hello world"},
		{"tabs", "hello\tworld", "hello world"},
		{"newlines", "hello\nworld", "hello world"},
		{"carriage returns", "hello\rworld", "hello world"},
		{"mixed whitespace", "  hello\n\tworld  \r", "hello world"},
		{"non-breaking space", "hello\u00a0world", "hello world"},
		{"non-breaking space with regular spaces", "hello \u00a0 world", "hello world"},
		{"only whitespace", "   \t\n\r  ", ""},
		{"only non-breaking spaces", "\u00a0\u00a0\u00a0", ""},
		{"multiple non-breaking spaces", "hello\u00a0\u00a0world", "hello world"},
		{"space around non-breaking space", "hello \u00a0 world", "hello world"},
		{"tab newlines mixed", "  hello\n\tworld\r\n  ", "hello world"},
		{"newlines and tabs", "line1\n\tline2\n\tline3", "line1 line2 line3"},
		{"carriage return newline", "line1\r\nline2", "line1 line2"},
		{"mixed all types", "\u00a0 hello \t\n \u00a0 world \r\n ", "hello world"},

		{"variant1 compatibility - javbus style", "  hello\u00a0world  ", "hello world"},

		{"variant2 compatibility - r18dev style", "hello\n\rworld", "hello world"},

		{"variant3 compatibility - dmm with tabs", "hello\t\nworld", "hello world"},

		{"variant4 compatibility - javdb style", "  hello world  ", "hello world"},

		{"variant5 compatibility - fc2 style", "hello\r\n\tworld", "hello world"},

		{"whitespace only with newlines", "   \n\r\n   ", ""},
		{"unicode CJK characters preserved", "こんにちは 世界", "こんにちは 世界"},
		{"single character", "a", "a"},
		{"string of only spaces", "     ", ""},
		{"string of only tabs", "\t\t\t", ""},
		{"string of only newlines", "\n\n\n", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanString(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNormalizeLanguage(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{"ja lowercase", "ja", "ja"},
		{"ja uppercase", "JA", "ja"},
		{"ja mixed case", "Ja", "ja"},
		{"ja with whitespace", " ja ", "ja"},
		{"en lowercase", "en", "en"},
		{"en uppercase", "EN", "en"},
		{"en mixed case", "En", "en"},
		{"zh", "zh", "zh"},
		{"zh uppercase", "ZH", "zh"},
		{"cn maps to zh", "cn", "zh"},
		{"cn uppercase", "CN", "zh"},
		{"tw maps to zh", "tw", "zh"},
		{"tw uppercase", "TW", "zh"},
		{"empty defaults to en", "", "en"},
		{"unknown language defaults to en", "fr", "en"},
		{"korean defaults to en", "ko", "en"},
		{"japanese word defaults to en", " japanese", "en"},
		{"english word defaults to en", " english", "en"},

		{"variant1 - r18dev binary ja check", "ja", "ja"},
		{"variant1 - r18dev non-ja returns en", "en", "en"},

		{"variant2 - javbus three-way en", "en", "en"},
		{"variant2 - javbus three-way ja", "ja", "ja"},
		{"variant2 - javbus three-way zh", "zh", "zh"},
		{"variant2 - javbus cn to zh", "cn", "zh"},

		{"variant3 - caribbeancom en returns en", "en", "en"},
		{"variant3 - caribbeancom non-en defaults to en (not ja)", "ja", "ja"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeLanguage(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseDate(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  *time.Time
	}{
		{"ISO format", "2024-01-15", datePtr(2024, 1, 15)},
		{"slash format", "2024/01/15", datePtr(2024, 1, 15)},
		{"dot format", "2024.01.15", datePtr(2024, 1, 15)},
		{"US format MM-DD-YYYY", "01-15-2024", datePtr(2024, 1, 15)},
		{"empty string", "", nil},
		{"invalid date", "not a date", nil},
		{"whitespace tolerance", "  2024-01-15  ", datePtr(2024, 1, 15)},
		{"whitespace with tabs", "2024-01-15\t", datePtr(2024, 1, 15)},
		{"date with trailing text", "2024-01-15 extra", nil},
		{"single digit month", "2024-1-05", nil},
		{"year only", "2024", nil},
		{"ISO with different date", "2023-12-31", datePtr(2023, 12, 31)},
		{"feb 29 leap year", "2024-02-29", datePtr(2024, 2, 29)},

		{"variant1 - javbus ISO", "2024-01-15", datePtr(2024, 1, 15)},
		{"variant1 - javbus slash", "2024/01/15", datePtr(2024, 1, 15)},
		{"variant1 - javbus dot", "2024.01.15", datePtr(2024, 1, 15)},

		{"variant2 - aventertainment US format", "01-15-2024", datePtr(2024, 1, 15)},

		{"date with non-breaking space", "2024\u00a0-\u00a001\u00a0-\u00a015", nil},
		{"date with tabs around", "\t2024-01-15\t", datePtr(2024, 1, 15)},
		{"slash format with whitespace", " 2024/01/15 ", datePtr(2024, 1, 15)},
		{"dot format with whitespace", "2024.01.15 ", datePtr(2024, 1, 15)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseDate(tc.input)
			if tc.want == nil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, tc.want.Year(), got.Year())
				assert.Equal(t, tc.want.Month(), got.Month())
				assert.Equal(t, tc.want.Day(), got.Day())
			}
		})
	}
}

func TestResolveURL(t *testing.T) {
	testCases := []struct {
		name string
		base string
		raw  string
		want string
	}{
		{"empty string returns empty", "https://example.com", "", ""},
		{"absolute https URL passthrough", "https://example.com", "https://other.com/page", "https://other.com/page"},
		{"absolute http URL passthrough", "https://example.com", "http://other.com/page", "http://other.com/page"},
		{"protocol-relative URL", "https://example.com", "//cdn.example.com/img.jpg", "https://cdn.example.com/img.jpg"},
		{"absolute path resolved against base", "https://example.com/page/item", "/img/photo.jpg", "https://example.com/img/photo.jpg"},
		{"absolute path with base trailing slash", "https://example.com/", "/img/photo.jpg", "https://example.com/img/photo.jpg"},
		{"relative path resolved against base", "https://example.com/dir/", "photo.jpg", "https://example.com/dir/photo.jpg"},
		{"relative path with subdirectory", "https://example.com/dir/sub/", "photo.jpg", "https://example.com/dir/sub/photo.jpg"},
		{"base URL with query string", "https://example.com/page?q=test", "/img/photo.jpg", "https://example.com/img/photo.jpg"},
		{"raw URL with whitespace", "https://example.com", "  https://other.com/img.jpg  ", "https://other.com/img.jpg"},
		{"base URL with port", "https://example.com:8080", "/img/photo.jpg", "https://example.com:8080/img/photo.jpg"},

		{"variant1 - javbus style absolute path clears query", "https://example.com/page?q=1", "/img/photo.jpg", "https://example.com/img/photo.jpg"},
		{"variant1 - javbus style relative path", "https://example.com/dir/page", "photo.jpg", "https://example.com/dir/photo.jpg"},

		{"variant2 - javdb string concat absolute path", "https://example.com", "/img/photo.jpg", "https://example.com/img/photo.jpg"},

		{"variant3 - aventertainment ResolveReference relative", "https://example.com/dir/", "photo.jpg", "https://example.com/dir/photo.jpg"},

		{"whitespace only raw returns empty", "https://example.com/foo/bar", "   ", ""},
		{"parent directory traversal", "https://example.com/foo/bar", "../baz", "https://example.com/baz"},
		{"absolute path clears fragment", "https://example.com/page#section", "/img/photo.jpg", "https://example.com/img/photo.jpg"},
		{"relative path without trailing slash", "https://example.com/dir/page", "photo.jpg", "https://example.com/dir/photo.jpg"},
		{"libredmm style relative path", "https://example.com/foo/bar", "qux", "https://example.com/foo/qux"},

		{"caribbeancom fragment cleared on absolute path", "https://example.com/page#anchor", "/img/photo.jpg", "https://example.com/img/photo.jpg"},

		{"javbus absolute path clears query", "https://www.javbus.com/en/ABC-123?page=2", "/pics/1.jpg", "https://www.javbus.com/pics/1.jpg"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveURL(tc.base, tc.raw)
			assert.Equal(t, tc.want, got)
		})
	}
}

func datePtr(year int, month time.Month, day int) *time.Time {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return &t
}
