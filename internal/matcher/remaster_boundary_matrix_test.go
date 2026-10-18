package matcher

import (
	"fmt"
	"github.com/javinizer/javinizer-go/internal/models"
	"reflect"
	"strings"
	"testing"
)

func TestAuditNormalMarkerBoundaryMatrix(t *testing.T) {
	m, _ := NewMatcher(&Config{})
	for _, suffix := range []string{"", "E", "Z"} {
		for _, marker := range []string{"H", "HD", "AI"} {
			for _, sep := range []string{"", "-", "_", ".", " "} {
				for _, tail := range []string{"", "-cd2", "-part03", "-vol4", "-1080p"} {
					name := "IPX-535" + suffix + sep + marker + tail + ".MKV"
					t.Run(name, func(t *testing.T) {
						r := m.MatchFile(models.FileMatchInfo{Name: name, Extension: ".mkv"})
						fold := marker
						if fold == "HD" {
							fold = "H"
						}
						want := "IPX-535" + suffix + fold
						part := 0
						switch tail {
						case "-cd2":
							part = 2
						case "-part03":
							part = 3
						case "-vol4":
							part = 4
						}
						if r == nil || r.ID != want || r.RemasterMarker != marker || r.PartNumber != part || m.MatchString(name) != want {
							t.Fatalf("got %+v string=%s want %s marker=%s part=%d", r, m.MatchString(name), want, marker, part)
						}
					})
				}
			}
		}
	}
	for _, tail := range []string{"HDA", "HDTV", "HDC", "AIR", "H264", "H.264"} {
		t.Run("reject_"+tail, func(t *testing.T) {
			n := "IPX-535-" + tail + ".mkv"
			r := m.MatchFile(models.FileMatchInfo{Name: n, Extension: ".mkv"})
			if r == nil || r.ID != "IPX-535" || r.RemasterMarker != "" {
				t.Fatalf("got %+v", r)
			}
		})
	}
}
func TestAuditNormalSiblingRestoration(t *testing.T) {
	m, _ := NewMatcher(&Config{})
	for _, letters := range []string{"AH", "ABH", "ABCH", "ABCDEFGH", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"} {
		t.Run(letters, func(t *testing.T) {
			rs := []MatchResult{}
			for _, l := range letters {
				n := fmt.Sprintf("ABC-123-%c.mkv", l)
				rs = append(rs, *m.MatchFile(models.FileMatchInfo{Name: n, Extension: ".mkv", Path: "normal/" + n}))
			}
			before := append([]MatchResult(nil), rs...)
			got := ValidateMultipartInDirectory(rs)
			if !reflect.DeepEqual(rs, before) {
				t.Fatal("mutates input")
			}
			for i, r := range got {
				isH := letters[i] == 'H'
				keepH := isH && (letters == "AH" || letters == "ABCH")
				wantID := "ABC-123"
				part := int(letters[i]-'A') + 1
				multi := true
				if keepH {
					wantID += "H"
					part = 0
					multi = false
				}
				if letters == "AH" {
					part = 0
					multi = false
				}
				if r.ID != wantID || r.PartNumber != part || r.IsMultiPart != multi {
					t.Fatalf("%c got %+v", letters[i], r)
				}
			}
		})
	}
}
func TestAuditPriorCatalogSuffixVariantsUnchanged(t *testing.T) {
	m, _ := NewMatcher(&Config{})
	for _, suffix := range []string{"E", "Z"} {
		for _, tail := range []string{"", "-cd2", "-part03", "-1080p"} {
			n := "IPX-535" + suffix + tail + ".mkv"
			t.Run(n, func(t *testing.T) {
				r := m.MatchFile(models.FileMatchInfo{Name: n, Extension: ".mkv"})
				rs := ValidateMultipartInDirectory([]MatchResult{*r})
				if rs[0].ID != "IPX-535"+suffix {
					t.Fatalf("got %+v", rs[0])
				}
			})
		}
	}
	for _, n := range []string{"1rct00156h.mkv", "1rct00156h.cd2.mkv", "189dts00087.mkv", "h_1472smkcx003.mkv"} {
		r := m.MatchFile(models.FileMatchInfo{Name: n, Extension: ".mkv"})
		want := strings.ToUpper(strings.Split(n, ".")[0])
		if r == nil || r.ID != want || m.MatchString(n) != want {
			t.Fatalf("%s got %+v", n, r)
		}
	}
}
