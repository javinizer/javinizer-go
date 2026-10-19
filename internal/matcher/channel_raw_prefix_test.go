package matcher

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestChannelRawPrefixAfterQuality(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, channel := range []string{"h_003", "n_600", "H_003", "N_600"} {
		for _, number := range []string{"156", "00156", "12345", "100000", "1000000"} {
			for _, suffix := range []string{"", "e", "z", "h", "hd", "ai", "eh", "zhd", "eai"} {
				cid := channel + "rct" + number + suffix
				for _, prefix := range []string{"", "x265 ", "WEB2160 ", "WEB-DL2160 ", "DTS-HD192 "} {
					name := prefix + cid + " ABC-123-HD.mkv"
					t.Run(name, func(t *testing.T) {
						want := strings.ToUpper(cid)
						require.Equal(t, want, m.MatchString(name))
						for _, ext := range []string{".mkv", ""} {
							got := m.MatchFile(models.FileMatchInfo{Name: name, Extension: ext})
							require.NotNil(t, got)
							require.Equal(t, want, got.ID)
							require.Empty(t, got.RemasterMarker)
						}
					})
				}
			}
		}
	}
}

func TestChannelRawPrefixSelectedSpan(t *testing.T) {
	for _, tc := range []struct{ input, id, remainder string }{
		{"x265 h_003rct00156hd-pt2", "h_003rct00156hd", "-pt2"},
		{"x265 n_600t28123h-part3", "n_600t28123h", "-part3"},
		{"noise_h_003rct00156eai[4K]", "h_003rct00156eai", "[4K]"},
		{"z003rct00156hd x265 h_003rct00156hd-pt2", "h_003rct00156hd", "-pt2"},
		{"118ipx00535 x265 h_003rct00156hd", "118ipx00535", "x265 h_003rct00156hd"},
		{"x265 h_003rct00156hd 118ipx00535", "h_003rct00156hd", "118ipx00535"},
		{"xh_003rct00156hd-pt2", "003rct00156hd", "-pt2"},
		{"9h_003rct00156hd-pt2", "003rct00156hd", "-pt2"},
		{"x_003rct00156hd-pt2", "003rct00156hd", "-pt2"},
		{"h_rct00156hd-pt2", "rct00156hd", "-pt2"},
		{"h_003rct00156xyz-pt2", "003rct00156xyz", "-pt2"},
		{"h_003rct00156hdd-pt2", "003rct00156hdd", "-pt2"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			id, remainder := contentIDPrefixMatch(tc.input)
			require.Equal(t, tc.id, id)
			require.Equal(t, tc.remainder, remainder)
		})
	}
}

func TestChannelRawPrefixMultipartAndPrecedence(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tail := range []string{"-pt2", "-cd2", "vol2", ".part2"} {
		name := "x265 h_003rct00156hd" + tail + ".mkv"
		got := m.MatchFile(models.FileMatchInfo{Name: name, Extension: ".mkv"})
		require.NotNil(t, got)
		require.Equal(t, "H_003RCT00156HD", got.ID)
		require.Equal(t, 2, got.PartNumber)
		require.True(t, got.IsMultiPart)
		require.Equal(t, PatternExplicit, got.MultipartPattern)
		require.Equal(t, got.ID, m.MatchString(name))
	}
}

func TestChannelRawPrefixControls(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ input, want string }{
		{"ABC-123-HD x265 h_003rct00156hd.mkv", "ABC-123H"},
		{"x265 1rct00156hd h_003rct00156hd.mkv", "1RCT00156HD"},
		{"x265 55t28123h.mkv", "55T28123H"},
		{"x265 T28-123-HD.mkv", "T28-123H"},
		{"x265 T-28123-HD.mkv", "T-28123H"},
		{"x265 h_003rct00157hd.mkv", "H_003RCT00157HD"},
		{"x265 h_003rct00156zhd.mkv", "H_003RCT00156ZHD"},
		{"x265 Ｈ_003rct00156hd.mkv", "003RCT00156HD"},
		{"x265 h_００３rct００１５６hd.mkv", "X265H"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			require.Equal(t, tc.want, m.MatchString(tc.input))
			got := m.MatchFile(models.FileMatchInfo{Name: tc.input, Extension: ".mkv"})
			require.NotNil(t, got)
			require.Equal(t, tc.want, got.ID)
		})
	}
	for _, tc := range []struct{ pattern, want string }{
		{`^(x265) h_003rct00156hd\.mkv$`, "X265"},
		{`(003rct00156hd)`, "003RCT00156HD"},
		{`(h_003rct00156hd)`, "H_003RCT00156HD"},
	} {
		custom, err := NewMatcher(&Config{RegexEnabled: true, RegexPattern: tc.pattern})
		require.NoError(t, err)
		require.Equal(t, tc.want, custom.MatchString("x265 h_003rct00156hd.mkv"))
		got := custom.MatchFile(models.FileMatchInfo{Name: "x265 h_003rct00156hd.mkv"})
		require.NotNil(t, got)
		require.Equal(t, tc.want, got.ID)
		require.Equal(t, "regex", got.MatchedBy)
	}
}

type channelPrefixURLRegistry struct{ scraper models.Scraper }

func (r channelPrefixURLRegistry) GetAllInstances() []models.Scraper {
	return []models.Scraper{r.scraper}
}

func TestChannelRawPrefixURLProvenance(t *testing.T) {
	const cid = "h_003rct00156hd"
	reg := channelPrefixURLRegistry{&mockScraper{name: "r18dev", enabled: true, canHandle: true, extractID: cid}}
	parsed, err := ParseInput("https://www.r18.dev/videos/vod/movies/detail/-/id="+cid+"/", reg)
	require.NoError(t, err)
	require.True(t, parsed.IsURL)
	require.Equal(t, cid, parsed.ID)
	require.Equal(t, "r18dev", parsed.ScraperHint)
	plain, err := ParseInput(cid, nil)
	require.NoError(t, err)
	require.False(t, plain.IsURL)
	require.Equal(t, cid, plain.ID)
}
