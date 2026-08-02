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
printf '/web/dist/ignored/\n*.syso\n' > .gitignore
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
check_hook() { # name expected_exit [grep-pattern]
  local name=$1 want=$2 pat=${3:-} got
  if PRE_COMMIT_FAST=1 bash "$HOOK" >hook.out 2>&1; then got=0; else got=$?; fi
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

reset; mkdir -p .planning; printf 'x\n' > ".planning/秘.txt"; git add ".planning/秘.txt"
check_hook "e2e: unicode forbidden path blocked" 1 "Blocked"

reset; printf 'go 1.21\n\nuse .\n' > go.work; git add -f go.work
check_hook "e2e: force-staged go.work refused" 1 "go.work"
rm -f go.work

reset; printf '\n// staged A\n' >> internal/foo/a.go; git add internal/foo/a.go
printf '\n// unstaged B\n' >> internal/bar/c.go
check_hook "e2e: cross-package unstaged change blocked" 1 "Offending paths"

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
printf '# touched\n' >> Makefile
check_hook "e2e: unstaged Makefile change blocked" 1 "Offending paths"

reset; printf '\n' >> go.mod; git add go.mod
check_hook "e2e: module change runs full go vet (even under FAST)" 0 "all packages — module files"

reset; printf '\n' >> go.mod; printf '\n// mixed\n' >> internal/foo/a.go
git add go.mod internal/foo/a.go
check_hook "e2e: mixed module+Go still runs full go vet" 0 "all packages — module files"

reset; printf '\n// staged\n' >> internal/foo/a.go; git add internal/foo/a.go
printf 'syso\n' > internal/foo/x.syso
check_hook "e2e: ignored *.syso build input blocked" 1 "x.syso"

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
( seen=""
  for _ in $(seq 1 200); do grep -qF '[3/8]' hook.out 2>/dev/null && { seen=1; break; }; sleep 0.05; done
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
elif [ "$toctou_rc" -eq 1 ]; then PASS=$((PASS+1)); echo "ok   - e2e: mid-run mutation blocked (TOCTOU recheck)"
else FAIL=$((FAIL+1)); echo "FAIL - e2e: mid-run mutation escaped (rc=$toctou_rc)"; tail -5 hook.out; fi

# Index drift: mid-run edit AND git add — the tree matches the index at the
# end, but the staged content was never classified/tested.
reset; printf '\n// staged for index-drift\n' >> internal/foo/a.go; git add internal/foo/a.go
( for _ in $(seq 1 200); do grep -qF '[3/8]' hook.out 2>/dev/null && break; sleep 0.05; done
  printf '\n// staged mid-run\n' >> internal/foo/a.go; git add internal/foo/a.go ) &
watcher=$!
if PRE_COMMIT_FAST=1 bash "$HOOK" >hook.out 2>&1; then drift_rc=0; else drift_rc=$?; fi
wait "$watcher"
if [ "$drift_rc" -eq 1 ] && grep -q "never validated" hook.out; then
  PASS=$((PASS+1)); echo "ok   - e2e: mid-run git add blocked (index drift)"
else FAIL=$((FAIL+1)); echo "FAIL - e2e: mid-run git add escaped (rc=$drift_rc)"; tail -5 hook.out; fi

# Mid-analysis drift: hammer the index with edits+adds through the WHOLE run
# (covering the pre-check classification window, which produces no output
# markers to sync against). With three comparators (post-analysis, post-run,
# and tree-consistency) against adds every ~20ms, an all-clean sweep is
# practically impossible; refusal may come from any comparator.
reset; printf '\n// staged for spam\n' >> internal/foo/a.go; git add internal/foo/a.go
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