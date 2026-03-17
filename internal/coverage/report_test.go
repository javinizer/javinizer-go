package coverage

import (
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

	summary, err := Analyze(profile)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
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

	_, err := Analyze(strings.NewReader("not-a-profile\n"))
	if err == nil {
		t.Fatal("Analyze() error = nil, want error")
	}
}
