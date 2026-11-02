# Owned Windows console prototype — SOURCE ONLY

This is a **future diagnostic**, not an admitted coverage test or CI gate. No
native execution, build, Go command, or lifetime bug has been demonstrated by
this patch. Execution is held for independent source review and a bounded native
plan; it does not require a new user permission merely because it is native.

## Source/build provenance

Reviewed base: `a3a376b279fcb7f1b2292835038a1adf4d75851e`.
No tracked AGENTS.md or repository-local SKILL.md exists in that tree. Keep
published PR568 and held admission component de0a9db unchanged.

Use an owned build checkout with the project's normal real web build and icon
resource generation. The authoritative Windows desktop command is
`.github/workflows/cli-release.yml`, `Build desktop binary` (line 681 at base):
CGO_ENABLED=1, GOOS=windows, GOARCH=amd64, `-trimpath`, tags
`desktop,production`, `-w -s -H windowsgui`, the three internal/version metadata
`-X` flags, and `-X github.com/javinizer/javinizer-go/internal/desktop.BuildDesktop=1`.
Use the actual version/commit/date values and pinned toolchain, not placeholder
assets or production substitutions. One console-subsystem **control** uses the
same source, desktop tags and BuildDesktop=1, omitting only `-H windowsgui`.
That control is not the shipped GUI artifact. Ordinary BuildDesktop=0 CLI builds
cannot cover attachParentConsole.

For later native counters, additionally build both actual CLI executables with
`-cover` and an explicitly reviewed coverpkg selection containing
`github.com/javinizer/javinizer-go/cmd/javinizer`. This is an instrumented build,
not proof of byte identity with release binaries. Archive full commands,
source/tree identity, asset/resource hashes, `go version -m` output and final
binary hashes. The prototype checks PE32+ plus COFF Machine=0x8664, subsystem, and input/copied binary hashes, **not**
the truth of a self-reported build receipt; independent build provenance review
is a prerequisite. Do not silently use an arbitrary executable with matching
self-authored metadata.

The manifest schema is:

```json
{
  "source_commit": "<actual externally bound tested checkout SHA>",
  "build_desktop": "1",
  "tags": "desktop,production",
  "console": {"path": "<owned-console-build.exe>", "sha256": "<actual-hash>"},
  "gui": {"path": "<owned-gui-build.exe>", "sha256": "<actual-hash>"},
  "expected_version_line": "javinizer <version> (commit: <commit>, built: <date>, go: <actual-pinned-Go>)\n"
}
```

Derive the exact version line independently from `internal/version.Info` and
actual build metadata; do not learn it from the GUI child's output. The error
case is `version --owned-console-probe-invalid`: Cobra SilenceErrors suppresses
its error prefix, but SilenceUsage is unset at parse failure. The complete
version-subcommand usage (including inherited config/verbose flags) is therefore
pinned **before** main's one-line crash log. Its literal comes from
cmd/javinizer/commands/version/command.go, root.go flags, pinned Cobra v1.8.0
command.go UsageTemplate/InitDefaultHelpFlag/execute/ExecuteC and pflag
FlagUsagesWrapped alignment. UsageString ends in LF and Println adds another LF.
No newline, whitespace, usage, or error output is normalized away. Actual native
output remains to be verified; preserve and diagnose any first mismatch.

## Ownership and call graph

`main -> run -> IsDesktopBuild && args -> attachParentConsole -> Execute`.
`--version` exits through Cobra before config initialization; the version
subcommand intentionally unknown flag fails parsing and reaches writeCrashLog. Neither launches Wails,
providers, updates, or a server. Error crash logs use owned APPDATA. Every child
has owned cwd/home/config locations; no actual user configuration is loaded.

The supervisor creates a real Python host with CREATE_NEW_CONSOLE and pipe
stdio. The host blocks on stdin before launching anything. Only after assigning
it to a newly created kill-on-close job does the supervisor send its request.
The host asserts GetConsoleProcessList contains solely its own PID before CLI
children. No harness code calls AttachConsole, FreeConsole, SetStdHandle,
GenerateConsoleCtrlEvent, or production globals. It uses no fabricated handles.

Each real CLI binary is copied into the retained fixture directory. Console
children inherit this owned console; GUI-subsystem children start without one
and may attach only to their immediate fixture host. Windows may refuse the
required console/job capability: stop and report, do not detach the runner or
bypass its job policy. No breakaway flags are used. All descendants inherit the
job; the handshake prevents a pre-assignment child race. Each subprocess has a
20-second timeout, and the supervisor has a 200-second host deadline. Only owned
processes are terminated, each is joined and pipe EOF drained; no console signal
broadcasts. If assignment fails, the still-gated host is killed/joined.

The harness uses real inherited pipe handles via public subprocess APIs, closes
its own job/process handles, and never changes the supervisor/user console. Partial timeout bytes and PID are persisted before kill/drain; bounded cleanup
failures remain alongside the original failure. TerminateJobObject is attempted
before CloseHandle, and host kill/join is attempted regardless of either result.
No Popen context-manager implicit unbounded wait is used. Failed cleanup means
the run is not admitted; source intent is not native proof of full-tree cleanup.
A receipt disk failure cannot be made durable: it is reported to stderr without
masking the primary failure and prohibits success. All
native data remains in a fresh directory under the supplied owned output root.
It does not remove input executables or rename any running executable.

## Output, lifetime hypothesis, and coverage admission

Eight bounded child launches: console/GUI × default GOGC/GOGC=1 × version/error.
Only CLI child environments change GC. Exact stdout **and** stderr bytes and
exit codes must agree with independent expected values; first observations are
persisted before assertion failure. Exit zero alone cannot hide lost output.
GOGC=1 is a supported stress control, **not** evidence that GC occurred at the
critical instant; no runtime.GC injection or production wrapper retention.

Source suggests a possible handle-ownership risk: os.NewFile owns, rather than
duplicates, the returned standard handle, and the old wrapper has a finalizer.
If AttachConsole preserves redirected handles, old/new wrappers may share it.
Logging globals may retain some original wrappers. This is a hypothesis only;
there is no native reproduction or correctness claim. Do not add KeepAlive,
extra production globals, or artificial old-wrapper retention to hide it.

Each child writes to its own GOCOVERDIR. The unexecuted `run_owned_windows_console_experiment.py` collector
converts **each child's actual covdata** using pinned `go tool covdata
textfmt`, archive raw data/output, and require positive counts for:

- console control: console_windows.go 25/26/27; success block must remain zero;
- GUI success: 25/26/29/30/32/33; refusal27 must remain zero.

These expectations must hold separately for both GC modes and both commands.
Reject missing metadata/counters, wrong package, zero required counts, malformed
profiles, or source-line mismatch. Parent/helper coverage cannot substitute.
The child harness intentionally prints “native coverage NOT YET VERIFIED”
even after output checks. Only the separate experiment driver can admit the
converted counters, after all eight exact output records and branch profiles
pass. That implementation remains source/Python-reviewed only, not natively
verified. A GUI output PASS
without those counters does not prove attachment success. Native branch failure
is a measured limitation, never permission to weaken a gate.

## Independent review required before any execution

1. Full public CLI early-exit paths and exact output/error oracle, official build
   flags/assets/CGO/subsystem provenance, no GUI or network/config side effects.
2. Native structure ABI/ctypes prototypes, job assignment rights/inheritance,
   child-before-handshake impossibility, nested CI job compatibility and failure
   cleanup; real pipe inheritance and no user/runner console changes.
3. Bounded timeout/exception paths, all process/pipe/handle ownership, first-failure
   preservation, and no production handle-lifetime masking.
4. Separate native covdata provenance and the experiment driver admission
   implementation before claiming any newly covered line. Later native execution may expose source-prototype
   errors; current Python AST parsing is not Windows runtime validation.

Cleanup18 remains unproven. Cleanup23 requires a different owned-copy/restore
handshake proposal; this prototype does not attempt either path. No CI wiring,
remote branch, publication or change to existing coverage uploads is included.

## Local checks (not native)

`PYTHONDONTWRITEBYTECODE=1 python scripts/diagnostics/test_owned_windows_console.py -v`
checks controller control flow with mock process/operation objects that are
never passed to Windows or production code. Cases cover partial timeout receipt
before cleanup, kill/drain failures, independent terminate/close attempts,
assignment refusal before handshake, receipt-write failure preserving the
primary exception, exact output receipt, and rejection of ARM64 PE32+ headers.
No actual OS handle is fabricated or injected into production. The native
structure size/offset gate is mocked only for this pure controller test; its
actual Windows ABI remains a separate prerequisite. No child is started.

## Proposed future integration (not workflow edits)

The current static-e2e job already runs `make test-static-e2e`, including the
normal production frontend build. `scripts/with_embedded_web.sh` restores only
web/dist; the real web/frontend/build remains. Reuse that exact output: after
existing static-e2e success, run the driver's `assets` action and upload the
bundle plus assets.json and provenance command receipts in a same-workflow-run
artifact. This is an artifact transfer, **not another npm installation/build**.
Record tested checkout SHA/tree and raw parents; independently supplied GitHub
contexts must bind PR source head/base to merge parents, or push head equality.
Use the existing checkout policy; never relabel a merge SHA as PR source head.

The existing Windows job can depend on static-e2e and download only this exact
run's asset artifact using pinned existing artifact actions. Preserve all old
Windows steps, profiles and gates. Invoke `experiment` with trusted GitHub
checkout/event/head/base values and a new RUNNER_TEMP evidence directory outside
the source checkout. Native collection and artifact consumption remain `!ACT`;
the pure Python tests are also runnable locally/under ACT. This dependency may
increase Windows start latency; any job timeout change needs explicit review.
No new Codecov upload action/session is needed. Only after successful admission,
append `coverage-owned-console-windows.out` to the **same existing primary-Undo
Windows upload**, keeping autocomplete, umask, existing-CLI and every prior file.
All six upload sessions remain six. Do not publish an aggregate when one branch
or one of the eight children fails.

The driver builds a fresh git archive of the tested checkout, stages only the
verified real assets into that owned copy, generates the official amd64 icon,
and compiles the console control/GUI pair using official desktop flags plus
explicit coverage instrumentation. It records source/module/lockfile/license,
asset/icon hashes, toolchain actual patch version, exact commands, binary hashes
and `go version -m`. go.mod currently pins the minor toolchain; the actual patch
is recorded and constrained to that minor. The test metadata version is a fixed
valid prerelease, commit is the tested checkout and date its git committer date;
this is a diagnostic build, not a release version/artifact. No dependency or
license substitution is performed: root LICENSE, go.mod/go.sum and frontend
package-lock provenance are retained, not a claim that root LICENSE relicenses
third-party components. Public artifacts must follow existing project policies.

Each native tool/build command uses a separate gated helper in a kill-on-close
owned job, so compiler/cgo children also inherit ownership. No tool child starts
before job assignment. Native command deadlines default to 900 seconds; the
matrix deadline is 300 seconds. A nested-job restriction is a reported blocker,
not grounds to break away from a runner job. Pre-command free-space checks require
96 MiB on both command and system-temp filesystems; source review must still
choose an appropriate hosted job budget. Never run this cold build concurrently
with unapproved local Go workloads.

The collector checks all eight exact pipe/exit records again independently,
converts each child's raw covmeta/covcounters with the same pinned Go toolchain,
requires its appropriate branch and zero opposite branch, then merges actual
covdata and emits one final profile. Raw child data, profiles and hashes are
retained. The coverage map is pinned to the actual production console source
hash; source changes require a new line-map review. No profile counts are
fabricated or patched. Synthetic Python evidence tests only verifier rejection,
not native execution.

Always archive first command receipts, stdout/stderr, lifecycle/observations,
per-child raw covdata, exact converted profiles and provenance even on failure.
Archive outputs from the first attempt, not a later retry that happens to raise
coverage. Large build archives/binaries may be retained separately with explicit
hashes and normal retention; no source or binary evidence is silently replaced.
This proposal has made **no CI edits, Go builds, native runs or remote changes**.

### Required trusted producer wiring and source-override controls

Before native integration, the CI diff must give the existing, unchanged
`make test-static-e2e` step an ID `run-static-e2e`. Its actual GitHub step outcome
must feed `assets --producer-outcome`; it is not a user-entered success claim.
The asset exporter checks HEAD-equal tracked frontend/Makefile/embedding-script
inputs and rejects nonignored untracked frontend source (ignored generated build
and node_modules remain permitted). The producer uses only
`web/frontend/build`, writes an actual assets.tar, inventories it, and publishes
its SHA256 as a **trusted producer job output** alongside exact run ID/attempt.

The Windows download must select this exact producer's artifact ID in the same
workflow run, not a name from a different run. Pass the producer job-output
bundle digest externally as `--expected-bundle-sha256`, with actual GitHub run
ID/attempt and producer success result. The driver requires those values to
match the proof and hashes the actual downloaded tar bytes before extraction;
the file inventory and checkout/tree binding are checked again afterward.
The artifact transport's own GitHub ID/ZIP digest should be retained from pinned
upload/download receipts too; the implemented hard content gate is the verified
asset-tar digest, not an invented claim that extracted files authenticate an
unavailable ZIP digest. CI wiring must independently verify this same-run chain
before treating producer metadata as trusted. No such workflow has been changed
or executed by this source prototype.

Git commands remove inherited GIT_* overrides, disable replacement objects and
system/global configuration, and use the explicit repository cwd. Build tools
use GOENV=off (an empty GOFLAGS alone is **not** sufficient), GOWORK=off,
GOTOOLCHAIN=local; inherited GO source/experiment overrides and CC/CXX/CGO flags
are removed. Cache/network module settings may remain, with go.sum source
integrity and actual tool executable hash retained. Effective `go env` settings
are recorded and gated. In the pinned Go source, `go env GOENV` prints an empty
filename when GOENV=off; requested-off and effective-empty are deliberately
recorded separately. Default compiler resolution is recorded, not silently
replaced with a test compiler. These source gates supplement, not replace, the
trusted runner/setup-go/action and reviewed dependency provenance.

### Concrete integration draft and limits

The source-only workflow draft now binds the existing static suite step outcome,
producer job result, exact artifact-ID download, run/attempt and checkout/tree,
and the trusted producer asset-tar SHA256. No workflow permissions or credentials
were added. Upload's outer artifact digest is retained as transport provenance;
the downloaded asset-tar bytes are independently hash-verified. Actual hosted
producer/download behavior still requires first-run evidence.

The Windows job waits for static-e2e but uses `!cancelled()` rather than default
needs-success, so producer failure does not suppress its prior tests. New
controller/experiment failures are locally continue-on-error, their first
failure diagnostics are archived, and an explicit final hard gate fails the job
after existing Windows steps. Only an admitted profile is appended to the same
old primary-Undo upload; missing/failed native coverage is never success. All
old upload sessions remain six. Existing native profiles from a later merged
base must be retained when this independent a3a-based draft is rebased; this
patch does not modify held PR568/de0 branches or silently claim they are present
in its older base.

The existing static job remains at 30 minutes. The Windows job increases from
40 to 70 minutes to preserve its original budget plus the new experiment.
The experiment has one 25-minute aggregate monotonic deadline; each command is
also bounded, and the step has a 28-minute cap for termination/drain overhead.
Cleanup is bounded separately (20-second drain attempts and 3-second watcher
join), and failed cleanup blocks admission. The critical scheduling path may
therefore add the existing static producer's time before Windows starts, up to
roughly 100 configured job-minutes (30 + 70), not a claim of expected wall time.
Typical build duration is unknown until native evidence. Exactly two covered
binaries and eight CLI children are attempted, no retry loop or metric chasing.

During every native child/tool tree, a continuous 0.5-second watcher measures
both command and system-temp roots against 96 MiB, records sample/minimum values,
and terminates only the owned job on shortage or watcher failure. The watcher is
stopped/joined before closing its job handle; if it cannot join, the handle stays
owned until process exit (avoiding reuse races), the run fails and no more tools
are scheduled. Native capacity/termination behavior is still unproven. Asset
copy/archive stages also have pre-command checks and remain in the owned tree.

Native collection/download/archive steps stay !ACT; controller tests still run
and have a final failure gate under ACT. The documented Makefile ACT targets do
not select this Windows job. Direct ACT invocation of Windows may now schedule
the existing static dependency; no pretend native proof is accepted locally.


### Final ownership/failure refinements

The workflow now calls `supervise`, which places the **entire** experiment
(including Python archive extraction and asset copying) in an outer owned job
with the continuous capacity/deadline guard. Per-tool jobs remain nested for
bounded individual cleanup. Direct `experiment` invocation without the guarded
supervisor context is rejected. The outer guard watches the owned output volume
and system-temp volume; inner tool guards additionally watch their working root.

Capacity failure signals the main thread as well as requesting owned-job
termination. Main drains the same process with bounded polling, never resending
its handshake or restarting a command. Poll timeouts are not recorded as failed
native attempts; the absolute deadline or capacity abort is terminal. On abort,
the driver closes/terminates the owned job before the final pipe drain. Thread
start/join/state failures are retained but cannot bypass job/host cleanup; a
possibly live watcher never receives a reused/closed job handle.

The new evidence upload is locally continue-on-error, then required successful
by the final hard gate, so an artifact service failure cannot suppress prior
Windows tests. Profile upload requires both successful experiment outcome and
its explicit admitted-profile output. The whole-experiment supervision receipts
are included in the first-failure archive. Native Python must expose safe tar
data filtering; capability absence fails before archive extraction/build, not
permission to use an unsafe extraction fallback. Its hosted availability remains
to be established alongside actual job ABI and compiler capabilities.
