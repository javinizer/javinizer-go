package nfo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseDate_ISO8601(t *testing.T) {
	parsed, err := parseDate("2024-01-15")
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), parsed)
}

func TestParseDate_SlashFormat(t *testing.T) {
	parsed, err := parseDate("2024/01/15")
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), parsed)
}

func TestParseDate_WithTime(t *testing.T) {
	parsed, err := parseDate("2024-01-15 10:30:00")
	assert.NoError(t, err)
	assert.Equal(t, 2024, parsed.Year())
	assert.Equal(t, 1, int(parsed.Month()))
	assert.Equal(t, 15, parsed.Day())
}

func TestParseDate_RFC3339(t *testing.T) {
	parsed, err := parseDate("2024-01-15T10:30:00Z")
	assert.NoError(t, err)
	assert.Equal(t, 2024, parsed.Year())
}

func TestParseDate_RFC3339Nano(t *testing.T) {
	parsed, err := parseDate("2024-01-15T10:30:00.123456789Z")
	assert.NoError(t, err)
	assert.Equal(t, 2024, parsed.Year())
}

func TestParseDate_DashFormat(t *testing.T) {
	parsed, err := parseDate("15-01-2024")
	assert.NoError(t, err)
	assert.Equal(t, 2024, parsed.Year())
}

func TestParseDate_USFormat(t *testing.T) {
	parsed, err := parseDate("01/15/2024")
	assert.NoError(t, err)
	assert.Equal(t, 2024, parsed.Year())
}

func TestParseDate_EmptyString(t *testing.T) {
	_, err := parseDate("")
	assert.Error(t, err)
}

func TestParseDate_WhitespaceOnly(t *testing.T) {
	_, err := parseDate("   ")
	assert.Error(t, err)
}

func TestParseDate_InvalidFormat(t *testing.T) {
	_, err := parseDate("not-a-date")
	assert.Error(t, err)
}

func TestParseDate_LeadingTrailingWhitespace(t *testing.T) {
	parsed, err := parseDate("  2024-03-20  ")
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC), parsed)
}

func TestParseDate_ISO8601WithTimezone(t *testing.T) {
	parsed, err := parseDate("2024-01-15T10:30:00+09:00")
	assert.NoError(t, err)
	assert.Equal(t, 2024, parsed.Year())
}
