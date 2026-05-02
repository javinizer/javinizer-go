package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
			name:    "in-place-norenamefolder mode",
			input:   "in-place-norenamefolder",
			want:    OperationModeInPlaceNoRenameFolder,
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
			name:    "empty string is invalid",
			input:   "",
			want:    OperationMode(""),
			wantErr: true,
			errMsg:  "invalid operation mode",
		},
		{
			name:    "ORGANIZE uppercase is normalized",
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
			name:  "in-place-norenamefolder is valid",
			mode:  OperationModeInPlaceNoRenameFolder,
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

func TestOperationMode_Constants(t *testing.T) {
	testCases := []struct {
		name     string
		constant OperationMode
		want     string
	}{
		{
			name:     "organize constant value",
			constant: OperationModeOrganize,
			want:     "organize",
		},
		{
			name:     "in-place constant value",
			constant: OperationModeInPlace,
			want:     "in-place",
		},
		{
			name:     "in-place-norenamefolder constant value",
			constant: OperationModeInPlaceNoRenameFolder,
			want:     "in-place-norenamefolder",
		},
		{
			name:     "metadata-artwork constant value",
			constant: OperationModeMetadataArtwork,
			want:     "metadata-artwork",
		},
		{
			name:     "preview constant value",
			constant: OperationModePreview,
			want:     "preview",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, string(tc.constant))
		})
	}
}

func TestIsValidOperationMode(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "organize is valid",
			input: "organize",
			want:  true,
		},
		{
			name:  "in-place is valid",
			input: "in-place",
			want:  true,
		},
		{
			name:  "in-place-norenamefolder is valid",
			input: "in-place-norenamefolder",
			want:  true,
		},
		{
			name:  "metadata-artwork is valid",
			input: "metadata-artwork",
			want:  true,
		},
		{
			name:  "preview is valid",
			input: "preview",
			want:  true,
		},
		{
			name:  "invalid mode",
			input: "invalid",
			want:  false,
		},
		{
			name:  "empty string is invalid",
			input: "",
			want:  false,
		},
		{
			name:  "case sensitive - Organize is invalid",
			input: "Organize",
			want:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsValidOperationMode(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}
