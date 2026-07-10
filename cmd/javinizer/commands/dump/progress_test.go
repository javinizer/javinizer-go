package dump

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestProgressBar_KnownTotal_PartialProgress(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 0)
	p.update(50*1024*1024, 100*1024*1024)
	out := buf.String()
	if !strings.Contains(out, "50.0%") {
		t.Errorf("expected 50.0%% in output, got: %q", out)
	}
	filled := strings.Count(out, "█")
	empty := strings.Count(out, "░")
	if filled+empty != barWidth {
		t.Errorf("expected %d total bar cells, got %d (filled=%d, empty=%d)", barWidth, filled+empty, filled, empty)
	}
	if filled != barWidth/2 {
		t.Errorf("expected %d filled cells at 50%%, got %d", barWidth/2, filled)
	}
	if !strings.Contains(out, "50.0 MB") || !strings.Contains(out, "100.0 MB") {
		t.Errorf("expected byte counts in output, got: %q", out)
	}
}

func TestProgressBar_KnownTotal_ZeroProgress(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 0)
	p.update(0, 100*1024*1024)
	out := buf.String()
	if !strings.Contains(out, "0.0%") {
		t.Errorf("expected 0.0%% in output, got: %q", out)
	}
	filled := strings.Count(out, "█")
	empty := strings.Count(out, "░")
	if filled != 0 {
		t.Errorf("expected 0 filled cells at 0%%, got %d", filled)
	}
	if empty != barWidth {
		t.Errorf("expected %d empty cells at 0%%, got %d", barWidth, empty)
	}
}

func TestProgressBar_PercentageClamping(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 0)
	p.update(200, 100)
	out := buf.String()
	if !strings.Contains(out, "100.0%") {
		t.Errorf("expected clamped 100.0%% in output, got: %q", out)
	}
	filled := strings.Count(out, "█")
	if filled != barWidth {
		t.Errorf("expected all %d cells filled when n>total, got %d", barWidth, filled)
	}
}

func TestProgressBar_NegativeN_ClampedToZero(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 0)
	p.update(-1, 100)
	out := buf.String()
	if !strings.Contains(out, "0.0%") {
		t.Errorf("expected clamped 0.0%% for negative n, got: %q", out)
	}
	filled := strings.Count(out, "█")
	empty := strings.Count(out, "░")
	if filled != 0 {
		t.Errorf("expected 0 filled cells for negative n, got %d", filled)
	}
	if empty != barWidth {
		t.Errorf("expected %d empty cells for negative n, got %d", barWidth, empty)
	}
	p.finish()
}

func TestProgressBar_UnknownTotal_Spinner(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 0)
	p.update(5*1024*1024, 0)
	out1 := buf.String()
	if !strings.Contains(out1, "downloaded") {
		t.Errorf("expected 'downloaded' in spinner output, got: %q", out1)
	}
	if !strings.Contains(out1, "5.0 MB") {
		t.Errorf("expected 5.0 MB in spinner output, got: %q", out1)
	}

	buf.Reset()
	p.lastDraw = p.lastDraw.Add(-200 * time.Millisecond)
	p.update(10*1024*1024, 0)
	out2 := buf.String()
	if !strings.Contains(out2, "10.0 MB") {
		t.Errorf("expected 10.0 MB in second spinner draw, got: %q", out2)
	}
	if out1 == out2 {
		t.Errorf("expected spinner frame to advance between draws, got identical output: %q", out1)
	}
	// Assert the specific spinner frames advanced (not just that byte count differs).
	if !strings.Contains(out1, spinFrames[0]) {
		t.Errorf("expected first draw to use frame %q, got: %q", spinFrames[0], out1)
	}
	if !strings.Contains(out2, spinFrames[1]) {
		t.Errorf("expected second draw to use frame %q, got: %q", spinFrames[1], out2)
	}
}

func TestProgressBar_Throttling(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 100)
	p.update(10, 100) // first draw always happens (drawn=false)
	lenAfterFirst := buf.Len()
	if lenAfterFirst == 0 {
		t.Fatal("expected first update to draw immediately")
	}

	// Second update within throttle window — should not draw.
	p.lastDraw = time.Now() // guarantee we're inside the window
	p.update(20, 100)
	if buf.Len() != lenAfterFirst {
		t.Errorf("throttled update should not write new output: len before=%d, after=%d", lenAfterFirst, buf.Len())
	}

	// Third update after throttle window expired — should draw.
	p.lastDraw = p.lastDraw.Add(-200 * time.Millisecond)
	p.update(30, 100)
	if buf.Len() <= lenAfterFirst {
		t.Errorf("expected new draw after throttle window expired: len before=%d, after=%d", lenAfterFirst, buf.Len())
	}
	if !strings.Contains(buf.String(), "30") {
		t.Errorf("expected updated n=30 in output after unthrottled draw, got: %q", buf.String())
	}
}

func TestProgressBar_FinishNoOpWhenUndrawn(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 100)
	p.finish()
	if buf.Len() != 0 {
		t.Errorf("expected no output from finish() when undrawn, got: %q", buf.String())
	}
}

func TestProgressBar_FinishAppendsExactlyOneNewline(t *testing.T) {
	for _, tty := range []bool{false, true} {
		t.Run(fmt.Sprintf("tty=%v", tty), func(t *testing.T) {
			var buf bytes.Buffer
			p := newProgressBar(&buf, 100)
			p.tty = tty
			p.update(50, 100)
			drawLen := buf.Len()
			p.finish()
			out := buf.String()[drawLen:]
			if !strings.HasSuffix(out, "\n") {
				t.Errorf("expected finish to append a trailing newline, got: %q", out)
			}
			if strings.Count(out, "\n") != 1 {
				t.Errorf("expected exactly one newline from finish, got %d in: %q", strings.Count(out, "\n"), out)
			}
			// In TTY mode, finish() adds the only newline (draw doesn't).
			// In non-TTY mode, the final draw already added one; finish() must NOT add another.
		})
	}
}

func TestProgressBar_FinishIdempotent(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 100)
	p.update(50, 100)
	p.finish()
	lenAfterFinish := buf.Len()
	if lenAfterFinish == 0 {
		t.Fatal("expected finish() to produce output after a draw")
	}
	p.finish()
	if buf.Len() != lenAfterFinish {
		t.Errorf("second finish() should not produce extra output: len before=%d, after=%d", lenAfterFinish, buf.Len())
	}
}

func TestProgressBar_UpdateAfterFinishIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 100)
	p.update(50, 100)
	p.finish()
	lenAfterFinish := buf.Len()
	p.update(100, 100)
	if buf.Len() != lenAfterFinish {
		t.Errorf("update after finish should not write: len before=%d, after=%d", lenAfterFinish, buf.Len())
	}
	if p.n != 50 {
		t.Errorf("update after finish should not mutate state: n=%d, want 50", p.n)
	}
}

func TestProgressBar_TTYMode_UsesCarriageReturn(t *testing.T) {
	// Simulate a terminal by forcing tty=true on the bar.
	var buf bytes.Buffer
	p := newProgressBar(&buf, 0)
	p.tty = true
	p.update(50, 100)
	out := buf.String()
	if !strings.HasPrefix(out, "\r") {
		t.Errorf("expected tty mode to prefix with \\r, got: %q", out)
	}
	if strings.HasSuffix(out, "\n") {
		t.Errorf("tty mode should not append a trailing newline per draw, got: %q", out)
	}
}

func TestProgressBar_NonTTYMode_UsesNewlines(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 0)
	// bytes.Buffer is not *os.File, so tty defaults to false.
	if p.tty {
		t.Fatal("expected non-TTY for bytes.Buffer writer")
	}
	p.update(50, 100)
	out := buf.String()
	if strings.HasPrefix(out, "\r") {
		t.Errorf("non-tty mode should not use \\r prefix, got: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("non-tty mode should append a trailing newline per draw, got: %q", out)
	}
}

func TestProgressBar_TTYMode_PadsShorterLine(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressBar(&buf, 0)
	p.tty = true
	// Long line first (renders "500.0 KB").
	p.update(500*1024, 0)
	longVisible := strings.TrimPrefix(buf.String(), "\r")
	longWidth := utf8.RuneCountInString(longVisible)
	// Shorter line (renders "10 B") — should be padded with trailing spaces
	// so its visible width matches the first line.
	p.lastDraw = p.lastDraw.Add(-200 * time.Millisecond)
	buf.Reset()
	p.update(10, 0)
	shortVisible := strings.TrimPrefix(buf.String(), "\r")
	shortWidth := utf8.RuneCountInString(shortVisible)
	if shortWidth != longWidth {
		t.Errorf("expected padded short line width %d to equal long line width %d; short=%q long=%q",
			shortWidth, longWidth, shortVisible, longVisible)
	}
	// Verify the padding is trailing spaces (not internal).
	if !strings.HasSuffix(shortVisible, strings.Repeat(" ", longWidth-utf8.RuneCountInString(strings.TrimRight(shortVisible, " ")))) {
		t.Errorf("expected trailing space padding, got: %q", shortVisible)
	}
}

func TestFormatBytes(t *testing.T) {
	const mb = 1024 * 1024
	const kb = 1024
	testCases := []struct {
		name string
		n    int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"500 bytes", 500, "500 B"},
		{"1023 bytes", 1023, "1023 B"},
		{"exactly 1 KB", kb, "1.0 KB"},
		{"just under 1 MB", mb - 1, "1024.0 KB"},
		{"exactly 1 MB", mb, "1.0 MB"},
		{"5 MB", 5 * mb, "5.0 MB"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatBytes(tc.n); got != tc.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

// Ensure isatty detection is exercised for an *os.File (even if not a real
// terminal in CI, this covers the type-assertion branch).
func TestProgressBar_OsFileWriter(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "pbar")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = f.Close() }()
	p := newProgressBar(f, 0)
	p.update(50, 100)
	p.finish()
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected output written to the *os.File writer")
	}
}
