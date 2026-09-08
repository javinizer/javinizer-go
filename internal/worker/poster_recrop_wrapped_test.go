package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// codex r11 P2: production recrop refusal text reaches Error wrapped by
// upstream layers ("download failed: apply poster: poster requires a fresh
// crop: source identity unavailable"). A plain HasPrefix gate misses this
// form, leaving the refusal text on the row even after the marker resolves.
func TestClearPosterRecrop_WrappedRefusalText(t *testing.T) {
	nonRecropCode := "some_other_code"
	casess := []struct {
		name            string
		errorCode       string
		errorText       string
		expectErrCode   string
		expectErrTextBe string // exact expectation; "" means cleared
	}{
		{
			name:            "wrapped production form",
			errorCode:       downloader.PosterRecropRequiredCode,
			errorText:       "download failed: apply poster: poster requires a fresh crop: source identity unavailable",
			expectErrCode:   "",
			expectErrTextBe: "",
		},
		{
			name:            "direct refusal text (legacy unwrapped)",
			errorCode:       downloader.PosterRecropRequiredCode,
			errorText:       "poster requires a fresh crop: source identity unavailable",
			expectErrCode:   "",
			expectErrTextBe: "",
		},
		{
			name:            "unrelated apply failure text preserved",
			errorCode:       downloader.PosterRecropRequiredCode,
			errorText:       "nfo generation failed: empty movie",
			expectErrCode:   "",
			expectErrTextBe: "nfo generation failed: empty movie",
		},
		{
			name:            "non-recrop ErrorCode path untouched",
			errorCode:       nonRecropCode,
			errorText:       "poster requires a fresh crop: elsewhere is fine but this row isn't flagged",
			expectErrCode:   nonRecropCode,
			expectErrTextBe: "poster requires a fresh crop: elsewhere is fine but this row isn't flagged",
		},
	}
	for _, tc := range casess {
		t.Run(tc.name, func(t *testing.T) {
			row := &resultstore.MovieResult{
				ErrorCode: tc.errorCode,
				Error:     tc.errorText,
				Status:    models.JobStatusFailed,
			}
			clearPosterRecrop(row)
			require.Equal(t, tc.expectErrCode, row.ErrorCode)
			require.Equal(t, tc.expectErrTextBe, row.Error)
		})
	}
}
