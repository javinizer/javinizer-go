package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Codex round-9 (PR #257): the bare-H demotion no longer pins the sibling
// shape to exactly A,B at the matcher level. applyRemasterDemotions accepts
// every corroborated distinct bare-letter sibling set (the Codex ask), and
// the group validation layer decides coherence: only the pinned A,B,H bet
// (parts 1, 2, 8) and a contiguous run completed by part 8 keep the
// demotion. Every other augmented part set — a three-or-more-part original
// whose part 8 cannot exist (the round-1/F3 outcome), a gapped or offset
// run — rolls back to the remaster spelling.

func mkR9(name, id string, part int, pattern, marker string) MatchResult {
	return MatchResult{File: models.FileMatchInfo{Path: "/v/" + name}, ID: id, PartNumber: part, MultipartPattern: pattern, RemasterMarker: marker}
}

func r9Siblings(parts ...int) []MatchResult {
	out := make([]MatchResult, 0, len(parts)+1)
	for _, p := range parts {
		out = append(out, mkR9(string(rune('A'+p-1))+".mkv", "ABC-123", p, PatternLetter, ""))
	}
	return append(out, mkR9("ABC-123H.mkv", "ABC-123H", 0, "", "H"))
}

// The matcher-level demotion accepts every corroborated distinct letter
// set, not only the classic A,B pair: the H match is tentatively part 8 of
// the base ID before group validation runs.
func TestApplyRemasterDemotions_CorroboratedDistinctSets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parts []int
	}{
		{"A B", []int{1, 2}},
		{"A B C", []int{1, 2, 3}},
		{"B C", []int{2, 3}},
		{"A C", []int{1, 3}},
		{"A through G", []int{1, 2, 3, 4, 5, 6, 7}},
		{"A through G plus I", []int{1, 2, 3, 4, 5, 6, 7, 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := r9Siblings(tc.parts...)
			demoted := applyRemasterDemotions(in)
			require.Len(t, demoted, 1, "a corroborated distinct set must demote tentatively")
			h := in[len(in)-1]
			assert.Equal(t, "ABC-123", h.ID)
			assert.Equal(t, 8, h.PartNumber)
			assert.Equal(t, PatternLetter, h.MultipartPattern)
		})
	}

	// A single distinct sibling, or none, never corroborates the demotion.
	for _, tc := range []struct {
		name  string
		parts []int
	}{
		{"A only", []int{1}},
		{"no siblings", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, applyRemasterDemotions(r9Siblings(tc.parts...)))
		})
	}
}

// Corroborated but incoherent augmented sets restore the remaster spelling
// end to end (the F3 outcome: a multi-part original cannot have a part H).
func TestValidateMultipart_RemasterDemotionIncoherentSetsRestore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parts []int
	}{
		{"A B C H", []int{1, 2, 3}},
		{"A B C D H", []int{1, 2, 3, 4}},
		{"B C H", []int{2, 3}},
		{"A C H", []int{1, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := ValidateMultipartInDirectory(r9Siblings(tc.parts...))
			h := out[len(out)-1]
			assert.Equal(t, "ABC-123H", h.ID, "an incoherent augmented set restores the remaster spelling")
			assert.Equal(t, 0, h.PartNumber)
			assert.Equal(t, "H", h.RemasterMarker)
			assert.False(t, h.IsMultiPart)
			for i := 0; i < len(out)-1; i++ {
				assert.True(t, out[i].IsMultiPart, "sibling %d revalidates without H", i)
			}
		})
	}
}

// A contiguous run completed by part 8 keeps the demotion: with parts 1..7
// visible, the H file is the genuine eighth part of the original, not a
// separate remaster id sorted into another movie folder.
func TestValidateMultipart_RemasterDemotionFullRunKeepsPart8(t *testing.T) {
	out := ValidateMultipartInDirectory(r9Siblings(1, 2, 3, 4, 5, 6, 7))
	require.Len(t, out, 8)
	for i, r := range out {
		assert.True(t, r.IsMultiPart, "part %d confirms", i+1)
		assert.Equal(t, "ABC-123", r.ID)
		assert.Equal(t, i+1, r.PartNumber)
	}
	assert.Equal(t, "", out[7].RemasterMarker)
	assert.Equal(t, "-H", out[7].PartSuffix)

	// A run longer than eight parts still completes at part 8: the H file
	// is the eighth part of a nine-part original.
	out = ValidateMultipartInDirectory(r9Siblings(1, 2, 3, 4, 5, 6, 7, 9))
	require.Len(t, out, 9)
	h := out[len(out)-1]
	assert.Equal(t, "ABC-123", h.ID)
	assert.Equal(t, 8, h.PartNumber)
	assert.True(t, h.IsMultiPart)
	assert.Equal(t, "", h.RemasterMarker)
}

// End to end through the real matcher: a complete A..G run folds its H file
// in as part 8, while an offset B,C run keeps the remaster spelling.
func TestValidateMultipart_RemasterDemotionEndToEndFiles(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	r9files := func(letters string) []models.FileMatchInfo {
		files := make([]models.FileMatchInfo, 0, len(letters))
		for _, r := range letters {
			name := "RCT-156-" + string(r) + ".mkv"
			files = append(files, models.FileMatchInfo{Path: "/v/" + name, Name: name, Extension: ".mkv"})
		}
		return files
	}

	results := m.Match(r9files("ABCDEFGH"))
	require.Len(t, results, 8)
	validated := ValidateMultipartInDirectory(results)
	part8 := validated[7]
	assert.Equal(t, "RCT-156", part8.ID, "the H file of a complete A..G run is part 8, not a separate remaster id")
	assert.Equal(t, 8, part8.PartNumber)
	assert.True(t, part8.IsMultiPart)
	assert.Equal(t, "", part8.RemasterMarker)

	results = m.Match(r9files("BCH"))
	require.Len(t, results, 3)
	validated = ValidateMultipartInDirectory(results)
	assert.Equal(t, "RCT-156H", validated[2].ID, "an offset run without A keeps the remaster spelling end to end")
	assert.Equal(t, "H", validated[2].RemasterMarker)
	assert.Equal(t, 0, validated[2].PartNumber)
}
