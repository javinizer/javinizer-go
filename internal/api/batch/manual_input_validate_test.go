package batch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeManualInput_StripsFormatAndControlChars(t *testing.T) {
	// Cf (format): zero-width space U+200B, zero-width joiner U+200D, BOM U+FEFF.
	// Cc (control): NUL \x00, TAB \t, LF \n, CR \r.
	// Space U+0020 is NOT Cf/Cc (it is Zs) and must be preserved for downstream TrimSpace.
	t.Run("strips Cf/Cc, preserves visible runes", func(t *testing.T) {
		got := sanitizeManualInput("IPX\u200B123\u200D\t\n\r\x00\uFEFF")
		assert.Equal(t, "IPX123", got)
	})

	t.Run("preserves spaces (TrimSpace is downstream)", func(t *testing.T) {
		assert.Equal(t, "  IPX-123  ", sanitizeManualInput("  IPX-123  "))
	})
}

func TestValidateManualURL_RejectsNonHTTPScheme(t *testing.T) {
	for _, input := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://example.com", "gopher://x"} {
		err := validateManualURL(input, nil)
		require.Error(t, err, "non-http(s) scheme %q must be rejected", input)
		assert.Contains(t, err.Error(), "unsupported URL scheme", input)
	}
}

func TestValidateManualURL_RejectsURLNoScraperHandles(t *testing.T) {
	// nil registry => no scrapers CanHandleURL => an http(s) URL must be rejected.
	err := validateManualURL("https://no-handler.example.com/video/123", nil)
	require.Error(t, err, "an http(s) URL no enabled scraper CanHandleURLs must be rejected")
	assert.Contains(t, err.Error(), "no enabled scraper can handle URL")
}

func TestValidateManualURL_AcceptsPlainID(t *testing.T) {
	// A plain ID (no scheme) is never a URL fetch — always accepted.
	assert.NoError(t, validateManualURL("IPX-123", nil))
}

func TestValidateAndSanitizeManualInputs_RejectsOverlongValue(t *testing.T) {
	raw := map[string]string{"/d/a.mp4": strings.Repeat("x", maxManualInputLen+1)}
	_, err := validateAndSanitizeManualInputs(raw, []string{"/d/a.mp4"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestValidateAndSanitizeManualInputs_RejectsMoreInputsThanFiles(t *testing.T) {
	raw := map[string]string{"/d/a.mp4": "IPX-1", "/d/b.mp4": "IPX-2"}
	_, err := validateAndSanitizeManualInputs(raw, []string{"/d/a.mp4"}, nil) // 2 inputs, 1 file
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds files count")
}
