package matcher

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Pure strings, shared unchanged with the unactivated baseline.
func TestLeadingQualityBaselineProbe(t *testing.T) {
	m, err := NewMatcher(&Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"x265 ABC-123-HD.mkv", "WEB2160 ABC-123-HD.mkv",
		"WEB-DL2160 ABC-123-HD.mkv", "DTS-HD192 ABC-123-HD.mkv",
		"x265.mkv", "WEB2160.mkv", "x265 N1234 ABC-123-HD.mkv",
		"N1234 x265 ABC-123-HD.mkv", "189web00087 x265 ABC-123-HD.mkv",
		"x265 1rct00156hd ABC-123-HD.mkv", "x265 ＲＣＴ-156-ＨＤ.mkv",
		"x265 h_003rct00156hd ABC-123-HD.mkv", "h_003rct00156hd x265 ABC-123-HD.mkv",
	} {
		fileID := ""
		if got := m.MatchFile(models.FileMatchInfo{Name: name, Extension: ".mkv"}); got != nil {
			fileID = got.ID
		}
		t.Logf("%q: string=%q file=%q", name, m.MatchString(name), fileID)
	}
}
