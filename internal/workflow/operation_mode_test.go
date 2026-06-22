package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/stretchr/testify/assert"
)

func TestParseOperationMode(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		want    operationmode.OperationMode
		wantErr bool
		errMsg  string
	}{
		{
			name:    "organize mode",
			input:   "organize",
			want:    operationmode.OperationModeOrganize,
			wantErr: false,
		},
		{
			name:    "in-place mode",
			input:   "in-place",
			want:    operationmode.OperationModeInPlace,
			wantErr: false,
		},
		{
			name:    "in-place-norenamefolder mode",
			input:   "in-place-norenamefolder",
			want:    operationmode.OperationModeInPlaceNoRenameFolder,
			wantErr: false,
		},
		{
			name:    "metadata-artwork mode",
			input:   "metadata-artwork",
			want:    operationmode.OperationModeMetadataArtwork,
			wantErr: false,
		},
		{
			name:    "preview mode",
			input:   "preview",
			want:    operationmode.OperationModePreview,
			wantErr: false,
		},
		{
			name:    "invalid mode",
			input:   "invalid",
			want:    operationmode.OperationMode(""),
			wantErr: true,
			errMsg:  "invalid operation mode",
		},
		{
			name:    "empty string defaults to organize",
			input:   "",
			want:    operationmode.OperationModeOrganize,
			wantErr: false,
		},
		{
			name:    "ORGANIZE uppercase is normalized",
			input:   "ORGANIZE",
			want:    operationmode.OperationModeOrganize,
			wantErr: false,
		},
		{
			name:    "organize with whitespace",
			input:   "  organize  ",
			want:    operationmode.OperationModeOrganize,
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := operationmode.ParseOperationMode(tc.input)
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
		mode  operationmode.OperationMode
		valid bool
	}{
		{
			name:  "organize is valid",
			mode:  operationmode.OperationModeOrganize,
			valid: true,
		},
		{
			name:  "in-place is valid",
			mode:  operationmode.OperationModeInPlace,
			valid: true,
		},
		{
			name:  "in-place-norenamefolder is valid",
			mode:  operationmode.OperationModeInPlaceNoRenameFolder,
			valid: true,
		},
		{
			name:  "metadata-artwork is valid",
			mode:  operationmode.OperationModeMetadataArtwork,
			valid: true,
		},
		{
			name:  "preview is valid",
			mode:  operationmode.OperationModePreview,
			valid: true,
		},
		{
			name:  "unknown is invalid",
			mode:  operationmode.OperationMode("unknown"),
			valid: false,
		},
		{
			name:  "empty is invalid",
			mode:  operationmode.OperationMode(""),
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
		constant operationmode.OperationMode
		want     string
	}{
		{
			name:     "organize constant value",
			constant: operationmode.OperationModeOrganize,
			want:     "organize",
		},
		{
			name:     "in-place constant value",
			constant: operationmode.OperationModeInPlace,
			want:     "in-place",
		},
		{
			name:     "in-place-norenamefolder constant value",
			constant: operationmode.OperationModeInPlaceNoRenameFolder,
			want:     "in-place-norenamefolder",
		},
		{
			name:     "metadata-artwork constant value",
			constant: operationmode.OperationModeMetadataArtwork,
			want:     "metadata-artwork",
		},
		{
			name:     "preview constant value",
			constant: operationmode.OperationModePreview,
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
			name:  "empty string is valid (defaults to organize)",
			input: "",
			want:  true,
		},
		{
			name:  "case insensitive - Organize is valid",
			input: "Organize",
			want:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := operationmode.ParseOperationMode(tc.input)
			got := err == nil || tc.input == ""
			assert.Equal(t, tc.want, got)
		})
	}
}
