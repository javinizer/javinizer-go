package aventertainment

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRuntime(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "clock format", input: "0:56:34", want: 56},
		{name: "clock format with hours", input: "1:23:45", want: 83},
		{name: "empty", input: "", want: 0},
		{name: "invalid", input: "not a time", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseRuntime(tt.input))
		})
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "format 1", input: "02/03/2026", want: "2026-02-03"},
		{name: "format 2", input: "2026/02/03", want: "2026-02-03"},
		{name: "empty", input: "", want: ""},
		{name: "invalid", input: "not a date", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDate(tt.input)
			if tt.want == "" {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.want, result.Format("2006-01-02"))
			}
		})
	}
}

func TestIsProductIDLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "Japanese", input: "商品番号", want: true},
		{name: "English", input: "item#", want: true},
		{name: "invalid", input: "not a product id", want: false},
		{name: "empty", input: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isProductIDLabel(tt.input))
		})
	}
}

func TestNormalizeInfoLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "with colon", input: "商品番号:", want: "商品番号"},
		{name: "with space", input: "商品番号 ", want: "商品番号"},
		{name: "with fullwidth colon", input: "商品番号：", want: "商品番号"},
		{name: "already clean", input: "商品番号", want: "商品番号"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeInfoLabel(tt.input))
		})
	}
}

func TestCleanString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "multiple spaces", input: "hello   world", want: "hello world"},
		{name: "newlines", input: "hello\nworld", want: "hello world"},
		{name: "tabs", input: "hello\tworld", want: "hello world"},
		{name: "leading/trailing", input: "  hello world  ", want: "hello world"},
		{name: "mixed whitespace", input: "  hello\n\tworld  ", want: "hello world"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanString(tt.input))
		})
	}
}
