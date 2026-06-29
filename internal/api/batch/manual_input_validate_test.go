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

func TestValidateAndSanitizeManualInputs_RejectsKeyNotInFiles(t *testing.T) {
	testCases := []struct {
		name     string
		raw      map[string]string
		files    []string
		wantErr  bool
		contains string
	}{
		{
			name:     "orphan key passes count check but is not in files",
			raw:      map[string]string{"/orphan/path": "IPX-123"},
			files:    []string{"/real/a.mp4", "/real/b.mp4"},
			wantErr:  true,
			contains: "/orphan/path",
		},
		{
			name:    "key in files passes (regression guard)",
			raw:     map[string]string{"/real/a.mp4": "IPX-123"},
			files:   []string{"/real/a.mp4", "/real/b.mp4"},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateAndSanitizeManualInputs(tc.raw, tc.files, nil)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.contains)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.raw, got)
			}
		})
	}
}

// A whitespace-padded URL is trimmed before the scheme/CanHandleURL check, so
// an unhandleable URL with edge whitespace is rejected (400) just like its
// trimmed counterpart — previously the leading space made url.Parse return
// Scheme="" so it took the plain-ID branch and bypassed the check.
func TestValidateAndSanitizeManualInputs_TrimsBeforeURLValidation(t *testing.T) {
	raw := map[string]string{"/d/a.mp4": "  https://no-handler.example.com/v/123  "}
	got, err := validateAndSanitizeManualInputs(raw, []string{"/d/a.mp4"}, nil)
	require.Error(t, err, "whitespace-padded unhandleable URL must be rejected after trim")
	assert.Contains(t, err.Error(), "no enabled scraper can handle URL")
	assert.Nil(t, got)
}

// Empty-after-trim entries are dropped from the sanitized map (not stored as "")
// so the override map carries no no-op entries.
func TestValidateAndSanitizeManualInputs_DropsEmptyAfterTrim(t *testing.T) {
	raw := map[string]string{"/d/a.mp4": "   "}
	got, err := validateAndSanitizeManualInputs(raw, []string{"/d/a.mp4"}, nil)
	require.NoError(t, err)
	_, present := got["/d/a.mp4"]
	assert.False(t, present, "empty-after-trim entry should be dropped, not stored as \"\"")
}

// The "no enabled scraper" 400 error redacts the URL query so a token in the
// echoed URL is not leaked back to the client.
func TestValidateManualURL_RedactsQueryInError(t *testing.T) {
	err := validateManualURL("https://no-handler.example.com/v/123?token=secret", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no enabled scraper can handle URL")
	assert.NotContains(t, err.Error(), "token=secret", "query token must be redacted from the error")
	assert.NotContains(t, err.Error(), "secret")
}

// The length cap is a CHARACTER (rune) limit, not a byte limit. A multibyte
// input (e.g. CJK IDs) at the rune cap must pass even though its byte length
// exceeds the cap; one rune over must fail.
func TestValidateAndSanitizeManualInputs_LengthCapIsRuneCount(t *testing.T) {
	// '中' is 3 bytes; 4096 runes = 12288 bytes, well over the old byte cap.
	atCap := strings.Repeat("中", maxManualInputLen)     // exactly the cap → pass
	overCap := strings.Repeat("中", maxManualInputLen+1) // one rune over → fail

	if _, err := validateAndSanitizeManualInputs(map[string]string{"/d/a.mp4": atCap}, []string{"/d/a.mp4"}, nil); err != nil {
		t.Fatalf("input at the rune cap should pass, got: %v", err)
	}
	_, err := validateAndSanitizeManualInputs(map[string]string{"/d/a.mp4": overCap}, []string{"/d/a.mp4"}, nil)
	require.Error(t, err, "one rune over the cap should fail even though byte length >> cap")
	assert.Contains(t, err.Error(), "exceeds")
}

// A malformed URL triggering a url.Parse error must not echo the raw input
// (which may carry a query token) back in the 400.
func TestValidateManualURL_RedactsParseError(t *testing.T) {
	// A control char in the host forces url.Parse to fail while keeping a
	// query-like token; the error must not contain the token.
	err := validateManualURL("https://exa\tmple.com/v/123?token=secret", nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "token=secret", "parse error must not echo the raw query token")
}
