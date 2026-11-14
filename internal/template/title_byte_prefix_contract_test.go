package template

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestTitleBytePrefixBoundaryContract(t *testing.T) {
	e := &Engine{}
	for _, tc := range []struct {
		name, title string
		want        [3]string
	}{
		{"ASCII", "abcd", [3]string{"a", "ab", "abc"}},
		{"two byte", "éé", [3]string{"", "é", "é"}},
		{"three byte", "日本", [3]string{"", "", "日"}},
		{"four byte", "😀x", [3]string{"", "", ""}},
		{"mixed", "aé日", [3]string{"a", "a", "aé"}},
		{"combining", "a\u0301bc", [3]string{"a", "a", "a\u0301"}},
		{"space retained", "a bc", [3]string{"a", "a ", "a b"}},
		{"invalid leading", "\xffabc", [3]string{"", "", "\ufffd"}},
		{"invalid after ascii", "a\xffbc", [3]string{"a", "a", "a"}},
		{"truncated encoding", "\xf0\x9f\x98x", [3]string{"", "", "\ufffd"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for budget := 1; budget <= 3; budget++ {
				got := e.TruncateTitleBytes(tc.title, budget)
				require.Equal(t, tc.want[budget-1], got, "budget=%d", budget)
				require.LessOrEqual(t, len(got), budget)
				require.True(t, utf8.ValidString(got))
			}
		})
	}
	for _, title := range []string{"", "a", "abc", "é", "日", "\xff", "\xff\xff", "\xf0\x9f"} {
		for budget := 1; budget <= 3; budget++ {
			if len(title) <= budget {
				require.Equal(t, title, e.TruncateTitleBytes(title, budget))
			}
		}
		require.Empty(t, e.TruncateTitleBytes(title, 0))
		require.Empty(t, e.TruncateTitleBytes(title, -1))
	}
	for _, tc := range []struct {
		title  string
		budget int
		want   string
	}{
		{"abcdef", 4, "a..."}, {"Hello World", 9, "Hello..."}, {"日本語", 7, "日..."},
		{"😀abcde", 7, "😀..."}, {"\xffabcdefg", 7, "\ufffda..."}, {"a  bcdef", 6, "a..."},
	} {
		require.Equal(t, tc.want, e.TruncateTitleBytes(tc.title, tc.budget))
	}
}

func TestTitleBytePrefixDecoderContract(t *testing.T) {
	e := &Engine{}
	alphabet := []byte{'a', ' ', 0xc2, 0xa2, 0xe6, 0x97, 0xa5, 0xff}
	for _, a := range alphabet {
		for _, b := range alphabet {
			for _, c := range alphabet {
				for _, d := range alphabet {
					title := string([]byte{a, b, c, d})
					for budget := 1; budget <= 3; budget++ {
						var want strings.Builder
						remaining := title
						for remaining != "" {
							r, size := utf8.DecodeRuneInString(remaining)
							if want.Len()+utf8.RuneLen(r) > budget {
								break
							}
							want.WriteRune(r)
							remaining = remaining[size:]
						}
						got := e.TruncateTitleBytes(title, budget)
						require.Equal(t, want.String(), got, "input=%x budget=%d", title, budget)
						require.LessOrEqual(t, len(got), budget)
					}
				}
			}
		}
	}
}
