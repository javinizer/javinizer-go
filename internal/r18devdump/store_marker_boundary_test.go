package r18devdump

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestStoreMarkerBoundaryRejectsConflictingRows(t *testing.T) {
	for _, tc := range []struct{ query, cid, dvd string }{
		{"T-28123H", "9t28123h", "T28-123-HD"},
		{"T-28123-HD", "9t28123h", "T28-123-HD"},
		{"T28-123H", "t28123h", "T-28123-HD"},
		{"T28-123-HD", "t28123h", "T-28123-HD"},
		{"T-28123H", "55t28123h", "T28-123-HD"}, // candidate fallback re-entry
		{"T28-123H", "9t2800123h", "T-28123-HD"},
		{"T-28000123H", "55t28123h", "T-28123-HD"}, // embedded T digits are not T28 padding
	} {
		t.Run(tc.query+"/"+tc.cid, func(t *testing.T) {
			store, err := Open(seedDump(t, tc.cid+"\t"+tc.dvd))
			require.NoError(t, err)
			defer store.Close()
			matches, err := store.MatchByDisplayID(context.Background(), tc.query)
			require.ErrorIs(t, err, models.ErrDumpMiss)
			require.Empty(t, matches)
			cid, err := store.LookupByDVDID(context.Background(), tc.query)
			require.ErrorIs(t, err, models.ErrDumpMiss)
			require.Empty(t, cid)
			movie, err := store.LookupMovie(context.Background(), tc.query)
			require.ErrorIs(t, err, models.ErrDumpMiss)
			require.Nil(t, movie)
		})
	}
}

func TestStoreMarkerBoundaryMatchingRowsAndRawPriority(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		rows := []string{"55t28123h\tT28-123-HD", "874t28123h\tT-28123-HD"}
		if reverse {
			rows[0], rows[1] = rows[1], rows[0]
		}
		store, err := Open(seedDump(t, strings.Join(rows, "\n")))
		require.NoError(t, err)
		for _, tc := range []struct{ query, cid string }{
			{"T-28123H", "874t28123h"}, {"T-28123-HD", "874t28123h"},
			{"T28-123H", "55t28123h"}, {"T28-00123-HD", "55t28123h"},
			{"55t28123h", "55t28123h"},
		} {
			t.Run(fmt.Sprintf("%t/%s", reverse, tc.query), func(t *testing.T) {
				matches, err := store.MatchByDisplayID(context.Background(), tc.query)
				require.NoError(t, err)
				require.NotEmpty(t, matches)
				require.Equal(t, tc.cid, matches[0].ContentID)
				if !strings.HasPrefix(tc.query, "55") {
					cid, err := store.LookupByDVDID(context.Background(), tc.query)
					require.NoError(t, err)
					require.Equal(t, tc.cid, cid)
					movie, err := store.LookupMovie(context.Background(), tc.query)
					require.NoError(t, err)
					require.Equal(t, tc.cid, movie.ContentID)
				}
			})
		}
		store.Close()
	}
	// Literal raw ownership beats a colliding normalized DVD identity.
	store, err := Open(seedDump(t, "t28123h\tT-28123-HD\n9t28123h\tT28-123H"))
	require.NoError(t, err)
	defer store.Close()
	matches, err := store.MatchByDisplayID(context.Background(), "t28123h")
	require.NoError(t, err)
	require.Equal(t, "t28123h", matches[0].ContentID)
}

func TestStoreMarkerUnicodeWhitespaceCandidates(t *testing.T) {
	store, err := Open(seedDump(t, "9t28123h\t\\N\n55t28123zh\t\\N"))
	require.NoError(t, err)
	defer store.Close()
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.IsSpace(r) {
			continue
		}
		for _, tc := range []struct{ display, cid string }{{"T28 123 HD", "9t28123h"}, {"T 28123 Z HD", "55t28123zh"}} {
			t.Run(fmt.Sprintf("U%04X/%s", r, tc.display), func(t *testing.T) {
				matches, err := store.MatchByDisplayID(context.Background(), strings.ReplaceAll(tc.display, " ", string(r)))
				require.NoError(t, err)
				require.NotEmpty(t, matches)
				require.Equal(t, tc.cid, matches[0].ContentID)
			})
		}
	}
}

func TestStoreMarkerRawOwnershipDoesNotExtendToCandidates(t *testing.T) {
	for _, tc := range []struct{ query, cid, dvd string }{
		{"t28123h", "9t28123h", "T28-123-HD"},
		{"t2800123h", "9t28123h", "T-28123-HD"},
		{"55t28123h", "874t28123h", "T-28123-HD"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			require.Contains(t, ContentIDCandidatesWithMarker(tc.query), tc.cid)
			store, err := Open(seedDump(t, tc.cid+"\t"+tc.dvd))
			require.NoError(t, err)
			defer store.Close()
			matches, err := store.MatchByDisplayID(context.Background(), tc.query)
			require.ErrorIs(t, err, models.ErrDumpMiss)
			require.Empty(t, matches)
		})
	}
	for _, cid := range []string{"55t28123h", "h_003t28123hd"} {
		store, err := Open(seedDump(t, cid+"\tT-28123-HD"))
		require.NoError(t, err)
		matches, err := store.MatchByDisplayID(context.Background(), cid)
		require.NoError(t, err)
		require.Equal(t, cid, matches[0].ContentID)
		store.Close()
	}
}

func TestStoreMarkerRawPaddingOwnership(t *testing.T) {
	for _, tc := range []struct{ query, cid, dvd string }{
		{"1rct156h", "1rct00156h", "RCT-156-HD"},
		{"9t28123h", "9t2800123h", "T28-123-HD"},
		{"1t28123h", "1t2800123h", "T28-123-HD"},
		{"h_003t28123h", "h_003t2800123h", "T28-123-HD"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			require.True(t, dumpRawMarkerEqual(tc.cid, tc.query))
			// The channel-prefixed candidate helper intentionally returns only the
			// literal raw input; its equality contract still allows padding aliases.
			if strings.HasPrefix(tc.query, "h_") {
				return
			}
			store, err := Open(seedDump(t, tc.cid+"\t"+tc.dvd))
			require.NoError(t, err)
			defer store.Close()
			matches, err := store.MatchByDisplayID(context.Background(), tc.query)
			require.NoError(t, err)
			require.Equal(t, tc.cid, matches[0].ContentID)
		})
	}
	for _, pair := range [][2]string{
		{"1rct156h", "2rct00156h"}, {"1rct156h", "1rct00157h"},
		{"1rct156h", "1rct00156hd"}, {"1rct156h", "1rct00156eh"},
		{"1rct156eh", "1rct00156zh"}, {"h_003rct156h", "n_003rct00156h"},
		{"55t28123h", "55t2800123h"}, {"t28123h", "t2800123h"},
		{"9t28123h", "1t2800123h"}, {"1rct156h", "garbage"},
	} {
		require.False(t, dumpRawMarkerEqual(pair[0], pair[1]), pair)
		require.False(t, dumpRawMarkerEqual(pair[1], pair[0]), pair)
	}
	require.True(t, dumpRawMarkerEqual(" 55t28123h ", "55T28123H"))
	require.True(t, dumpRawMarkerEqual("h_003rct156h", "h_003rct00156h"))
}

func TestStoreMarkerCompactDisplayKeepsNormalizedPriority(t *testing.T) {
	for _, tc := range []struct{ query, cid, dvd, literal string }{
		{"RCT12345H", "1rct12345h", "RCT-12345-HD", "rct12345h"},
		{"RCT123456HD", "1rct123456h", "RCT-123456-HD", "rct123456hd"},
		{"T28123H", "55t28123h", "T-28123-HD", "t28123h"},
	} {
		for _, literal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/literal=%t", tc.query, literal), func(t *testing.T) {
				rows := tc.cid + "\t" + tc.dvd
				if literal {
					rows += "\n" + tc.literal + "\tOTHER-999-HD"
				}
				store, err := Open(seedDump(t, rows))
				require.NoError(t, err)
				defer store.Close()
				matches, err := store.MatchByDisplayID(context.Background(), tc.query)
				require.NoError(t, err)
				require.Len(t, matches, 1)
				require.Equal(t, tc.cid, matches[0].ContentID)
				cid, err := store.LookupByDVDID(context.Background(), tc.query)
				require.NoError(t, err)
				require.Equal(t, tc.cid, cid)
				movie, err := store.LookupMovie(context.Background(), tc.query)
				require.NoError(t, err)
				require.Equal(t, tc.cid, movie.ContentID)
			})
		}
	}
}

func TestStoreMarkerDisplayKeyControls(t *testing.T) {
	for _, tc := range []struct{ id, key string }{
		{"ABC-00000-H", "ABC/0//H"}, {"T-28000123-HD", "T/28000123//H"},
		{"T2800123HD", "T28/123//H"}, {"T281234HD", "T/281234//H"},
		{"T28\u00a000123\tZ\u2003AI", "T28/123/Z/AI"},
		{"RCT-156 E-HD", "RCT/156/E/H"}, {"invalid", ""},
	} {
		require.Equal(t, tc.key, dumpMarkerDisplayKey(tc.id), tc.id)
	}
}
