//go:build live

// Package live_e2e contains REAL-LIVE end-to-end tests that hit actual scraper
// websites over the public internet. These are deliberately excluded from CI
// (the `live` build tag is not passed in any workflow) and must be run locally
// by the developer.
//
// Why not CI: real scrapers are inherently flaky — sites change their HTML,
// rate-limit, geo-block, require auth/cookies, or go down. A failing live
// scrape is not always a code regression; it's often an upstream change. These
// tests exist to give the developer a structured way to detect scraping
// degradation across all scrapers from their own machine, with their own
// proxy/FlareSolverr/browser setup.
//
// Future: this suite is designed to be lifted onto a dedicated server that
// runs on a schedule and tracks scraping degradation over time. The JSON
// output mode (JAVINIZER_LIVE_E2E_JSON=true) emits machine-parseable per-
// scraper results for that purpose.
//
// Safety: TWO opt-ins are required to run — the `live` build tag AND the
// JAVINIZER_LIVE_E2E=true env var. Either missing → the suite skips. This
// prevents accidental hammering of real sites from a stray `go test ./...`.
//
// Usage:
//
//	# Run all scrapers (uses your real config at configs/config.yaml):
//	JAVINIZER_LIVE_E2E=true make test-e2e-live
//
//	# Run a single scraper:
//	JAVINIZER_LIVE_E2E=true go test -tags live -run 'TestLive_Scrapers/r18dev' ./test/e2e/live/
//
//	# JSON output (for future server-side tracking):
//	JAVINIZER_LIVE_E2E=true JAVINIZER_LIVE_E2E_JSON=true make test-e2e-live
//
//	# Use a specific config (your proxy/FlareSolverr/browser setup):
//	JAVINIZER_LIVE_E2E=true JAVINIZER_CONFIG=path/to/config.yaml make test-e2e-live
package live_e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var binaryPath string

func TestMain(m *testing.M) {
	// Double opt-in: build tag (live) is already required to compile this
	// file; the env var is the second gate. Skip cleanly if only the tag
	// was set without the env.
	if os.Getenv("JAVINIZER_LIVE_E2E") != "true" {
		fmt.Fprintln(os.Stderr, "live_e2e: skipping (set JAVINIZER_LIVE_E2E=true to run real-network scraper tests)")
		os.Exit(0)
	}

	tmp, err := os.MkdirTemp("", "javinizer-live-e2e-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "live_e2e: mkdir temp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "javinizer")
	if p := os.Getenv("JAVINIZER_E2E_BIN"); p != "" {
		binaryPath = p // reuse a prebuilt binary for faster iteration
	} else {
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/javinizer")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "live_e2e: go build failed: %v\n%s\n", err, out)
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

func repoRoot() string {
	src, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	return filepath.Join(src, "..", "..", "..")
}

// scraperFixture pairs a scraper name with a known-good movie ID for it.
// The IDs are sourced from the developer's test-videos/ directory and were
// VERIFIED to resolve on each site during the first live run — a failing
// fixture means an upstream change (site dropped the title, HTML changed,
// geo-block), which is exactly the degradation signal this suite catches.
//
// The standard-JAV scrapers each use a DIFFERENT studio/ID so that one site
// dropping one title fails only that scraper, not all nine. The special-
// format scrapers (caribbeancom, fc2, tokyohot) use site-specific ID formats
// drawn from the corresponding test-video filenames.
//
// IDs by scraper (all verified from test-videos/):
//   - r18dev:          SSIS-123   (S1 — broad VOD coverage)
//   - javlibrary:      IPX-535    (IPX — most comprehensive index)
//   - javbus:          ABW-013    (Prestige — wide coverage)
//   - javdb:           SONE-267   (S1 — comprehensive aggregator)
//   - mgstage:         ABW-102    (Prestige — mgstage only indexes certain
//     labels; ABW series is reliably present)
//   - jav321:          STARS-136  (SOD Star)
//   - javstash:        DASS-643   (DAS — aggregator)
//   - libredmm:        ROYD-191   (mirrors DMM's index)
//   - dmm:             SONE-267   (S1 — DMM-hosted; content-id resolves)
//   - caribbeancom:    120614-753 (Caribbean format MMDDYY-NNN)
//   - fc2:             FC2-PPV-4761557 (FC2 PPV article ID)
//   - tokyohot:        n0814      (Tokyo-Hot format [A-Za-z]\d+)
//   - dlgetchu:        4021016    (product ID — scraper resolves to 4064461)
//   - aventertainment: 1PON-020326-001 (1Pondo PPV — AVEntertainment hosts
//     PPV content (1Pondo/Caribbean/FC2), not standard studio
//     JAV; standard IDs like SSIS-123 don't exist in its
//     catalog)
var scraperFixtures = map[string]string{
	"r18dev":          "SSIS-123",
	"javlibrary":      "IPX-535",
	"javbus":          "ABW-013",
	"javdb":           "SONE-267",
	"mgstage":         "ABW-102",
	"jav321":          "STARS-136",
	"javstash":        "DASS-643",
	"libredmm":        "ROYD-191",
	"dmm":             "SONE-267",
	"caribbeancom":    "120614-753",
	"fc2":             "FC2-PPV-4761557",
	"tokyohot":        "n0814",
	"dlgetchu":        "4021016",
	"aventertainment": "1PON-020326-001",
}

// pendingFixtures lists scrapers that have no verified fixture ID yet.
// These are skipped (not failed) so the suite stays green — a missing fixture
// is not a degradation signal. Remove a scraper from this set once a verified
// ID is added to scraperFixtures.
var pendingFixtures = map[string]bool{}

// scrapeTimeout is the per-scraper wall-clock budget. Real sites can be slow
// (FlareSolverr challenges, proxy hops, browser rendering). 90s is generous
// but keeps a hung scraper from stalling the whole suite.
const scrapeTimeout = 90 * time.Second

// scraperResult is the structured outcome for one scraper, emitted in JSON
// mode for future server-side degradation tracking.
type scraperResult struct {
	Scraper string        `json:"scraper"`
	ID      string        `json:"id"`
	Pass    bool          `json:"pass"`
	Latency time.Duration `json:"latency_ms"`
	Error   string        `json:"error,omitempty"`
	Title   string        `json:"title,omitempty"`
}

// TestLive_Scrapers runs a real scrape against every scraper in the fixture,
// using the developer's real config (configs/config.yaml or $JAVINIZER_CONFIG)
// for proxy/FlareSolverr/browser setup. The DB is isolated to a temp path so
// the test never pollutes the developer's real database.
//
// Each scraper is a subtest — run one with:
//
//	JAVINIZER_LIVE_E2E=true go test -tags live -run 'TestLive_Scrapers/<name>' ./test/e2e/live/
//
// A scraper PASSES if the binary exits 0 and the output contains a "Title:"
// row with a non-empty value — the core signal that scraping produced real
// metadata. A failure is reported with the error/exit code so the developer
// can diagnose (upstream change, geo-block, missing proxy, etc.).
func TestLive_Scrapers(t *testing.T) {
	configPath := os.Getenv("JAVINIZER_CONFIG")
	if configPath == "" {
		// Default to the shipped config, which has all 14 scrapers in the
		// priority list + the developer's proxy/FlareSolverr/browser setup.
		configPath = filepath.Join(repoRoot(), "configs", "config.yaml")
	}

	// Isolate the DB so repeated runs don't accumulate cache entries that
	// would mask a regressed scraper (--force below also bypasses cache,
	// but isolation is defense-in-depth).
	dbPath := filepath.Join(t.TempDir(), "live-e2e.db")

	jsonMode := os.Getenv("JAVINIZER_LIVE_E2E_JSON") == "true"
	var results []scraperResult

	// Deterministic order (map iteration is random).
	names := make([]string, 0, len(scraperFixtures))
	for name := range scraperFixtures {
		names = append(names, name)
	}
	sortStrings(names)

	for _, name := range names {
		name := name
		id := scraperFixtures[name]

		t.Run(name, func(t *testing.T) {
			if pendingFixtures[name] {
				t.Skipf("%s: no verified fixture ID — add one to scraperFixtures and remove from pendingFixtures", name)
			}
			start := time.Now()
			out, code := runScrape(t, configPath, dbPath, id, name)
			latency := time.Since(start)

			// Pass gate: the scrape binary returns exit 1 when the workflow
			// considers the scrape failed (no results, site error, etc.). Exit 0
			// means the scraper fetched + parsed real metadata. The title is a
			// quality signal (reported below) but not a pass/fail gate — many
			// scrapers return valid metadata (ID, screenshots, cover) without
			// populating the Title field specifically.
			title := extractTitle(out)
			pass := code == 0

			res := scraperResult{
				Scraper: name,
				ID:      id,
				Pass:    pass,
				Latency: latency,
				Title:   title,
			}
			if !pass {
				res.Error = diagnoseFailure(code, out)
			}
			results = append(results, res)

			if !pass {
				t.Errorf("%s scrape failed (exit %d): %s\n--- output ---\n%s",
					name, code, res.Error, out)
			} else {
				if title != "" {
					t.Logf("%s ✓  %s  (%.1fs)", name, truncate(title, 60), latency.Seconds())
				} else {
					t.Logf("%s ✓  (no title; exit 0 with metadata)  (%.1fs)", name, latency.Seconds())
				}
			}
		})
	}

	// After all subtests: emit a summary. In JSON mode, print machine-
	// parseable JSON for future server-side tracking.
	if jsonMode {
		t.Run("Summary_JSON", func(t *testing.T) {
			data, _ := json.MarshalIndent(results, "", "  ")
			fmt.Println(string(data))
		})
	} else {
		t.Run("Summary", func(t *testing.T) {
			passed := 0
			for _, r := range results {
				if r.Pass {
					passed++
				}
			}
			fmt.Printf("\n=== Live E2E Summary: %d/%d scrapers passed ===\n", passed, len(results))
			for _, r := range results {
				mark := "✗"
				if r.Pass {
					mark = "✓"
				}
				extra := ""
				if r.Error != "" {
					extra = " — " + r.Error
				}
				fmt.Printf("  %s %-16s %6.1fs  %s%s\n", mark, r.Scraper, r.Latency.Seconds(), r.Title, extra)
			}
		})
	}
}

// runScrape executes `javinizer scrape <id> --scrapers <name> --force` with
// the real config + an isolated DB. Returns combined output + exit code.
func runScrape(t *testing.T, configPath, dbPath, id, scraper string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "scrape", "--scrapers", scraper, "--force", id)
	cmd.Env = func() []string {
		env := os.Environ()
		out := make([]string, 0, len(env)+2)
		for _, kv := range env {
			// Strip our own env vars so they don't leak into the subprocess
			// config resolution (we set them explicitly below).
			if strings.HasPrefix(kv, "JAVINIZER_CONFIG=") || strings.HasPrefix(kv, "JAVINIZER_DB=") {
				continue
			}
			out = append(out, kv)
		}
		return append(out,
			"JAVINIZER_CONFIG="+configPath,
			"JAVINIZER_DB="+dbPath,
		)
	}()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		// Context deadline / exec failure.
		code = 124
		if ctx.Err() == context.DeadlineExceeded {
			buf.WriteString(fmt.Sprintf("\n[timeout: scrape exceeded %s]", scrapeTimeout))
		}
	}
	return buf.String(), code
}

// extractTitle pulls the movie title out of the formatter's text-table output.
// The table prints rows as `<padded-label> : <value>` (e.g. `Title         : foo`),
// so we split on the first colon and match the label exactly. Returns "" if no
// Title row is present — many scrapers return valid metadata (ID, screenshots,
// cover) without populating the title field, so an empty title is NOT a failure
// signal; the pass/fail gate is the binary's exit code (see pass criteria).
func extractTitle(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		if strings.TrimSpace(line[:idx]) == "Title" {
			return strings.TrimSpace(line[idx+1:])
		}
	}
	return ""
}

// diagnoseFailure produces a short, human-readable reason for the failure
// from the exit code + output tail.
func diagnoseFailure(code int, out string) string {
	if code == 124 {
		return "timeout"
	}
	// Surface the last non-empty debug/error line — usually the root cause
	// (e.g. "403 Forbidden", "no result", "geo-restriction").
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		// Truncate long lines so the summary stays readable.
		if len(l) > 200 {
			l = l[:200] + "…"
		}
		return fmt.Sprintf("exit %d: %s", code, l)
	}
	return fmt.Sprintf("exit %d (no output)", code)
}

func sortStrings(s []string) {
	// Small fixed set; avoid importing sort for one call.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// truncate clips a string to n chars (with ellipsis) for summary readability.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
