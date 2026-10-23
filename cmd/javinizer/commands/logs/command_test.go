package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
)

// These tests deliberately run serially: the current CLI writes to os.Stdout,
// rather than Cobra's output writer. A file avoids pipe-buffer deadlocks and
// preserves the output contract without introducing a production-only test seam.
func captureOutput(t *testing.T, run func() error) (string, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = original; _ = f.Close() }()
	runErr := run()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), runErr
}

func logFixture(t *testing.T, events []models.Event) string {
	t.Helper()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(t.TempDir(), "events.db")
	cfg.Database.LogLevel = "silent"
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := deps.Close(); err != nil {
			t.Error(err)
		}
	}()
	repo := database.NewEventRepository(deps.DB)
	for i := range events {
		if err := repo.Create(context.Background(), &events[i]); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func executeLogs(t *testing.T, ctx context.Context, path string, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "javinizer", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "configuration file")
	root.AddCommand(NewCommand())
	root.SetArgs(append([]string{"--config", path, "logs"}, args...))
	return captureOutput(t, func() error { return root.ExecuteContext(ctx) })
}

func requireContains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("output missing %q:\n%s", s, got)
		}
	}
}

func TestLogsListEmptyAndNilContext(t *testing.T) {
	path := logFixture(t, nil)
	out, err := executeLogs(t, context.Background(), path, "list")
	if err != nil {
		t.Fatal(err)
	}
	if out != "No events found\n" {
		t.Fatalf("unexpected output: %q", out)
	}
	// Calling the handler directly also exercises its documented nil-context fallback.
	cmd, _, err := NewCommand().Find([]string{"list"})
	if err != nil {
		t.Fatal(err)
	}
	out, err = captureOutput(t, func() error { return runLogsList(cmd, nil, path) })
	if err != nil || out != "No events found\n" {
		t.Fatalf("nil context: output=%q error=%v", out, err)
	}
}

func TestLogsListFormattingAndContext(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	longMessage := strings.Repeat("m", 61)
	longError := strings.Repeat("e", 81)
	events := []models.Event{
		{ID: 1, EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Source: "old", Message: "oldest", CreatedAt: at},
		{ID: 2, EventType: models.EventCategoryScraper, Severity: models.SeverityError, Source: "r18dev", Message: longMessage, Context: fmt.Sprintf(`{"job_id":"job-7","movie_id":"ABC-123","error":%q}`, longError), CreatedAt: at.Add(time.Second)},
		{ID: 3, EventType: models.EventCategoryOrganize, Severity: models.SeverityWarn, Source: "sort", Message: strings.Repeat("b", 60), Context: `{"job_id":null,"movie_id":null,"error":null}`, CreatedAt: at.Add(2 * time.Second)},
		{ID: 4, EventType: models.EventCategorySystem, Severity: models.SeverityDebug, Source: "parser", Message: "malformed context", Context: `{broken`, CreatedAt: at.Add(3 * time.Second)},
		{ID: 5, EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Source: "parser", Message: "short context", Context: `{"job_id":42,"movie_id":"XYZ-9","error":"short error"}`, CreatedAt: at.Add(4 * time.Second)},
		{ID: 6, EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Source: "parser", Message: "missing fields", Context: `{}`, CreatedAt: at.Add(5 * time.Second)},
	}
	path := logFixture(t, events)
	out, err := executeLogs(t, context.Background(), path, "list")
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, out, "=== Event Logs ===\n", fmt.Sprintf("%-6s %-10s %-8s %-12s %-20s %s\n", "ID", "Type", "Sev", "Source", "Time", "Message"), strings.Repeat("-", 110), "2026-01-02 03:04:06", strings.Repeat("m", 57)+"...", strings.Repeat("b", 60), "       Job: job-7\n", "       Movie: ABC-123\n", "       Job: 42\n", "       Movie: XYZ-9\n", "\x1b[31mError: "+strings.Repeat("e", 77)+"...\x1b[0m\n", "\x1b[31mError: short error\x1b[0m\n", "\nShowing 6 event(s)\n")
	if strings.Contains(out, longMessage) || strings.Contains(out, longError) || strings.Contains(out, "<nil>") || strings.Contains(out, "{broken") {
		t.Errorf("untruncated message/error or invalid context leaked:\n%s", out)
	}
	if strings.Count(out, "       Job:") != 2 || strings.Count(out, "       Movie:") != 2 || strings.Count(out, "Error:") != 2 {
		t.Errorf("unexpected context fields:\n%s", out)
	}
	last := -1
	for _, msg := range []string{"missing fields", "short context", "malformed context", strings.Repeat("b", 60), strings.Repeat("m", 57), "oldest"} {
		idx := strings.Index(out, msg)
		if idx <= last {
			t.Errorf("events not newest first at %q:\n%s", msg, out)
		}
		last = idx
	}
}

func TestLogsListFiltersAndLimit(t *testing.T) {
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	path := logFixture(t, []models.Event{
		{EventType: models.EventCategoryScraper, Severity: models.SeverityError, Source: "r18dev", Message: "first-match", CreatedAt: at},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityError, Source: "r18dev", Message: "latest-match", CreatedAt: at.Add(time.Second)},
		{EventType: models.EventCategoryOrganize, Severity: models.SeverityWarn, Source: "sort", Message: "organize-row", CreatedAt: at.Add(2 * time.Second)},
		{EventType: models.EventCategorySystem, Severity: models.SeverityDebug, Source: "startup", Message: "debug-row", CreatedAt: at.Add(3 * time.Second)},
		{EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Source: "startup", Message: "info-row", CreatedAt: at.Add(4 * time.Second)},
	})
	for _, tt := range []struct {
		name                      string
		flags, included, excluded []string
	}{
		{"combined and short flags", []string{"-t", "scraper", "-s", "error", "--source", "r18dev", "-n", "1"}, []string{"latest-match", "Showing 1 event(s)"}, []string{"first-match", "organize-row", "debug-row", "info-row"}},
		{"organize", []string{"--type", "organize", "--severity", "warn"}, []string{"organize-row", "Showing 1 event(s)"}, []string{"match", "debug-row", "info-row"}},
		{"system debug", []string{"--type", "system", "--severity", "debug"}, []string{"debug-row", "Showing 1 event(s)"}, []string{"match", "organize-row", "info-row"}},
		{"info", []string{"--severity", "info"}, []string{"info-row", "Showing 1 event(s)"}, []string{"match", "organize-row", "debug-row"}},
		{"source only", []string{"--source", "startup"}, []string{"debug-row", "info-row", "Showing 2 event(s)"}, []string{"match", "organize-row"}},
		{"no match", []string{"--source", "absent"}, []string{"No events found\n"}, []string{"=== Event Logs ===", "Showing"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := executeLogs(t, context.Background(), path, append([]string{"list"}, tt.flags...)...)
			if err != nil {
				t.Fatal(err)
			}
			requireContains(t, out, tt.included...)
			for _, s := range tt.excluded {
				if strings.Contains(out, s) {
					t.Errorf("unexpected %q in:\n%s", s, out)
				}
			}
		})
	}
	for _, tt := range []struct{ flag, value, want string }{
		{"--type", "invalid", `invalid event type "invalid": must be one of scraper, organize, system`},
		{"--severity", "fatal", `invalid severity "fatal": must be one of debug, info, warn, error`},
	} {
		t.Run(tt.flag, func(t *testing.T) {
			out, err := executeLogs(t, context.Background(), path, "list", tt.flag, tt.value)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
			if out != "" {
				t.Errorf("unexpected output: %q", out)
			}
		})
	}
}

func TestLogsStats(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprintf("populated=%v", populated), func(t *testing.T) {
			var events []models.Event
			if populated {
				events = []models.Event{
					{EventType: models.EventCategoryScraper, Severity: models.SeverityError, Source: "r18dev", Message: "one"},
					{EventType: models.EventCategoryScraper, Severity: models.SeverityWarn, Source: "r18dev", Message: "two"},
					{EventType: models.EventCategoryOrganize, Severity: models.SeverityInfo, Source: "sort", Message: "three"},
					{EventType: models.EventCategorySystem, Severity: models.SeverityDebug, Source: "startup", Message: "four"},
				}
			}
			path := logFixture(t, events)
			out, err := executeLogs(t, context.Background(), path, "stats")
			if err != nil {
				t.Fatal(err)
			}
			requireContains(t, out, "=== Event Statistics ===\n", "\nBy Type:\n", "\nBy Severity:\n")
			counts := []int{0, 0, 0, 0, 0, 0, 0}
			if populated {
				counts = []int{2, 1, 1, 1, 1, 1, 1}
			}
			for i, label := range []string{"🌐 Scraper:", "📦 Organize:", "⚙️ System:", "❌ Error:", "⚠️ Warn:", "ℹ️ Info:", "🐛 Debug:"} {
				found := false
				for _, line := range strings.Split(out, "\n") {
					if strings.HasPrefix(strings.TrimSpace(line), label) {
						found = true
						want := fmt.Sprintf("%d (%.1f%%)", counts[i], float64(counts[i])*25)
						if !strings.HasSuffix(line, want) {
							t.Errorf("line=%q want suffix=%q", line, want)
						}
					}
				}
				if !found {
					t.Errorf("missing label %q", label)
				}
			}
			if populated {
				requireContains(t, out, "Total Events: 4", "\nBy Source:\n", fmt.Sprintf("  %-15s 2 (50.0%%)\n", "r18dev:"), fmt.Sprintf("  %-15s 1 (25.0%%)\n", "sort:"), fmt.Sprintf("  %-15s 1 (25.0%%)\n", "startup:"))
			} else {
				requireContains(t, out, "Total Events: 0")
				if strings.Contains(out, "By Source:") {
					t.Errorf("empty stats has sources:\n%s", out)
				}
				cmd := &cobra.Command{}
				direct, err := captureOutput(t, func() error { return runLogsStats(cmd, nil, path) })
				if err != nil || direct != out {
					t.Errorf("nil-context stats output=%q err=%v", direct, err)
				}
			}
		})
	}
}

func TestLogsErrors(t *testing.T) {
	path := logFixture(t, nil)
	malformed := filepath.Join(t.TempDir(), "malformed.yaml")
	if err := os.WriteFile(malformed, []byte("database: ["), 0600); err != nil {
		t.Fatal(err)
	}
	// A regular file in the database directory path deterministically prevents
	// initialization even when the test is run as root.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(blocker, "events.db")
	badDB := filepath.Join(t.TempDir(), "bad-db.yaml")
	if err := config.Save(cfg, badDB); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, command := range []string{"list", "stats"} {
		for _, tt := range []struct {
			name, path, prefix string
			ctx                context.Context
			cause              error
		}{
			{"config", malformed, "failed to load config:", context.Background(), nil},
			{"dependencies", badDB, "failed to initialize dependencies:", context.Background(), nil},
			{"query", path, map[string]string{"list": "failed to retrieve events:", "stats": "failed to count events:"}[command], canceled, context.Canceled},
		} {
			t.Run(command+"/"+tt.name, func(t *testing.T) {
				out, err := executeLogs(t, tt.ctx, tt.path, command)
				if err == nil || !strings.HasPrefix(err.Error(), tt.prefix) {
					t.Fatalf("error=%v want prefix=%q", err, tt.prefix)
				}
				if tt.cause != nil && !errors.Is(err, tt.cause) {
					t.Errorf("lost wrapped cause: %v", err)
				}
				if out != "" {
					t.Errorf("error printed success output: %q", out)
				}
			})
		}
	}
}
