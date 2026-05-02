package organizer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/types"
)

func TestParseOperationMode(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		want    OperationMode
		wantErr bool
		errMsg  string
	}{
		{
			name:    "organize mode",
			input:   "organize",
			want:    OperationModeOrganize,
			wantErr: false,
		},
		{
			name:    "in-place mode",
			input:   "in-place",
			want:    OperationModeInPlace,
			wantErr: false,
		},
		{
			name:    "metadata-artwork mode",
			input:   "metadata-artwork",
			want:    OperationModeMetadataArtwork,
			wantErr: false,
		},
		{
			name:    "preview mode",
			input:   "preview",
			want:    OperationModePreview,
			wantErr: false,
		},
		{
			name:    "invalid mode",
			input:   "invalid",
			want:    OperationMode(""),
			wantErr: true,
			errMsg:  "invalid operation mode",
		},
		{
			name:    "organize uppercase normalized",
			input:   "ORGANIZE",
			want:    OperationModeOrganize,
			wantErr: false,
		},
		{
			name:    "organize with whitespace",
			input:   "  organize  ",
			want:    OperationModeOrganize,
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseOperationMode(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestOperationMode_IsValid(t *testing.T) {
	testCases := []struct {
		name  string
		mode  OperationMode
		valid bool
	}{
		{
			name:  "organize is valid",
			mode:  OperationModeOrganize,
			valid: true,
		},
		{
			name:  "in-place is valid",
			mode:  OperationModeInPlace,
			valid: true,
		},
		{
			name:  "metadata-artwork is valid",
			mode:  OperationModeMetadataArtwork,
			valid: true,
		},
		{
			name:  "preview is valid",
			mode:  OperationModePreview,
			valid: true,
		},
		{
			name:  "unknown is invalid",
			mode:  OperationMode("unknown"),
			valid: false,
		},
		{
			name:  "empty is invalid",
			mode:  OperationMode(""),
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.mode.IsValid()
			assert.Equal(t, tc.valid, got)
		})
	}
}

func TestOrganizerAliases_MatchTypesPackage(t *testing.T) {
	t.Run("OperationMode type alias matches types package", func(t *testing.T) {
		var m OperationMode = types.OperationModeOrganize
		assert.Equal(t, types.OperationModeOrganize, m)
	})

	t.Run("OperationModeOrganize constant matches types package", func(t *testing.T) {
		assert.Equal(t, types.OperationModeOrganize, OperationModeOrganize)
	})

	t.Run("OperationModeInPlace constant matches types package", func(t *testing.T) {
		assert.Equal(t, types.OperationModeInPlace, OperationModeInPlace)
	})

	t.Run("OperationModeMetadataArtwork constant matches types package", func(t *testing.T) {
		assert.Equal(t, types.OperationModeMetadataArtwork, OperationModeMetadataArtwork)
	})

	t.Run("OperationModePreview constant matches types package", func(t *testing.T) {
		assert.Equal(t, types.OperationModePreview, OperationModePreview)
	})

	t.Run("ParseOperationMode delegates to types package", func(t *testing.T) {
		got, err := ParseOperationMode("organize")
		assert.NoError(t, err)
		assert.Equal(t, OperationModeOrganize, got)
	})
}
