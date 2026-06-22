package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ApplyPreset (typed version) tests ---

func TestApplyPreset_Typed(t *testing.T) {
	tests := []struct {
		name        string
		preset      string
		scalar      nfo.MergeStrategy
		array       bool
		wantScalar  nfo.MergeStrategy
		wantArray   bool
		wantErr     bool
		errContains string
	}{
		{
			name:       "conservative preset",
			preset:     "conservative",
			scalar:     nfo.PreferScraper,
			array:      false,
			wantScalar: nfo.PreserveExisting,
			wantArray:  true,
		},
		{
			name:       "gap-fill preset",
			preset:     "gap-fill",
			scalar:     nfo.PreferScraper,
			array:      false,
			wantScalar: nfo.FillMissingOnly,
			wantArray:  true,
		},
		{
			name:       "aggressive preset",
			preset:     "aggressive",
			scalar:     nfo.PreferNFO,
			array:      true,
			wantScalar: nfo.PreferScraper,
			wantArray:  false,
		},
		{
			name:       "empty preset returns input unchanged",
			preset:     "",
			scalar:     nfo.PreferNFO,
			array:      true,
			wantScalar: nfo.PreferNFO,
			wantArray:  true,
		},
		{
			name:        "invalid preset returns error",
			preset:      "does-not-exist",
			scalar:      nfo.PreferNFO,
			array:       true,
			wantScalar:  nfo.PreferNFO,
			wantArray:   true,
			wantErr:     true,
			errContains: "invalid preset",
		},
		{
			name:       "case-insensitive CONSERVATIVE",
			preset:     "CONSERVATIVE",
			scalar:     nfo.PreferScraper,
			array:      false,
			wantScalar: nfo.PreserveExisting,
			wantArray:  true,
		},
		{
			name:       "case-insensitive Gap-Fill",
			preset:     "Gap-Fill",
			scalar:     nfo.PreferScraper,
			array:      false,
			wantScalar: nfo.FillMissingOnly,
			wantArray:  true,
		},
		{
			name:       "case-insensitive AGGRESSIVE",
			preset:     "AGGRESSIVE",
			scalar:     nfo.PreferNFO,
			array:      true,
			wantScalar: nfo.PreferScraper,
			wantArray:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotScalar, gotArray, err := nfo.ApplyPresetTyped(tt.preset, tt.scalar, tt.array)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				// On error, the original values should be returned unchanged.
				assert.Equal(t, tt.wantScalar, gotScalar, "error path should return original scalar")
				assert.Equal(t, tt.wantArray, gotArray, "error path should return original array")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantScalar, gotScalar, "scalar strategy mismatch")
				assert.Equal(t, tt.wantArray, gotArray, "array strategy mismatch")
			}
		})
	}
}

// --- ApplyPresetString tests ---

func TestApplyPreset_String(t *testing.T) {
	tests := []struct {
		name        string
		preset      string
		scalar      string
		array       string
		wantScalar  string
		wantArray   string
		wantErr     bool
		errContains string
	}{
		{
			name:       "conservative preset",
			preset:     "conservative",
			scalar:     "prefer-nfo",
			array:      "merge",
			wantScalar: "preserve-existing",
			wantArray:  "merge",
		},
		{
			name:       "gap-fill preset",
			preset:     "gap-fill",
			scalar:     "prefer-nfo",
			array:      "merge",
			wantScalar: "fill-missing-only",
			wantArray:  "merge",
		},
		{
			name:       "aggressive preset",
			preset:     "aggressive",
			scalar:     "prefer-nfo",
			array:      "merge",
			wantScalar: "prefer-scraper",
			wantArray:  "replace",
		},
		{
			name:       "empty preset returns input unchanged",
			preset:     "",
			scalar:     "prefer-nfo",
			array:      "merge",
			wantScalar: "prefer-nfo",
			wantArray:  "merge",
		},
		{
			name:        "invalid preset returns error",
			preset:      "bogus",
			scalar:      "prefer-nfo",
			array:       "merge",
			wantScalar:  "prefer-nfo",
			wantArray:   "merge",
			wantErr:     true,
			errContains: "invalid preset",
		},
		{
			name:       "case-insensitive CONSERVATIVE",
			preset:     "CONSERVATIVE",
			scalar:     "prefer-scraper",
			array:      "replace",
			wantScalar: "preserve-existing",
			wantArray:  "merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotScalar, gotArray, err := nfo.ApplyPreset(tt.preset, tt.scalar, tt.array)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Equal(t, tt.wantScalar, gotScalar, "error path should return original scalar")
				assert.Equal(t, tt.wantArray, gotArray, "error path should return original array")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantScalar, gotScalar, "scalar strategy mismatch")
				assert.Equal(t, tt.wantArray, gotArray, "array strategy mismatch")
			}
		})
	}
}

// --- ResolveScalarStrategy tests ---

func TestParseScalarStrategy(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    nfo.MergeStrategy
		wantErr bool
	}{
		{
			name:  "empty string defaults to PreferNFO",
			input: "",
			want:  nfo.PreferNFO,
		},
		{
			name:  "preserve-existing",
			input: "preserve-existing",
			want:  nfo.PreserveExisting,
		},
		{
			name:  "fill-missing-only",
			input: "fill-missing-only",
			want:  nfo.FillMissingOnly,
		},
		{
			name:  "prefer-scraper",
			input: "prefer-scraper",
			want:  nfo.PreferScraper,
		},
		{
			name:  "prefer-nfo",
			input: "prefer-nfo",
			want:  nfo.PreferNFO,
		},
		{
			name:  "merge-arrays",
			input: "merge-arrays",
			want:  nfo.MergeArrays,
		},
		{
			name:  "case-insensitive Prefer-Scraper",
			input: "Prefer-Scraper",
			want:  nfo.PreferScraper,
		},
		{
			name:    "invalid strategy returns error",
			input:   "unknown-strategy",
			want:    nfo.PreferNFO,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nfo.ParseScalarStrategy(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unknown scalar strategy")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got, "resolved strategy mismatch")
			}
		})
	}
}

// --- ResolveArrayStrategy tests ---

func TestParseArrayStrategy(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{
			name:  "empty string defaults to merge (true)",
			input: "",
			want:  true,
		},
		{
			name:  "merge returns true",
			input: "merge",
			want:  true,
		},
		{
			name:  "replace returns false",
			input: "replace",
			want:  false,
		},
		{
			name:  "case-insensitive Merge",
			input: "Merge",
			want:  true,
		},
		{
			name:  "case-insensitive Replace",
			input: "REPLACE",
			want:  false,
		},
		{
			name:    "invalid strategy returns error",
			input:   "bogus",
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nfo.ParseArrayStrategy(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unknown array strategy")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got, "resolved array strategy mismatch")
			}
		})
	}
}

// --- ValidateScalarStrategy tests ---

func TestValidateScalarStrategy_Valid(t *testing.T) {
	validStrategies := []string{
		"preserve-existing",
		"fill-missing-only",
		"prefer-scraper",
		"prefer-nfo",
		"merge-arrays",
	}
	for _, tc := range validStrategies {
		t.Run(tc, func(t *testing.T) {
			_, err := nfo.ParseScalarStrategy(tc)
			assert.NoError(t, err, "nfo.ParseScalarStrategy(%q) should not error", tc)
		})
	}
}

func TestValidateScalarStrategy_Invalid(t *testing.T) {
	invalidStrategies := []string{"invalid", "unknown", "prefer_existing"}
	for _, tc := range invalidStrategies {
		t.Run(tc, func(t *testing.T) {
			_, err := nfo.ParseScalarStrategy(tc)
			require.Error(t, err, "nfo.ParseScalarStrategy(%q) should return error", tc)
			assert.Contains(t, err.Error(), "unknown scalar strategy")
		})
	}
}

// --- ValidateArrayStrategy tests ---

func TestValidateArrayStrategy_Valid(t *testing.T) {
	validStrategies := []string{"merge", "replace"}
	for _, tc := range validStrategies {
		t.Run(tc, func(t *testing.T) {
			_, err := nfo.ParseArrayStrategy(tc)
			assert.NoError(t, err, "nfo.ParseArrayStrategy(%q) should not error", tc)
		})
	}
}

func TestValidateArrayStrategy_Invalid(t *testing.T) {
	invalidStrategies := []string{"invalid", "unknown", "append"}
	for _, tc := range invalidStrategies {
		t.Run(tc, func(t *testing.T) {
			_, err := nfo.ParseArrayStrategy(tc)
			require.Error(t, err, "nfo.ParseArrayStrategy(%q) should return error", tc)
			assert.Contains(t, err.Error(), "unknown array strategy")
		})
	}
}

// --- Type alias verification tests ---

func TestMergeStrategyTypeAlias(t *testing.T) {
	// Verify the type alias compiles and works: nfo.MergeStrategy should be assignable from nfo.MergeStrategy.
	var ms nfo.MergeStrategy = nfo.PreferScraper
	assert.Equal(t, nfo.PreferScraper, ms)
}

func TestDataSourceTypeAlias(t *testing.T) {
	var ds nfo.DataSource = nfo.DataSource{Source: "scraper:r18dev"}
	assert.Equal(t, nfo.DataSource{Source: "scraper:r18dev"}, ds)
}
