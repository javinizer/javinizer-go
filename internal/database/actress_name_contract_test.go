package database

import (
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCanonicalActressNameWhitespaceContract(t *testing.T) {
	for _, tc := range []struct{ name, first, last, japanese, want string }{
		{"Japanese priority", "Hana", "Example", "\u3000花子\u00a0", "花子"},
		{"first only", "\u3000Hana\u00a0", "", " ", "Hana"},
		{"last only", "", "\u3000Example\u00a0", " ", "Example"},
		{"blank last", "Hana", "\u3000", "", "Hana"},
		{"blank first", "\u00a0", "Example", "", "Example"},
		{"all blank", "\u3000", "\u00a0", "\t", ""},
		{"internal spaces preserved", " Hana ", " Example ", "", "Example   Hana"},
		{"zero width retained", "\u200b", "", "", "\u200b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := models.Actress{FirstName: tc.first, LastName: tc.last, JapaneseName: tc.japanese}
			require.Equal(t, tc.want, canonicalActressName(&actor))
		})
	}
}

func TestSyncNameEvidenceOutcomeContract(t *testing.T) {
	snapshot, options, key := lifecycleSyncEvidence(t)
	expected, ok := snapshot.Get(key)
	require.True(t, ok)
	for _, tc := range []struct {
		name     string
		snapshot *actresscache.Snapshot
		key      string
		now      time.Time
		age      time.Duration
		valid    bool
	}{
		{"fresh", snapshot, key, options.Now, time.Hour, true},
		{"oldest accepted", snapshot, key, options.Now.Add(time.Hour), time.Hour, true},
		{"expired", snapshot, key, options.Now.Add(time.Hour + time.Nanosecond), time.Hour, false},
		{"future", snapshot, key, options.Now.Add(-time.Nanosecond), time.Hour, false},
		{"missing snapshot", nil, key, options.Now, time.Hour, false},
		{"empty snapshot", new(actresscache.Snapshot), key, options.Now, time.Hour, false},
		{"missing key", snapshot, "missing", options.Now, time.Hour, false},
		{"oversized key", snapshot, strings.Repeat("x", 513), options.Now, time.Hour, false},
		{"zero time", snapshot, key, time.Time{}, time.Hour, false},
		{"zero age", snapshot, key, options.Now, 0, false},
		{"negative age", snapshot, key, options.Now, -time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence, err := syncNameEvidence(tc.snapshot, tc.key, tc.now, tc.age)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, expected, evidence)
			} else {
				require.Equal(t, ErrActressSyncStale, err)
				require.Equal(t, actresscache.Evidence{}, evidence)
			}
		})
	}
}
