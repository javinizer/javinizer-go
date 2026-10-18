package r18devdump

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkerCandidatesRejectInvalidPinnedSeries(t *testing.T) {
	for _, series := range []string{"", "-", "_", ".", "9", "123", "!", "r!ct", "1rct", "rct1", "t29", "r ct"} {
		for _, separator := range []string{"-", "_", "."} {
			for _, tail := range []string{"123-H", "123-HD", "123-AI", "123-Z-HD", "123-E-H"} {
				input := series + separator + tail
				assert.Empty(t, ContentIDCandidatesWithMarker(input), input)
			}
		}
	}
	for _, input := range []string{" -123-HD ", "\t-123-HD\u00a0", "--123-HD", "_-123-HD"} {
		assert.Empty(t, ContentIDCandidatesWithMarker(input), input)
	}
	for _, series := range []string{"rct", "t", "t28"} {
		for _, sep := range []string{"-", "_", ".", "\t", "\u00a0"} {
			require.NotEmpty(t, ContentIDCandidatesWithMarker(series+sep+"123-HD"))
		}
	}
}

func TestMarkerCandidatesRespectPrefixedTSeriesBoundary(t *testing.T) {
	for _, marker := range []string{"h", "hd", "ai", "zhd", "eh"} {
		for _, number := range []string{"123", "00123", "123456", "000123"} {
			for _, prefix := range []string{"9", "1"} {
				input := prefix + "t28" + number + marker
				got := ContentIDCandidatesWithMarker(input)
				require.NotEmpty(t, got, input)
				assert.Equal(t, input, got[0])
				if number == "123456" {
					assert.Contains(t, got, "1t28123456"+marker)
					assert.NotContains(t, got, "55t28123456"+marker)
					continue
				}
				assert.Contains(t, got, "9t2800123"+marker)
				assert.Contains(t, got, "1t2800123"+marker)
				assert.Contains(t, got, "t2800123"+marker)
				for _, candidate := range got {
					for _, foreign := range []string{"55t", "874t", "1038t"} {
						assert.False(t, strings.HasPrefix(candidate, foreign), input+" -> "+candidate)
					}
				}
			}
		}
		for _, prefix := range []string{"55", "874", "1038"} {
			input := prefix + "t28123" + marker
			got := ContentIDCandidatesWithMarker(input)
			require.NotEmpty(t, got)
			assert.Equal(t, input, got[0])
			assert.Contains(t, got, "55t28123"+marker)
			assert.NotContains(t, got, "9t2800123"+marker)
		}
		for _, prefix := range []string{"2", "118", "9999", "0"} {
			input := prefix + "t2800123" + marker
			assert.Equal(t, []string{input}, ContentIDCandidatesWithMarker(input))
		}
		for _, prefix := range []string{"9", "1", "55", "874", "1038", "118"} {
			input := prefix + "t28" + strings.Repeat("9", 40) + marker
			assert.Equal(t, []string{input}, ContentIDCandidatesWithMarker(input))
		}
		input := "n_1137t28123" + marker
		assert.Equal(t, []string{input}, ContentIDCandidatesWithMarker(input))
	}
	got := ContentIDCandidatesWithMarker("T-28123-HD")
	assert.Contains(t, got, "55t28123h")
	assert.NotContains(t, got, "9t2800123h")
	// The legacy marker-free API has no new T28 reinterpretation.
	assert.Contains(t, ContentIDCandidates("9t2800123"), "55t2800123")
}
