package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectPartSuffix(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		wantNum     int
		wantSuf     string
		wantPattern string
	}{
		// Explicit patterns - always multipart
		{"IPX-535-pt1", "IPX-535", 1, "-pt1", PatternExplicit},
		{"IPX-535PT2", "IPX-535", 2, "-pt2", PatternExplicit},
		{"IPX-535-part1", "IPX-535", 1, "-part1", PatternExplicit},
		{"IPX-535part2", "IPX-535", 2, "-part2", PatternExplicit},
		{"IPX-535 pt1", "IPX-535", 1, "-pt1", PatternExplicit},
		{"IPX-535_part3", "IPX-535", 3, "-part3", PatternExplicit},
		{"PRED-151-1", "PRED-151", 1, "-1", PatternExplicit},
		{"PRED-151-2", "PRED-151", 2, "-2", PatternExplicit},

		// Ambiguous letter patterns - need directory context validation
		{"MDB-087A", "MDB-087", 1, "-A", PatternLetter},
		{"MDB-087-b", "MDB-087", 2, "-B", PatternLetter},
		{"ABP-123c", "ABP-123", 3, "-C", PatternLetter},
		{"IPX-535-D", "IPX-535", 4, "-D", PatternLetter},
		{"IPX-535-Z", "IPX-535", 26, "-Z", PatternLetter},
		{"ABW-121-C", "ABW-121", 3, "-C", PatternLetter}, // Chinese subtitle case

		// No pattern
		{"ABC-123", "ABC-123", 0, "", PatternNone},
		{"IPX-535 no suffix", "IPX-535", 0, "", PatternNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			num, suf, pattern, _ := DetectPartSuffix(tt.name, tt.id)
			assert.Equal(t, tt.wantNum, num, "PartNumber mismatch")
			assert.Equal(t, tt.wantSuf, suf, "PartSuffix mismatch")
			assert.Equal(t, tt.wantPattern, pattern, "PatternType mismatch")
		})
	}
}

func TestDetectPartSuffix_PatternConstants(t *testing.T) {
	// Verify pattern constants are correct
	assert.Equal(t, "explicit", PatternExplicit)
	assert.Equal(t, "letter", PatternLetter)
	assert.Equal(t, "", PatternNone)
}
