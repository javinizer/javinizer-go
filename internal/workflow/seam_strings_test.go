package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/assert"
)

func TestResolveOperationMode_Validation(t *testing.T) {
	validCases := []string{"", "organize", "in-place", "in-place-norenamefolder", "metadata-artwork", "preview"}
	for _, tc := range validCases {
		_, err := ResolveOperationMode(tc)
		assert.NoError(t, err, "ResolveOperationMode(%q) should not error", tc)
	}

	invalidCases := []string{"invalid", "unknown"}
	for _, tc := range invalidCases {
		_, err := ResolveOperationMode(tc)
		assert.Error(t, err, "ResolveOperationMode(%q) should return error", tc)
	}
}

func TestResolveOperationMode_CaseInsensitive(t *testing.T) {
	_, err := ResolveOperationMode("Organize")
	assert.NoError(t, err)
	_, err = ResolveOperationMode("IN-PLACE")
	assert.NoError(t, err)
	_, err = ResolveOperationMode("Preview")
	assert.NoError(t, err)
}

func TestResolveLinkMode_Validation(t *testing.T) {
	validCases := []string{"", "none", "hard", "soft"}
	for _, tc := range validCases {
		_, err := ResolveLinkMode(tc)
		assert.NoError(t, err, "ResolveLinkMode(%q) should not error", tc)
	}

	invalidCases := []string{"invalid", "UNKNOWN"}
	for _, tc := range invalidCases {
		_, err := ResolveLinkMode(tc)
		assert.Error(t, err, "ResolveLinkMode(%q) should return error", tc)
	}
}

func TestResolveLinkMode_CaseInsensitive(t *testing.T) {
	_, err := ResolveLinkMode("Hard")
	assert.NoError(t, err)
	_, err = ResolveLinkMode("SOFT")
	assert.NoError(t, err)
	_, err = ResolveLinkMode("None")
	assert.NoError(t, err)
}

func TestResolvePreset(t *testing.T) {
	validCases := []string{"", "conservative", "gap-fill", "aggressive"}
	for _, tc := range validCases {
		_, err := ResolvePreset(tc)
		assert.NoError(t, err, "ResolvePreset(%q) should not error", tc)
	}

	invalidCases := []string{"invalid", "unknown"}
	for _, tc := range invalidCases {
		_, err := ResolvePreset(tc)
		assert.Error(t, err, "ResolvePreset(%q) should return error", tc)
	}
}

func TestResolvePreset_CaseInsensitive(t *testing.T) {
	resolved, err := ResolvePreset("Conservative")
	assert.NoError(t, err)
	assert.Equal(t, "conservative", resolved)
	resolved, err = ResolvePreset("GAP-FILL")
	assert.NoError(t, err)
	assert.Equal(t, "gap-fill", resolved)
	resolved, err = ResolvePreset("Aggressive")
	assert.NoError(t, err)
	assert.Equal(t, "aggressive", resolved)
}

func TestValidateScalarStrategy(t *testing.T) {
	validCases := []string{"", "prefer-scraper", "prefer-nfo", "preserve-existing", "fill-missing-only", "merge-arrays"}
	for _, tc := range validCases {
		_, err := nfo.ParseScalarStrategy(tc)
		assert.NoError(t, err, "ParseScalarStrategy(%q) should not error", tc)
	}

	invalidCases := []string{"invalid", "unknown"}
	for _, tc := range invalidCases {
		_, err := nfo.ParseScalarStrategy(tc)
		assert.Error(t, err, "ParseScalarStrategy(%q) should return error", tc)
	}
}

func TestValidateArrayStrategy(t *testing.T) {
	validCases := []string{"", "merge", "replace"}
	for _, tc := range validCases {
		_, err := nfo.ParseArrayStrategy(tc)
		assert.NoError(t, err, "ParseArrayStrategy(%q) should not error", tc)
	}

	invalidCases := []string{"invalid", "unknown"}
	for _, tc := range invalidCases {
		_, err := nfo.ParseArrayStrategy(tc)
		assert.Error(t, err, "ParseArrayStrategy(%q) should return error", tc)
	}
}

func TestResolveOperationMode(t *testing.T) {
	// CR-02: Empty input must return OperationModeOrganize (not zero-value)
	mode, err := ResolveOperationMode("")
	assert.NoError(t, err)
	assert.Equal(t, operationmode.OperationModeOrganize, mode)
	assert.True(t, mode.IsValid(), "ResolveOperationMode(\"\") must return a valid OperationMode")

	validCases := []string{"organize", "in-place", "in-place-norenamefolder", "metadata-artwork", "preview"}
	for _, tc := range validCases {
		mode, err := ResolveOperationMode(tc)
		assert.NoError(t, err, "ResolveOperationMode(%q) should not error", tc)
		assert.True(t, mode.IsValid(), "ResolveOperationMode(%q) should return valid mode", tc)
	}

	mode, err = ResolveOperationMode("Organize")
	assert.NoError(t, err)
	assert.Equal(t, operationmode.OperationModeOrganize, mode)

	mode, err = ResolveOperationMode("invalid")
	assert.Error(t, err)
	assert.Equal(t, operationmode.OperationModeOrganize, mode, "error case should return OperationModeOrganize as default")
}

func TestResolveSeamStrings(t *testing.T) {
	t.Run("defaults for empty input", func(t *testing.T) {
		resolved, err := ResolveSeamStrings(SeamStringsInput{})
		assert.NoError(t, err)
		assert.Equal(t, operationmode.OperationModeOrganize, resolved.OperationMode)
		assert.Equal(t, organizer.LinkModeNone, resolved.LinkMode)
		assert.Equal(t, nfo.PreferNFO, resolved.ScalarStrategy)
		assert.True(t, resolved.ArrayStrategy, "default ArrayStrategy should be merge (true)")
	})

	t.Run("all fields specified", func(t *testing.T) {
		resolved, err := ResolveSeamStrings(SeamStringsInput{
			OperationMode:  "in-place",
			LinkMode:       "hard",
			ScalarStrategy: "prefer-scraper",
			ArrayStrategy:  "replace",
		})
		assert.NoError(t, err)
		assert.Equal(t, operationmode.OperationModeInPlace, resolved.OperationMode)
		assert.Equal(t, organizer.LinkModeHard, resolved.LinkMode)
		assert.Equal(t, nfo.PreferScraper, resolved.ScalarStrategy)
		assert.False(t, resolved.ArrayStrategy, "replace should be false")
	})

	t.Run("preset overrides strategies", func(t *testing.T) {
		resolved, err := ResolveSeamStrings(SeamStringsInput{
			ScalarStrategy: "prefer-nfo",
			ArrayStrategy:  "merge",
			Preset:         "aggressive",
		})
		assert.NoError(t, err)
		assert.Equal(t, nfo.PreferScraper, resolved.ScalarStrategy)
		assert.False(t, resolved.ArrayStrategy, "aggressive preset should set ArrayStrategy to replace")
	})

	t.Run("validation errors accumulated", func(t *testing.T) {
		_, err := ResolveSeamStrings(SeamStringsInput{
			OperationMode:  "invalid-mode",
			LinkMode:       "invalid-link",
			ScalarStrategy: "invalid-scalar",
			ArrayStrategy:  "invalid-array",
			Preset:         "invalid-preset",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid operation mode")
		assert.Contains(t, err.Error(), "invalid link mode")
		assert.Contains(t, err.Error(), "unknown scalar strategy")
		assert.Contains(t, err.Error(), "unknown array strategy")
		assert.Contains(t, err.Error(), "invalid preset")
	})

	t.Run("partial input resolves correctly", func(t *testing.T) {
		resolved, err := ResolveSeamStrings(SeamStringsInput{
			LinkMode: "soft",
		})
		assert.NoError(t, err)
		assert.Equal(t, operationmode.OperationModeOrganize, resolved.OperationMode, "default op mode")
		assert.Equal(t, organizer.LinkModeSoft, resolved.LinkMode)
		assert.Equal(t, nfo.PreferNFO, resolved.ScalarStrategy, "default scalar")
		assert.True(t, resolved.ArrayStrategy, "default array")
	})
}
