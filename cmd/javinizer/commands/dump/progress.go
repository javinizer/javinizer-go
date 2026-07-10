package dump

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-isatty"
)

const (
	barWidth    = 30
	refreshRate = 100 * time.Millisecond
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// progressBar renders download progress to w. When w is a terminal, it uses a
// single carriage-return-overwritten line (a filled bar with percentage when
// the total is known, or an animated spinner when unknown). When w is not a
// terminal (piped, redirected, or CI), each draw is emitted on its own line so
// logs remain readable without \r artifacts. Redraws are throttled to
// refreshRate so high-frequency Read callbacks don't flood the output.
type progressBar struct {
	w         io.Writer
	tty       bool
	total     int64
	n         int64
	lastDraw  time.Time
	spinFrame int
	drawn     bool
	finished  bool
	prevWidth int
}

func newProgressBar(w io.Writer, total int64) *progressBar {
	tty := false
	if f, ok := w.(*os.File); ok {
		tty = isatty.IsTerminal(f.Fd())
	}
	return &progressBar{w: w, tty: tty, total: total}
}

func (p *progressBar) update(n, total int64) {
	if p.finished {
		return
	}
	if total > 0 {
		p.total = total
	}
	p.n = n
	if p.drawn && time.Since(p.lastDraw) < refreshRate {
		return
	}
	p.draw()
}

func (p *progressBar) finish() {
	if p.finished {
		return
	}
	p.finished = true
	if !p.drawn {
		return
	}
	p.draw()
	if p.tty {
		_, _ = fmt.Fprint(p.w, "\n")
	}
}

func (p *progressBar) draw() {
	p.lastDraw = time.Now()
	p.drawn = true
	var line string
	if p.total > 0 {
		pct := float64(p.n) / float64(p.total)
		if pct > 1 {
			pct = 1
		}
		if pct < 0 {
			pct = 0
		}
		filled := int(pct * float64(barWidth))
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		line = fmt.Sprintf("  [%s] %5.1f%%  %s / %s", bar, pct*100, formatBytes(p.n), formatBytes(p.total))
	} else {
		frame := spinFrames[p.spinFrame%len(spinFrames)]
		p.spinFrame++
		line = fmt.Sprintf("  %s  downloaded %s", frame, formatBytes(p.n))
	}
	if p.tty {
		width := utf8.RuneCountInString(line)
		if width < p.prevWidth {
			line += strings.Repeat(" ", p.prevWidth-width)
		}
		p.prevWidth = width
		line = "\r" + line
	} else {
		line += "\n"
	}
	_, _ = fmt.Fprint(p.w, line)
}

func formatBytes(n int64) string {
	const mb = 1024 * 1024
	if n >= mb {
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	}
	const kb = 1024
	if n >= kb {
		return fmt.Sprintf("%.1f KB", float64(n)/kb)
	}
	return fmt.Sprintf("%d B", n)
}
