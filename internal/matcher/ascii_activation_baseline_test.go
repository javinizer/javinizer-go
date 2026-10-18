package matcher

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// This identical pure-string fixture also runs against the untouched main
// baseline. It does not invoke scanners, organizers, or filesystem operations.
func TestASCIIActivationPreservesUnfoldedBaseline(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		want string
	}{
		{"ＲＣＴ-156-ＨＤ.mkv", ""},
		{"ＲＣＴ－１５６－ＨＤ.mkv", ""},
		{"ＩＰＸ－５３５．ｍｋｖ", ""},
		{"IPX-535．ｍｋｖ", "IPX-535"},
		{"RCT-156-ＨＤ.mkv", "RCT-156"},
		{"ABC-123-HD｛1080p｝.mkv", "ABC-123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, m.MatchString(tc.name))
			got := m.MatchFile(models.FileMatchInfo{Name: tc.name, Extension: ".mkv"})
			if tc.want == "" {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, tc.want, got.ID)
			}
		})
	}
	for _, tc := range []struct {
		pattern  string
		name     string
		want     string
		stemWant string
	}{
		{`([Ａ-Ｚａ-ｚ]+-\d+)`, "ｒｃｔ-156.mkv", "ＲＣＴ-156", "ＲＣＴ-156"},
		{`(IPX-\d+)`, "ＩＰＸ-535.mkv", "", ""},
		{`^(ＲＣＴ-156)$`, "ＲＣＴ-156.mkv", "", "ＲＣＴ-156"},
	} {
		custom, err := NewMatcher(&Config{RegexEnabled: true, RegexPattern: tc.pattern})
		require.NoError(t, err)
		require.Equal(t, tc.want, custom.MatchString(tc.name))
		withoutExtension := custom.MatchFile(models.FileMatchInfo{Name: tc.name})
		if tc.want == "" {
			require.Nil(t, withoutExtension)
		} else {
			require.NotNil(t, withoutExtension)
			require.Equal(t, tc.want, withoutExtension.ID)
		}
		got := custom.MatchFile(models.FileMatchInfo{Name: tc.name, Extension: ".mkv"})
		if tc.stemWant == "" {
			require.Nil(t, got)
		} else {
			require.NotNil(t, got)
			require.Equal(t, tc.stemWant, got.ID)
			require.Equal(t, "regex", got.MatchedBy)
		}
	}
}
