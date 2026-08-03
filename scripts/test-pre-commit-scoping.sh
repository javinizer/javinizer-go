#!/usr/bin/env bash
# test-pre-commit-scoping.sh — regression matrix for the staged-file scoping
# logic in scripts/pre-commit.sample.
#
# Builds a temporary REAL Go module (binary + auxiliary cmd + shared helpers
# + fixtures + embeds + multi-hop import chains), then replays staged-change
# scenarios against the scoping block EXTRACTED FROM THE HOOK ITSELF (no
# copied logic), asserting GO_PKGS | TEST_PKGS | FULL_SUITE | API_CHANGED |
# BUILD for each scenario.
#
# Usage: bash scripts/test-pre-commit-scoping.sh [path-to-hook]
# Requires: git, go. Writes only to a mktemp dir; exit 1 on any failure.
set -u

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
HOOK=${1:-"$SCRIPT_DIR/pre-commit.sample"}
[ -f "$HOOK" ] || { echo "hook not found: $HOOK" >&2; exit 2; }
# Absolute path: scenarios cd into the scratch repo and run the hook there.
HOOK=$(cd "$(dirname "$HOOK")" && pwd)/$(basename "$HOOK")
command -v git >/dev/null || { echo 'git required' >&2; exit 2; }
command -v go  >/dev/null || { echo 'go required' >&2; exit 2; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cd "$WORK" || exit 2
export GOFLAGS=-mod=mod GOPROXY=off
git init -q
git config user.email test@example.invalid
git config user.name test
git config commit.gpgsign false
# *.db mirrors the real repo's .gitignore (embed-asset masking scenario)
printf '/web/dist/ignored/\n*.syso\nlocal_*.go\n*.log\n*.db\n.worktrees/\n.scratch/\n.tmprepro/\n.planning/\n' > .gitignore
mkdir -p internal/foo internal/bar internal/shared internal/consumer internal/legacy internal/orphan \
         internal/tagged \
         internal/dual_unref internal/dual_ref internal/tree/sub web/frontend/src \
         internal/base internal/mid internal/high internal/database/migrations \
         cmd/javinizer cmd/e2e testdata docs/swagger web/dist internal/foo/testdata configs
printf 'module probe.local/x\n\ngo 1.21\n' > go.mod
: > go.sum
cat > internal/foo/a.go <<'EOF'
package foo

func Foo() int { return 1 }
EOF
cat > internal/foo/b.go <<'EOF'
package foo

func FooB() int { return 2 }
EOF
printf 'package foo\n\nimport (\n\t"testing"\n\t_ "probe.local/x/internal/shared"\n)\n\nfunc TestFoo(t *testing.T) { if Foo() != 1 { t.Fatal() } }\n' > internal/foo/a_test.go
printf 'asset\n' > internal/foo/readme.txt
printf 'fixture\n' > internal/foo/testdata/f.txt
cat > internal/bar/c.go <<'EOF'
package bar

func Bar() int { return 1 }
EOF
cat > internal/shared/s.go <<'EOF'
package shared

func Shared() int { return 1 }
EOF
cat > internal/legacy/l.go <<'EOF'
package legacy

func L() int { return 1 }
EOF
cat > internal/tree/parent.go <<'EOF'
package tree

func T() int { return 1 }
EOF
# deliberately OUTSIDE the binary graph: exact-package scoping must not sweep it
cat > internal/tree/sub/sub.go <<'EOF'
package sub

func S() int { return 1 }
EOF
printf 'export const x = 1;\n' > web/frontend/src/app.ts
# vanish: package whose last tracked .go file can be staged-deleted while an
# ignored sibling (local_*) remains — exercises the deleted-dirs guard union
mkdir -p internal/vanish
cat > internal/vanish/v.go <<'EOF'
package vanish

func V() int { return 1 }
EOF
cat > internal/orphan/o.go <<'EOF'
package orphan

func O() int { return 1 }
EOF
# symlink target for the typechange scenario (valid standalone package foo)
cat > internal/foo/real.go <<'EOF'
package foo

func FooC() int { return 3 }
EOF
# emptied-package fixtures: deleting the only .go file leaves keep.txt behind
cat > internal/dual_unref/d.go <<'EOF'
package dualunref

func U() int { return 1 }
EOF
printf 'stay\n' > internal/dual_unref/keep.txt
cat > internal/dual_ref/d.go <<'EOF'
package dualref

func R() int { return 1 }
EOF
printf 'stay\n' > internal/dual_ref/keep.txt
# consumer: in binary graph; its TEST imports foo and legacy
cat > internal/consumer/consumer.go <<'EOF'
package consumer

func C() int { return 1 }
EOF
printf 'package consumer\n\nimport (\n\t"testing"\n\t_ "probe.local/x/internal/dual_ref"\n\t"probe.local/x/internal/foo"\n\t_ "probe.local/x/internal/legacy"\n)\n\nfunc TestC(t *testing.T) { if foo.Foo() != 1 { t.Fatal() } }\n' > internal/consumer/consumer_test.go
# multi-hop chain: high --test--> mid --regular--> base (closure must reach high)
cat > internal/base/b.go <<'EOF'
package base

func V() int { return 1 }
EOF
cat > internal/mid/m.go <<'EOF'
package mid

import "probe.local/x/internal/base"

// Re-export indirection, like internal/models aliasing scraperconfig types.
func M() int { return base.V() }
EOF
cat > internal/high/h.go <<'EOF'
package high

func H() int { return 1 }
EOF
printf 'package high\n\nimport (\n\t"testing"\n\t"probe.local/x/internal/mid"\n)\n\nfunc TestH(t *testing.T) { if mid.M() != 1 { t.Fatal() } }\n' > internal/high/h_test.go
# database imports migrations (embed consumer chain)
cat > internal/database/db.go <<'EOF'
package database

import "probe.local/x/internal/database/migrations"

func SQL() string { return migrations.Content }
EOF
printf 'package database\n\nimport "testing"\n\nfunc TestDB(t *testing.T) { if SQL() == "" { t.Fatal("empty migration") } }\n' > internal/database/db_test.go
cat > internal/database/migrations/mig.go <<'EOF'
package migrations

import _ "embed"

//go:embed *.sql
var Content string
EOF
printf 'select 1;\n' > internal/database/migrations/001.sql
cat > cmd/javinizer/main.go <<'EOF'
package main

import (
	"fmt"
	_ "probe.local/x/docs/swagger"
	_ "probe.local/x/web"
	"probe.local/x/internal/bar"
	"probe.local/x/internal/consumer"
	"probe.local/x/internal/database"
	"probe.local/x/internal/foo"
	"probe.local/x/internal/mid"
	_ "probe.local/x/internal/tree"
)

func main() { fmt.Println(foo.Foo(), bar.Bar(), consumer.C(), database.SQL(), mid.M()) }
EOF
# auxiliary command: regular reverse dep of foo, NOT built by the binary check
cat > cmd/e2e/main.go <<'EOF'
package main

import (
	"fmt"
	"probe.local/x/internal/foo"
)

func main() { fmt.Println(foo.FooB()) }
EOF
cat > web/embed.go <<'EOF'
package web

import _ "embed"

//go:embed all:dist
var Dist string
EOF
printf '<html/>\n' > web/dist/index.html
cat > "web/foo'bar.go" <<'EOF'
package web
EOF
cat > docs/swagger/embed.go <<'EOF'
package swagger

import _ "embed"

//go:embed swagger.json
var Spec string
EOF
printf '{}\n' > docs/swagger/swagger.json
printf 'rootfix\n' > testdata/x.txt
printf 'hi\n' > docs/readme.md
printf 'example: true\n' > configs/config.yaml.example
# Minimal Makefile so the hook's swagger check (triggered by staged internal/
# changes when swag is installed) has something to run.
printf 'swagger:\n\t@true\n' > Makefile
# Generated fixture files are close to (but not exactly) gofmt-clean —
# normalize rather than hand-format each heredoc.
gofmt -w .
git add -A && git commit -qm init

go list -deps ./cmd/javinizer >/dev/null 2>&1 || { echo 'ERROR: scratch module failed to initialize' >&2; exit 2; }

PASS=0; FAIL=0
check() { # name expected actual
  if [ "$2" = "$3" ]; then
    PASS=$((PASS+1)); echo "ok   - $1"
  else
    FAIL=$((FAIL+1))
    echo "FAIL - $1:"
    echo "  expected [$2]"
    echo "  got      [$3]"
  fi
}
# probe prints "GO_PKGS|TEST_PKGS|FULL_SUITE|API_CHANGED|BUILD" by evaluating
# the scoping block lifted verbatim from the hook (STAGED_GO= .. closing rule).
probe() {
  unset STAGED_GO DELETED_GO MODULE_CHANGED API_CHANGED GO_DIRS GO_PKGS \
        DROPPED_DIRS ASSET_PKGS ASSET_FULL EMBED_PKGS STATIC_PKGS FULL_SUITE \
        STAGED_IMPS REV_PKGS TEST_PKGS LINT_CONFIG LINT_PKGS LINT_SCOPE SHOULD_BUILD
  # color vars (unbound under set -u) for the hook's hard-block messages
  # shellcheck disable=SC2034 # consumed by the eval'd hook code below
  RED='' GREEN='' YELLOW='' BLUE='' NC=''
  # shellcheck disable=SC2016 # sed pattern must match the hook's literal $(
  eval "$(sed -n '/^STAGED_GO=$(git/,/^# ---------------------------------------------------------------------------$/p' "$HOOK")"
  # BUILD column reads the production predicate (SHOULD_BUILD) — no mirror.
  local build=no
  [ -n "$SHOULD_BUILD" ] && build=yes
  printf '%s|%s|%s|%s|%s' "$GO_PKGS" "$TEST_PKGS" "$FULL_SUITE" "$API_CHANGED" "$build"
}
reset() { git reset -q --hard HEAD; git clean -qfdx; }
nl=$'\n'
# Reverse closure of internal/foo: cmd/e2e + cmd/javinizer (regular), consumer (test)
C4="./cmd/e2e${nl}./cmd/javinizer${nl}./internal/consumer${nl}./internal/foo"

reset; printf '\n// x\n' >> internal/foo/a.go; git add internal/foo/a.go
check "modified .go (regular+test rev deps)" "./internal/foo|$C4||1|yes" "$(probe)"

# lint/vet scope includes reverse dependents (consumer-only diagnostics)
reset; printf '\n// x2\n' >> internal/foo/a.go; git add internal/foo/a.go
ck=$( { unset STAGED_GO DELETED_GO MODULE_CHANGED API_CHANGED GO_DIRS GO_PKGS \
      DROPPED_DIRS ASSET_PKGS ASSET_FULL EMBED_PKGS STATIC_PKGS FULL_SUITE \
      STAGED_IMPS REV_PKGS TEST_PKGS LINT_CONFIG LINT_PKGS LINT_SCOPE SHOULD_BUILD \
      FRONTEND_CHANGED FRONTEND_FMT_FILES CHECK_PKGS
  # shellcheck disable=SC2016 # sed pattern must match the hook's literal $(
  eval "$(sed -n '/^STAGED_GO=$(git/,/^# ---------------------------------------------------------------------------$/p' "$HOOK")"
  printf '%s' "$CHECK_PKGS"; } )
check "lint/vet scope includes reverse dependents" "$C4" "$ck"

# C-quoted unicode paths must not evade anchored matchers (API_CHANGED et al.)
reset; printf 'package foo\n\nfunc U() int { return 4 }\n' > "internal/foo/注釈.go"; git add "internal/foo/注釈.go"
check "unicode .go filename" "./internal/foo|$C4||1|yes" "$(probe)"

reset; git rm -q internal/foo/b.go
check "delete .go, pkg survives" "./internal/foo|$C4||1|yes" "$(probe)"

reset; printf '\n' >> go.mod; git add go.mod
check "go.mod -> full suite" "||1|0|yes" "$(probe)"

reset; printf '\n' >> go.sum; git add go.sum
check "go.sum -> full suite" "||1|0|yes" "$(probe)"

check_blocked() { # name grep-pattern — hook must hard-exit with message
  if out=$(probe); then
    FAIL=$((FAIL+1)); echo "FAIL - $1: expected hard block, got [$out]"
  else
    case "$out" in
      *"$2"*) PASS=$((PASS+1)); echo "ok   - $1" ;;
      *) FAIL=$((FAIL+1)); echo "FAIL - $1: blocked with unexpected output:"; printf '%s\n' "$out" | tail -3 ;;
    esac
  fi
}

reset; git rm -q internal/bar/c.go && rmdir internal/bar 2>/dev/null || true
check_blocked "delete last file of pkg (bin-imported, hard block)" "Cannot enumerate"

reset; printf 'more\n' >> docs/readme.md; git add docs/readme.md
check "docs only (all skipped)" "|||0|no" "$(probe)"

reset; printf '\n// y\n' >> internal/foo/b.go; git add internal/foo/b.go; git rm -q internal/foo/a.go internal/foo/a_test.go
check "modify+delete same pkg (dedup)" "./internal/foo|$C4||3|yes" "$(probe)"

reset; git mv internal/foo/b.go internal/bar/b.go
{ sed -i '' 's/^package foo$/package bar/' internal/bar/b.go 2>/dev/null || sed -i 's/^package foo$/package bar/' internal/bar/b.go; }
git add internal/bar/b.go
check "cross-dir rename (both pkgs)" "./internal/bar${nl}./internal/foo|./cmd/e2e${nl}./cmd/javinizer${nl}./internal/bar${nl}./internal/consumer${nl}./internal/foo||2|yes" "$(probe)"

reset; printf '{"x":1}\n' > docs/swagger/swagger.json; git add docs/swagger/swagger.json
check "swagger.json (embed root, rev-expanded)" "|./cmd/javinizer${nl}./docs/swagger||1|yes" "$(probe)"

reset; printf '\n// q\n' >> "web/foo'bar.go"; git add "web/foo'bar.go"
check "apostrophe filename (quote-safe)" "./web|./cmd/javinizer${nl}./web||0|yes" "$(probe)"

reset; printf 'f2\n' >> internal/foo/testdata/f.txt; git add internal/foo/testdata/f.txt
check "per-package fixture -> owning pkg only" "|./internal/foo||1|yes" "$(probe)"

reset; printf 'r2\n' >> testdata/x.txt; git add testdata/x.txt
check "root fixture -> full suite" "||1|0|yes" "$(probe)"

reset; mkdir -p tools; git mv internal/orphan/o.go tools/o.go
{ sed -i '' 's/^package orphan$/package tools/' tools/o.go 2>/dev/null || sed -i 's/^package orphan$/package tools/' tools/o.go; }
git add tools/o.go
check "rename out of internal/ still triggers swagger check" "./tools|./tools|1|1|yes" "$(probe)"

reset; rm internal/orphan/o.go; ln -s ../foo/real.go internal/orphan/o.go; git add -A
check "typechange (file->symlink) counts as change" "./internal/orphan|./internal/orphan|1|1|yes" "$(probe)"

reset; printf '\n// s\n' >> internal/shared/s.go; git add internal/shared/s.go
check "binary-unreachable helper -> full suite" "./internal/shared|./internal/shared|1|1|yes" "$(probe)"

# nested subpackage: editing internal/tree must NOT sweep internal/tree/sub
# (distinct package, outside the binary graph — sweeping would force FULL)
reset; printf '\n// t\n' >> internal/tree/parent.go; git add internal/tree/parent.go
check "nested subpackage not swept" "./internal/tree|./cmd/javinizer${nl}./internal/tree||1|yes" "$(probe)"

# fully tag-excluded package (windows-only on this host): pruned silently,
# no false FULL_SUITE; build still runs via the staged-file trigger
reset; mkdir -p internal/tagged
printf '//go:build windows\n\npackage tagged\n\nfunc W() int { return 1 }\n' > internal/tagged/t.go; git add internal/tagged/t.go
check "tag-excluded package pruned quietly" "|||1|yes" "$(probe)"

# live -> tag-excluded producer with consumers: package enumeration breaks
# (its importer no longer resolves), so this must hard-block, not prune
reset; printf '//go:build windows\n\npackage tree\n\nfunc T() int { return 1 }\n' > internal/tree/parent.go
git add internal/tree/parent.go
check_blocked "live->excluded producer with consumers (hard block)" "Cannot enumerate"

# frontend rename out of src/ must still count (rename detection shows only
# the destination without --no-renames)
reset; git mv web/frontend/src/app.ts web/frontend/app.ts
fe=$( { unset STAGED_GO DELETED_GO MODULE_CHANGED API_CHANGED GO_DIRS GO_PKGS \
      DROPPED_DIRS ASSET_PKGS ASSET_FULL EMBED_PKGS STATIC_PKGS FULL_SUITE \
      STAGED_IMPS REV_PKGS TEST_PKGS LINT_CONFIG LINT_PKGS LINT_SCOPE SHOULD_BUILD \
      FRONTEND_CHANGED FRONTEND_FMT_FILES
  # shellcheck disable=SC2016 # sed pattern must match the hook's literal $(
  eval "$(sed -n '/^STAGED_GO=$(git/,/^# ---------------------------------------------------------------------------$/p' "$HOOK")"
  printf '%s|%s' "$FRONTEND_CHANGED" "$FRONTEND_FMT_FILES"; } )
check "frontend rename out of src/ still detected" "1|web/frontend/app.ts" "$fe"

reset; printf 'a2\n' >> internal/foo/readme.txt; git add internal/foo/readme.txt
check "embed-type asset -> pkg + consumers" "|$C4||1|yes" "$(probe)"

reset; printf '<html2/>\n' > web/dist/index.html; git add web/dist/index.html
check "web/dist asset -> pkg + binary" "|./cmd/javinizer${nl}./web||0|yes" "$(probe)"

# cgo/toolchain source in a Go package dir outside the scanned roots:
# an unsupported .c in a Go package dir must never silently pass: the
# owning package joins resolution and go list rejects it loudly
reset; rm -f web/rogue.c 2>/dev/null; printf 'int rogue(void) { return 0; }\n' > web/rogue.c; git add web/rogue.c
check_blocked "cgo source outside scanned roots (hard block)" "C source files not allowed"

# full Go-recognized toolchain extension set (covering *.m here as the
# outside-root representative; all share one pathspec row per class)
reset; rm -f web/rogue.c 2>/dev/null; printf '@interface Rogue\n@end\n' > web/rogue.m; git add web/rogue.m
check_blocked "objc source outside scanned roots (hard block)" "Cannot resolve staged packages"

# C++ header-only input: headers are pure data to go list (no hard block),
# but must map to the owning package so its checks run
reset; rm -f web/rogue.m 2>/dev/null; printf '#pragma once\n' > web/rogue.hpp; git add web/rogue.hpp
check "header-only input outside scanned roots -> web pkg" "|./cmd/javinizer${nl}./web||0|yes" "$(probe)"

reset; printf 'more: true\n' >> configs/config.yaml.example; git add configs/config.yaml.example
check "configs example -> internal/config (static)" "|./internal/config||0|no" "$(probe)"

reset; printf 'select 2;\n' >> internal/database/migrations/001.sql; git add internal/database/migrations/001.sql
check "migration sql -> embed pkg + consumer" "|./cmd/javinizer${nl}./internal/database${nl}./internal/database/migrations||1|yes" "$(probe)"

reset; printf '\n// v\n' >> internal/base/b.go; git add internal/base/b.go
check "re-export chain -> transitive closure" "./internal/base|./cmd/javinizer${nl}./internal/base${nl}./internal/high${nl}./internal/mid||1|yes" "$(probe)"

reset; git rm -q internal/legacy/l.go
rmdir internal/legacy 2>/dev/null || true
check_blocked "delete referenced test-only pkg (hard block)" "still import"

reset; printf '\n' >> go.mod; git add go.mod; git rm -q internal/legacy/l.go
rmdir internal/legacy 2>/dev/null || true
check_blocked "go.mod + referenced deletion (integrity gate beats FULL_SUITE)" "still import"

reset; printf 'package foo\n\nimport _ "probe.local/x/internal/doesnotexist"\n\nfunc Foo() int { return 1 }\n' > internal/foo/a.go; git add internal/foo/a.go
check_blocked "unresolvable staged import (hard block)" "dependency graph"

# linter config staged: sets LINT_CONFIG (full-repo lint escalation) but must
# not by itself trigger Go package checks
reset; printf 'run:\n  tests: false\n' > .golangci.yml; git add .golangci.yml
lint_cfg=$( { unset STAGED_GO DELETED_GO MODULE_CHANGED API_CHANGED GO_DIRS GO_PKGS \
        DROPPED_DIRS ASSET_PKGS ASSET_FULL EMBED_PKGS STATIC_PKGS FULL_SUITE \
        STAGED_IMPS REV_PKGS TEST_PKGS LINT_CONFIG LINT_PKGS LINT_SCOPE
  # shellcheck disable=SC2016 # sed pattern must match the hook's literal $(
  eval "$(sed -n '/^STAGED_GO=$(git/,/^# ---------------------------------------------------------------------------$/p' "$HOOK")"
  printf '%s|%s|%s' "$LINT_CONFIG" "$GO_PKGS" "$FULL_SUITE"; } )
check "lint config staged -> full-lint flag only" ".golangci.yml||" "$lint_cfg"

# staged Makefile alone must still trigger the swagger check (the make
# swagger recipe is defined there) without triggering Go package checks
reset; printf '# recipe touched\n' >> Makefile; git add Makefile
check "Makefile staged -> swagger trigger" "|||1|no" "$(probe)"

reset; git rm -q internal/orphan/o.go
rmdir internal/orphan 2>/dev/null || true
check "delete orphan pkg (no refs, build only)" "|||1|yes" "$(probe)"

# emptied package dirs (keep.txt survives -> directory remains)
reset; git rm -q internal/dual_unref/d.go
check "empty unreferenced pkg (solo)" "|||1|yes" "$(probe)"

reset; git rm -q internal/dual_unref/d.go; printf '\n// x\n' >> internal/foo/a.go; git add internal/foo/a.go
check "empty unreferenced pkg + valid change" "./internal/foo|$C4||2|yes" "$(probe)"

reset; git rm -q internal/dual_ref/d.go
check_blocked "empty referenced pkg (hard block)" "still import"

# --- end-to-end: execute the hook itself (index/tree guard lives outside
# the classified block, so classification probes cannot reach it) -----------
check_hook() { # name expected_exit [grep-pattern]; set PCFAST= (empty) to run the FULL hook
  local name=$1 want=$2 pat=${3:-} got
  if PRE_COMMIT_FAST=${PCFAST-1} bash "$HOOK" >hook.out 2>&1; then got=0; else got=$?; fi
  if [ "$got" = "$want" ] && { [ -z "$pat" ] || grep -q -- "$pat" hook.out; }; then
    PASS=$((PASS+1)); echo "ok   - $name"
  else
    FAIL=$((FAIL+1)); echo "FAIL - $name: exit=$got (want $want)"
    tail -5 hook.out
  fi
}

reset; printf '\n// e2e clean\n' >> internal/foo/a.go; git add internal/foo/a.go
check_hook "e2e: clean staged change passes" 0

reset; printf '\n// staged part\n' >> internal/foo/a.go; git add internal/foo/a.go
printf '\n// unstaged part\n' >> internal/foo/a.go
check_hook "e2e: partial staging blocked (overlap)" 1 "Offending paths"

reset; git rm -q internal/foo/b.go
printf 'package foo\n\nfunc Recreated() int { return 9 }\n' > internal/foo/b.go
check_hook "e2e: staged delete + unstaged recreate blocked" 1

reset; git rm -q web/dist/index.html
mkdir -p web/dist/ignored && printf 'x\n' > web/dist/ignored/x.js
check_hook "e2e: ignored web/dist artifact masks staged deletion" 1 "Offending paths"

reset
check_hook "e2e: empty stage passes" 0

reset; mkdir -p .planning; printf 'x\n' > ".planning/秘.txt"; git add -f ".planning/秘.txt"
check_hook "e2e: unicode forbidden path blocked" 1 "Blocked"

reset; printf 'go 1.21\n\nuse .\n' > go.work; git add -f go.work
check_hook "e2e: force-staged go.work refused" 1 "go.work"

# a previously-TRACKED workspace file must be removable (deletion != staging)
reset; printf 'go 1.21\n\nuse .\n' > go.work; git add -f go.work; git -c commit.gpgsign=false commit -qm 'track go.work (fixture)'
git rm -q --cached go.work
check_hook "e2e: staged deletion of tracked go.work allowed" 0
git reset -q --hard HEAD >/dev/null; git rm -q --cached go.work 2>/dev/null; reset

reset; printf '\n// staged A\n' >> internal/foo/a.go; git add internal/foo/a.go
printf '\n// unstaged B\n' >> internal/bar/c.go
check_hook "e2e: cross-package unstaged change blocked" 1 "Offending paths"

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
printf '# touched\n' >> Makefile
check_hook "e2e: unstaged Makefile change blocked" 1 "Offending paths"

reset; printf '.text\n.globl broken\nbroken:\n\t.badop\n' > internal/foo/x.s; git add internal/foo/x.s
printf '.text\n.globl broken\nbroken:\n\tRET\n' > internal/foo/x.s
check_hook "e2e: partial staging of toolchain source blocked" 1 "x.s"

reset; printf '\n' >> go.mod; git add go.mod
check_hook "e2e: module change runs full go vet (even under FAST)" 0 "all packages — module/shared-input change"

reset; printf '\n' >> go.mod; printf '\n// mixed\n' >> internal/foo/a.go
git add go.mod internal/foo/a.go
check_hook "e2e: mixed module+Go still runs full go vet" 0 "all packages — module/shared-input change"

reset; printf 'f2\n' >> testdata/x.txt; git add testdata/x.txt
check_hook "e2e: shared fixture escalates lint/vet to full repo" 0 "all packages — module/shared-input change"

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
printf 'syso\n' > internal/foo/x.syso
check_hook "e2e: ignored *.syso build input blocked" 1 "x.syso"

# non-ASCII ignored compile input: default git output C-QUOTES unusual
# paths, the package-dir match then misses, and the masked input evades
# the guard — every scan that feeds a comparison must emit -z raw bytes
reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
printf 'syso\n' > "internal/foo/注釈.syso"
check_hook "e2e: non-ASCII ignored build input blocked" 1 "注釈.syso"

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
printf 'package foo\n\nfunc Local() int { return 1 }\n' > internal/foo/local_extra.go
check_hook "e2e: ignored *.go masked symbol blocked" 1 "local_extra.go"

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
printf 'noise\n' > internal/foo/run.log
check_hook "e2e: harmless ignored runtime artifact does not block" 0

# ignored go:embed ASSET: every check is green locally (go list/vet/build
# consume the working-tree file), but the committed snapshot lacks the
# ignored *.db and fails with 'pattern local.db: no matching files found'.
# The static extension pathspec cannot see arbitrary embed types, so the
# guard must consult the toolchain-resolved embed set.
# (db_embed.go must not match the fixture gitignore's local_*.go rule —
# only the ASSET is ignored here)
reset; cat > internal/database/db_embed.go <<'EOF'
package database

import _ "embed"

//go:embed local.db
var LocalDB string
EOF
printf 'local\n' > internal/database/local.db
git add internal/database/db_embed.go
check_hook "e2e: ignored go:embed asset masked (local.db)" 1 "local.db"

# control: the SAME embed with the asset staged is consistent — the
# index-membership subtraction must not produce a false positive
reset; cat > internal/database/db_embed.go <<'EOF'
package database

import _ "embed"

//go:embed local.db
var LocalDB string
EOF
printf 'local\n' > internal/database/local.db
git add internal/database/db_embed.go
git add -f internal/database/local.db
check_hook "e2e: staged go:embed asset consistent" 0

# test-file variant: //go:embed inside a _test.go is consumed by go test
# only — plain go list leaves TestEmbedFiles EMPTY, so the -test flag on
# the embed scan is what keeps this from slipping the guard
reset; printf 'package foo\n\nimport (\n\t_ "embed"\n\t"testing"\n)\n\n//go:embed local_t.db\nvar fixtureDB string\n\nfunc TestEmbedDB(t *testing.T) { if fixtureDB == "" { t.Fatal() } }\n' > internal/foo/embed_test.go
printf 'fixture\n' > internal/foo/local_t.db
git add internal/foo/embed_test.go
check_hook "e2e: ignored _test.go embed asset masked" 1 "local_t.db"

# ignored embed asset in a package BENEATH testdata/: wildcard ./...
# never enumerates testdata/ (explicit imports beneath it DO resolve, see
# the ghost-import scenarios below), so the embed census needs -deps to
# reach it — exactly like the TOOL_DIRS_CACHE graph union
reset; mkdir -p internal/foo/testdata/temb
printf 'package temb\n\nimport _ "embed"\n\n//go:embed local_e.db\n\nvar E string\n' > internal/foo/testdata/temb/e.go
printf 'embeddata\n' > internal/foo/testdata/temb/local_e.db
printf 'package foo\n\nimport (\n\t_ "probe.local/x/internal/foo/testdata/temb"\n)\n\nfunc Foo() int { return 1 }\n' > internal/foo/a.go
gofmt -w internal/foo/a.go; git add internal/foo/a.go
check_hook "e2e: ignored embed asset under testdata masked" 1 "local_e.db"

# asset-only DELETION of an embed target outside the checked pathspecs:
# nothing checked is staged, yet the committed snapshot loses the LAST
# match of a //go:embed pattern — the tier-1 census must engage and the
# fail-closed graph refusal converts the unresolvable pattern into a
# block (pre-tiering this exact commit passed the hook; CI did not)
reset
cat > rootembed_fixture.go <<'EOF'
package x

import _ "embed"

//go:embed banner.txt
var Banner string
EOF
gofmt -w rootembed_fixture.go
printf 'banner\n' > banner.txt
git add rootembed_fixture.go banner.txt
git commit -qm 'fixture: root-level //go:embed banner.txt'
git rm -q banner.txt
check_hook "e2e: asset-only embed-target deletion outside pathspecs refused" 1 "Cannot enumerate"
git reset -q --hard HEAD >/dev/null

# fail-closed guard scans: with a package graph the toolchain cannot
# enumerate (UNTRACKED file importing an unresolvable module — GOPROXY=off
# makes resolution fail fast), the guard must REFUSE rather than silently
# skip its ignored-input/embed scans. The refusal, not the subsequent
# untracked-file report, must fire first. (-f {{.Dir}} resolves imports
# lazily, so only a genuinely broken GRAPH reaches the refusal; a mere
# syntax error in non-import code does not fail go list and stays with
# the untracked-path report covered above.)
reset; printf '\nvalue: 2\n' >> configs/config.yaml.example; git add configs/config.yaml.example
printf 'package foo\n\nimport _ "totally.invalid/nonexistent/pkg"\n\nvar BrokenDep int\n' > internal/foo/zbroken.go
check_hook "e2e: unresolvable package graph hard-blocks guard scans" 1 "Cannot enumerate"

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
mkdir -p .worktrees/wt; printf 'package wt\n' > .worktrees/wt/x.go
check_hook "e2e: ignored .go inside .worktrees/ does not block" 0

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
mkdir -p .tmprepro; printf 'package main\n\nfunc main() {}\n' > .tmprepro/repro.go
check_hook "e2e: ignored .go inside scratch tree does not block" 0

# create-side masking: a brand-new package whose ONLY .go file is
# ignored+untracked — a staged import of it compiles locally but cannot on CI
reset; mkdir -p internal/ghost
printf 'package ghost\n\nfunc G() int { return 1 }\n' > internal/ghost/local_g.go
printf 'package foo\n\nimport (\n\t"fmt"\n\n\t_ "probe.local/x/internal/ghost"\n)\n\nfunc Foo() int { fmt.Println(1); return 1 }\n' > internal/foo/a.go
gofmt -w internal/foo/a.go; git add internal/foo/a.go
check_hook "e2e: ignored-only new package masked (create side)" 1 "local_g.go"

# same, at the MODULE ROOT: go list reports {{.Dir}} == $PWD exactly, so the
# root package must normalise to '.' for the guard to see it
reset; printf 'package x\n\nfunc RootGhost() int { return 1 }\n' > local_root.go
printf 'package foo\n\nimport (\n\t"fmt"\n\n\t_ "probe.local/x"\n)\n\nfunc Foo() int { fmt.Println(1); return 1 }\n' > internal/foo/a.go
gofmt -w internal/foo/a.go; git add internal/foo/a.go
check_hook "e2e: ignored-only module-root package masked" 1 "local_root.go"

# ignored-only package BENEATH testdata/: go list ./... never enumerates it,
# but an explicit import resolves fine — the dep-graph union must catch it
reset; mkdir -p internal/foo/testdata/ghost
printf 'package ghost\n\nfunc G() int { return 1 }\n' > internal/foo/testdata/ghost/local_g.go
printf 'package foo\n\nimport (\n\t"fmt"\n\n\t_ "probe.local/x/internal/foo/testdata/ghost"\n)\n\nfunc Foo() int { fmt.Println(1); return 1 }\n' > internal/foo/a.go
gofmt -w internal/foo/a.go; git add internal/foo/a.go
check_hook "e2e: ignored-only package under testdata masked" 1 "local_g.go"

# test-ONLY import of an ignored-only testdata package: -deps without -test
# never traverses it — the cache must include test dependencies
reset; mkdir -p internal/foo/testdata/tghost
printf 'package tghost\n\nfunc T() int { return 1 }\n' > internal/foo/testdata/tghost/local_t.go
printf 'package foo\n\nimport (\n\t"testing"\n\n\t_ "probe.local/x/internal/foo/testdata/tghost"\n)\n\nfunc TestFoo(t *testing.T) { if Foo() != 1 { t.Fatal() } }\n' > internal/foo/a_test.go
gofmt -w internal/foo/a_test.go; git add internal/foo/a_test.go
check_hook "e2e: test-only ignored testdata import masked" 1 "local_t.go"
git reset -q --hard HEAD >/dev/null 2>&1; rm -f local_root.go

# regex-hostile checkout path: whole scratch copied under a [bracket] dir —
# the prefix strip must not fail open there either
reset
# fresh unique root: a predictable sibling path could belong to a
# concurrent or crashed run — rm -rf must only ever target what THIS run
# created, so uniqueness comes first and the bracket lives in the LEAF
BRKROOT=$(mktemp -d)
BRK="$BRKROOT/br[ack]et"
cp -R "$WORK" "$BRK"
cd "$BRK" || exit 2
gofmt_w() { gofmt -w "$1"; }
mkdir -p internal/ghost
printf 'package ghost\n\nfunc G() int { return 1 }\n' > internal/ghost/local_g.go
printf 'package x\n\nfunc RootGhost() int { return 1 }\n' > local_root.go
printf 'package foo\n\nimport (\n\t"fmt"\n\n\t_ "probe.local/x"\n\t_ "probe.local/x/internal/ghost"\n)\n\nfunc Foo() int { fmt.Println(1); return 1 }\n' > internal/foo/a.go
gofmt_w internal/foo/a.go; git add internal/foo/a.go
if PRE_COMMIT_FAST=1 bash "$HOOK" >hook.out 2>&1; then brk_rc=0; else brk_rc=$?; fi
if [ "$brk_rc" -eq 1 ] && grep -q local_g.go hook.out && grep -q local_root.go hook.out; then
  PASS=$((PASS+1)); echo "ok   - e2e: bracketed checkout path — both ghost masks blocked"
else FAIL=$((FAIL+1)); echo "FAIL - e2e: bracketed path regressed (rc=$brk_rc)"; tail -5 hook.out; fi
cd "$WORK" || exit 2; rm -rf "$BRKROOT"
git reset -q --hard HEAD >/dev/null 2>&1; rm -f local_root.go
git reset -q --hard HEAD >/dev/null 2>&1; rm -rf internal/ghost

# full (non-FAST) run: the scoped test invocation genuinely executes tests
reset; printf '\n// full run\n' >> internal/foo/a.go; git add internal/foo/a.go
PCFAST= check_hook "e2e: non-FAST run executes scoped tests" 0 "Running fast unit tests"

# last tracked .go staged-deleted, ignored sibling remains -> must still block
reset; printf 'package vanish\n\nfunc Ghost() int { return 1 }\n' > internal/vanish/local_ghost.go
git rm -q internal/vanish/v.go
check_hook "e2e: ignored .go masks fully-deleted package" 1 "local_ghost.go"

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
printf 'package bar\n\nfunc Brand() int { return 5 }\n' > internal/bar/new.go
check_hook "e2e: untracked new file under checked path blocked" 1

reset; git rm -q internal/foo/readme.txt; printf 'asset2\n' > internal/foo/readme.txt
check_hook "e2e: staged-deleted asset recreated unstaged blocked" 1

# GOWORK neutralization: a local go.work with a broken replace must not
# affect the hook's verdict (it is git-ignored; CI never sees it).
reset; printf '\n// staged under gowork\n' >> internal/foo/a.go; git add internal/foo/a.go
printf 'go 1.21\n\nuse .\n\nreplace probe.local/x => ./does-not-exist\n' > go.work
# sanity: with workspace mode ACTIVE this scratch repo should fail to resolve
if go list ./... >/dev/null 2>&1; then
  echo 'WARN: go.work sanity check inconclusive (list succeeded)' >&2
fi
check_hook "e2e: broken local go.work neutralized (GOWORK=off)" 0
rm -f go.work

# TOCTOU: mutate the staged file WHILE the hook runs. The mutation is
# synchronized to a deterministic hook checkpoint (the [3/8] go vet banner,
# long past the pre-check guard) and is never restored, so the post-check
# recheck MUST observe it — no fixed-sleep race. Either way rc must be 1.
reset; printf '\n// staged for toctou\n' >> internal/foo/a.go; git add internal/foo/a.go
: > hook.out   # no stale markers from earlier scenarios
( seen=""
  for ((_p = 0; _p < 200; _p++)); do grep -qF '[3/8]' hook.out 2>/dev/null && { seen=1; break; }; sleep 0.05; done
  # Deterministic barrier: mutate ONLY after observing the hook's [3/8]
  # checkpoint; if it never appeared, the test must fail as inconclusive,
  # never silently pass or race the finished hook.
  [ -n "$seen" ] || exit 2
  printf '\n// mid-run mutation\n' >> internal/foo/a.go ) &
watcher=$!
if PRE_COMMIT_FAST=1 bash "$HOOK" >hook.out 2>&1; then toctou_rc=0; else toctou_rc=$?; fi
watcher_rc=0; wait "$watcher" || watcher_rc=$?
if [ "$watcher_rc" -ne 0 ]; then
  FAIL=$((FAIL+1)); echo "FAIL - e2e: TOCTOU checkpoint never observed (inconclusive)"
elif [ "$toctou_rc" -eq 1 ] && grep -q "changed while the checks were running" hook.out; then
  PASS=$((PASS+1)); echo "ok   - e2e: mid-run mutation blocked (TOCTOU recheck)"
else FAIL=$((FAIL+1)); echo "FAIL - e2e: mid-run mutation escaped (rc=$toctou_rc)"; tail -5 hook.out; fi

# Index drift: mid-run edit AND git add — the tree matches the index at the
# end, but the staged content was never classified/tested.
reset; printf '\n// staged for index-drift\n' >> internal/foo/a.go; git add internal/foo/a.go
: > hook.out   # no stale markers from earlier scenarios
( seen=""
  for ((_p = 0; _p < 200; _p++)); do grep -qF '[3/8]' hook.out 2>/dev/null && { seen=1; break; }; sleep 0.05; done
  [ -n "$seen" ] || exit 2
  printf '\n// staged mid-run\n' >> internal/foo/a.go; git add internal/foo/a.go ) &
watcher=$!
if PRE_COMMIT_FAST=1 bash "$HOOK" >hook.out 2>&1; then drift_rc=0; else drift_rc=$?; fi
watcher_rc=0; wait "$watcher" || watcher_rc=$?
if [ "$watcher_rc" -ne 0 ]; then
  FAIL=$((FAIL+1)); echo "FAIL - e2e: index-drift checkpoint never observed (inconclusive)"
elif [ "$drift_rc" -eq 1 ] && grep -q "never validated" hook.out; then
  PASS=$((PASS+1)); echo "ok   - e2e: mid-run git add blocked (index drift)"
else FAIL=$((FAIL+1)); echo "FAIL - e2e: mid-run git add escaped (rc=$drift_rc)"; tail -5 hook.out; fi

# Mid-analysis drift: hammer the index with edits+adds through the WHOLE run
# (covering the pre-check classification window, which produces no output
# markers to sync against). With three comparators (post-analysis, post-run,
# and tree-consistency) against adds every ~20ms, an all-clean sweep is
# practically impossible; refusal may come from any comparator.
reset; printf '\n// staged for spam\n' >> internal/foo/a.go; git add internal/foo/a.go
: > hook.out   # no stale markers from earlier scenarios
: > spam.count
( i=0
  while ! grep -qE 'checks passed|✗' hook.out 2>/dev/null; do
    i=$((i+1)); [ "$i" -gt 400 ] && break
    printf "\\n// spam %s\\n" "$i" >> internal/foo/a.go
    # lock contention with the hook's index reads is expected; retry next iter
    git add internal/foo/a.go 2>/dev/null
    echo "$i" > spam.count
    sleep 0.02
  done ) &
spammer=$!
if PRE_COMMIT_FAST=1 bash "$HOOK" >hook.out 2>&1; then spam_rc=0; else spam_rc=$?; fi
wait "$spammer"
spam_n=$(cat spam.count)
# Deterministic barriers: the hammer must have fired >=3 index changes across
# the run (proving it was not starved) AND the hook must have refused.
if [ "$spam_rc" -eq 1 ] && [ "${spam_n:-0}" -ge 3 ]; then PASS=$((PASS+1)); echo "ok   - e2e: index hammering blocked (analysis-window drift, $spam_n adds)"
else FAIL=$((FAIL+1)); echo "FAIL - e2e: index hammering escaped (rc=$spam_rc, adds=${spam_n:-0})"; tail -5 hook.out; fi

echo "---"
echo "passed=$PASS failed=$FAIL"
[ "$FAIL" -eq 0 ]