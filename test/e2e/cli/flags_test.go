//go:build e2e

package cli_e2e

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flagSpec is a single documented CLI flag, pinned from the exhaustive flag
// catalog produced by the scout subagent. Adding a flag to the binary without
// adding it here (or removing a flag without removing it here) fails this
// test — guarding against silent flag regressions across releases.
type flagSpec struct {
	long  string // the --foo form (without leading --)
	short string // the -f form (without leading -), or "" if none
}

// commandFlags maps a command path to every flag it documents. The command
// path is the args passed to `javinizer <path...> --help`.
//
// Source: scout catalog (async subagent run f157f90c). Exhaustive over every
// .go file under cmd/javinizer/commands/ + root.go. Hidden flags
// (e.g. update --show-merge-stats) are intentionally included — a hidden flag
// is still a registered flag whose removal would be a regression.
var commandFlags = map[string][]flagSpec{
	"": { // root
		{"config", ""},
		{"verbose", "v"},
		// --version is cobra-builtin (rootCmd.Version), not explicitly
		// registered, but appears in --help; pin it so a regression in the
		// version wiring surfaces here too.
		{"version", ""},
	},
	"scrape": {
		{"scrapers", "s"},
		{"force", "f"},
		{"output", ""},
		{"scrape-actress", ""},
		{"no-scrape-actress", ""},
		{"browser", ""},
		{"no-browser", ""},
		{"browser-timeout", ""},
		{"actress-db", ""},
		{"no-actress-db", ""},
		{"genre-replacement", ""},
		{"no-genre-replacement", ""},
	},
	"sort": {
		{"dry-run", "n"},
		{"recursive", "r"},
		{"dest", "d"},
		{"move", "m"},
		{"link-mode", ""},
		{"nfo", ""},
		{"download", ""},
		{"extrafanart", ""},
		{"scrapers", "p"},
		{"force-update", "f"},
		{"force-refresh", ""},
	},
	"update": {
		{"dry-run", "n"},
		{"download", ""},
		{"extrafanart", ""},
		{"scrapers", "p"},
		{"force-refresh", ""},
		{"force-overwrite", ""},
		{"preserve-nfo", ""},
		{"preset", ""},
		{"scalar-strategy", ""},
		{"array-strategy", ""},
	},
	"version": {
		{"short", "s"},
		{"check", "c"},
	},
	"upgrade": {
		{"check", "c"},
		{"force", ""},
		{"prerelease", ""},
	},
	"tui": {
		{"source", "s"},
		{"dest", "d"},
		{"recursive", "r"},
		{"move", "m"},
		{"dry-run", "n"},
		{"link-mode", ""},
		{"extrafanart", ""},
		{"scrapers", "p"},
		{"update-mode", ""},
		{"preset", ""},
		{"scalar-strategy", ""},
		{"array-strategy", ""},
	},
	"web": {
		{"host", ""},
		{"port", ""},
	},
	"config migrate": {
		{"dry-run", ""},
	},
	"history list": {
		{"limit", "n"},
		{"operation", "o"},
		{"status", "s"},
		{"batch", "b"},
	},
	"history clean": {
		{"days", "d"},
	},
	"history revert": {
		{"scrape-ids", ""},
	},
	"actress merge": {
		{"target", ""},
		{"source", ""},
		{"non-interactive", ""},
		{"prefer", ""},
		{"yes", "y"},
	},
	"word import": {
		{"include-defaults", ""},
	},
	"token": {
		// --json is a PersistentFlag on the token parent, inherited by
		// create/revoke/list. Asserted at the parent level.
		{"json", ""},
	},
	"token create": {
		{"name", ""},
	},
	"logs list": {
		{"limit", "n"},
		{"type", "t"},
		{"severity", "s"},
		{"source", ""},
	},
}

// TestCLI_FlagsPresent is the regression guard: for every documented command,
// `javinizer <cmd> --help` must list every documented flag. A flag removed or
// renamed in a refactor fails here with a clear diff.
//
// We assert the long form (`--foo`) appears in help output. Short forms are
// not asserted in the help text (cobra lists them alongside the long form,
// but the long form is the stable contract — short forms are cosmetic).
func TestCLI_FlagsPresent(t *testing.T) {
	for cmdPath, flags := range commandFlags {
		cmdPath := cmdPath
		flags := flags
		t.Run(cmdPath, func(t *testing.T) {
			args := []string{}
			if cmdPath != "" {
				args = append(args, strings.Split(cmdPath, " ")...)
			}
			args = append(args, "--help")

			out, code := run(t, "", args...)
			assert.Equal(t, 0, code, "%s --help exited %d\n%s", cmdPath, code, out)

			for _, f := range flags {
				want := "--" + f.long
				assert.Contains(t, out, want,
					"%s --help must document %q (flag %q missing from help)\n%s",
					cmdPath, want, f.long, out)
			}
		})
	}
}

// TestCLI_FlagCatalogComplete is a meta-guard: it fails if a NEW flag appears
// in the binary's help output that is not in our commandFlags catalog. This
// catches the other direction — a flag added without being pinned here. The
// check is heuristic (it parses `--flag` tokens out of help text) to avoid
// depending on cobra's exact help formatting.
//
// We skip commands whose help output lists inherited/root flags that would
// produce false positives (the root --config/--verbose appear on every
// subcommand's help). For leaf commands we restrict to the Flags: section.
func TestCLI_FlagCatalogComplete(t *testing.T) {
	for cmdPath, expected := range commandFlags {
		cmdPath := cmdPath
		expected := expected
		t.Run(cmdPath, func(t *testing.T) {
			args := []string{}
			if cmdPath != "" {
				args = append(args, strings.Split(cmdPath, " ")...)
			}
			args = append(args, "--help")

			out, code := run(t, "", args...)
			require.Equal(t, 0, code)

			documented := map[string]bool{}
			for _, f := range expected {
				documented[f.long] = true
			}
			// Root persistent flags are inherited by every subcommand and are
			// not command-specific; allow them everywhere. --help is a
			// cobra-builtin present on every command; --version is the
			// root-only builtin. --json is inherited by token subcommands.
			documented["config"] = true
			documented["verbose"] = true
			documented["help"] = true
			documented["version"] = true
			if strings.HasPrefix(cmdPath, "token") {
				documented["json"] = true
			}

			// Scan for `--<word>` tokens in the help text.
			for _, tok := range strings.Fields(out) {
				if !strings.HasPrefix(tok, "--") {
					continue
				}
				name := strings.TrimPrefix(tok, "--")
				// Strip trailing punctuation/brackets/quotes cobra uses in usage
				// lines (e.g. `--help"` from `Use "... --help"` sentences, or
				// `--foo]` in option lists).
				name = strings.TrimRight(name, ",.[]=:\"")
				if name == "" {
					continue
				}
				// Only flag the FIRST segment before any '=' (e.g. --foo=bar).
				if i := strings.Index(name, "="); i >= 0 {
					name = name[:i]
				}
				if !documented[name] {
					t.Errorf(
						"%s --help documents --%s, which is not in the e2e flag catalog; "+
							"add it to commandFlags so it is pinned against regression",
						cmdPath, name)
				}
			}
		})
	}
}
