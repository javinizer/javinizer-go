package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyze(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader(`mode: count
example.go:1.1,3.10 2 1
example.go:2.1,4.10 1 0
example.go:5.1,5.8 1 0
example.go:5.1,5.8 1 2
other.go:10.1,10.4 1 0
`)

	summary, err := analyze(profile, ReportOptions{})
	if err != nil {
		t.Fatalf("analyze() error = %v", err)
	}

	if summary.Line.Total != 6 {
		t.Fatalf("Line.Total = %d, want 6", summary.Line.Total)
	}
	if summary.Line.Hit != 2 {
		t.Fatalf("Line.Hit = %d, want 2", summary.Line.Hit)
	}
	if summary.Line.Partial != 2 {
		t.Fatalf("Line.Partial = %d, want 2", summary.Line.Partial)
	}
	if summary.Line.Miss != 2 {
		t.Fatalf("Line.Miss = %d, want 2", summary.Line.Miss)
	}
	if got := summary.Line.Percent; got < 33.3 || got > 33.4 {
		t.Fatalf("Line.Percent = %.4f, want about 33.33", got)
	}

	if summary.Statement.Total != 5 {
		t.Fatalf("Statement.Total = %d, want 5", summary.Statement.Total)
	}
	if summary.Statement.Covered != 3 {
		t.Fatalf("Statement.Covered = %d, want 3", summary.Statement.Covered)
	}
	if got := summary.Statement.Percent; got < 59.9 || got > 60.1 {
		t.Fatalf("Statement.Percent = %.4f, want about 60.0", got)
	}
}

func TestAnalyzeRejectsInvalidHeader(t *testing.T) {
	t.Parallel()

	_, err := analyze(strings.NewReader("not-a-profile\n"), ReportOptions{})
	if err == nil {
		t.Fatal("analyze() error = nil, want error")
	}
}

func TestAnalyzeProfile(t *testing.T) {
	t.Parallel()

	t.Run("returns error when file cannot be opened", func(t *testing.T) {
		_, err := AnalyzeProfile(filepath.Join(t.TempDir(), "missing.out"))
		if err == nil {
			t.Fatal("AnalyzeProfile() error = nil, want error")
		}
	})

	t.Run("analyzes valid profile file", func(t *testing.T) {
		profilePath := filepath.Join(t.TempDir(), "cover.out")
		content := `mode: count
example.go:1.1,1.10 1 1
example.go:2.1,2.10 1 0
`
		if err := os.WriteFile(profilePath, []byte(content), 0644); err != nil {
			t.Fatalf("write profile: %v", err)
		}

		summary, err := AnalyzeProfile(profilePath)
		if err != nil {
			t.Fatalf("AnalyzeProfile() error = %v", err)
		}

		if summary.Line.Total != 2 || summary.Line.Hit != 1 || summary.Line.Miss != 1 {
			t.Fatalf("unexpected line summary: %+v", summary.Line)
		}
	})
}

func TestAnalyzeRejectsMalformedProfileLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile string
	}{
		{
			name: "missing fields",
			profile: `mode: count
example.go:1.1,1.10 1
`,
		},
		{
			name: "invalid statement count",
			profile: `mode: count
example.go:1.1,1.10 x 1
`,
		},
		{
			name: "invalid span",
			profile: `mode: count
example.go:1.1-1.10 1 1
`,
		},
		{
			name: "invalid position",
			profile: `mode: count
example.go:1,1.10 1 1
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := analyze(strings.NewReader(tt.profile), ReportOptions{})
			if err == nil {
				t.Fatal("analyze() error = nil, want error")
			}
		})
	}
}

func TestAnalyze_IgnoreGlobs(t *testing.T) {
	t.Parallel()

	// Two files: one covered, one fully missed. With docs/swagger ignored, the
	// missed file must not drag down the Codecov-style line percentage — this
	// is the project-mode parity Codecov applies via its ignore: list.
	mkProfile := func() *strings.Reader {
		return strings.NewReader("mode: atomic\n" +
			"github.com/javinizer/javinizer-go/internal/x/ok.go:4.1,6.1 2 1\n" +
			"github.com/javinizer/javinizer-go/docs/swagger/docs.go:10.1,12.1 2 0\n")
	}

	plain, err := analyze(mkProfile(), ReportOptions{})
	if err != nil {
		t.Fatalf("analyze() error = %v", err)
	}
	if plain.Line.Percent >= 100 {
		t.Fatalf("without ignores, line coverage = %.2f, want < 100 (missed swagger file counted)", plain.Line.Percent)
	}

	filtered, err := analyze(mkProfile(), ReportOptions{
		IgnoreGlobs:  []string{"docs/swagger/**"},
		ModulePrefix: "github.com/javinizer/javinizer-go/",
	})
	if err != nil {
		t.Fatalf("analyze() error = %v", err)
	}
	if filtered.Line.Percent != 100 || filtered.Line.Total != 3 {
		t.Fatalf("with ignores, line coverage = %.2f%% (%d lines), want 100%% over 3 lines",
			filtered.Line.Percent, filtered.Line.Total)
	}
	if filtered.Statement.Total != 2 {
		t.Fatalf("with ignores, statements total = %d, want 2 (swagger excluded)", filtered.Statement.Total)
	}
}

func TestPercentage(t *testing.T) {
	t.Parallel()

	if got := percentage(0, 0); got != 100 {
		t.Fatalf("percentage(0,0) = %.2f, want 100", got)
	}
	if got := percentage(1, 4); got != 25 {
		t.Fatalf("percentage(1,4) = %.2f, want 25", got)
	}
}
