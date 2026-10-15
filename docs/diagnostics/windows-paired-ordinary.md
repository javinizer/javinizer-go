# Bounded Windows ordinary-product comparison

This diagnostic compares a fixed set of existing product tests on hosted Windows runners. Publishing a pull request that triggers it or dispatching it requires maintainer approval after review. It does not publish releases, packages, or containers.

## Fixed source records

- `original`: `2f97d9e75dd8402d17544c5501222400bdf233df`, tree `e5e495d179e9bcf804ad23b285c572d17b346a09`. This is the exact tree-equivalent PR commit for the originally failing merge checkout `dcab5c680a55ef6189ece23b672826bd62fbcc41`; the latter object was not available locally. This is not `4611e15e`.
- `cleanup`: `2895e16e1ab53a40cbc6c95ddd2242ff0be39554`, tree `1b218219361eb636a2cc716a91b69edcfd08ef64`, after cleanup changes and before explicit nil guards.
- `fixed`: `16043816a27007f304916739f1734dd0ba4d0ba0`, tree `dcf484715d742b387aa66add8929853057858d68`.

Tracked source cleanliness is rechecked after dependency downloads and compilation, before any product execution. Each source checkout is clean and validated against both its full commit and tree. The existing offline overlay generator blob is `10f013f25cb4aaa083de051e5315dcd927fded46` and guard tree is `b967d5d8603a8cae61282e17902d3dec76b26adc` for all three. No arbitrary source ref, toolchain, test selector, command, or repository URL is a workflow input.

## Fixed one-time sample and budget

Three independent `windows-2025` jobs, fail-fast disabled, each comparing all three revisions on the SAME host and official Go 1.27.2 installation with default GC, CGO=1, gcc, Windows/amd64 v1. This is a three-host sample, not retries until green, and a runner label does not identify an immutable image or CPU.

Each host predeclares 18 product processes, each invoked at most once:

1. Full server: original, cleanup, fixed.
2. Full server: cleanup, fixed, original.
3. Full server: fixed, original, cleanup.
4. Observed core-prefix-59 slice once per revision, original/cleanup/fixed.
5. Shared existing four-test core nil-contract subset once per revision.
6. Isolated existing InitializedAuth once per revision.

Thus six ordinary attempts per revision, 18 per host, 54 total. No repeated `-test.count` loop, custom fault test, synthetic corruption, exception metadata, debugger, WinAPI/PInvoke, GC tuning, runtime patch, memory dump, security change, external service, or OS network modification is introduced.

Each job has a hard 40-minute ceiling (maximum 120 runner-minutes across all three). The controller allows ten minutes for its setup phase and 28 minutes after the offline boundary, leaving nominal two minutes for evidence; action checkout/toolchain time also consumes the job ceiling. Compile caps are 240s each, existing guards 150s outer/120s Go timeout, full server and prefix 210s outer/180s Go timeout, isolated controls 90s outer/60s Go timeout. Worst-case allocations exceed the overall budget by design: completion is NOT guaranteed. Exhaustion or any inner/outer timeout stops future test execution and makes the sample incomplete/non-successful. It does not authorize extra hosts, longer jobs, or reruns. Artifact preservation can itself be interrupted by the hard job limit; missing artifacts mean evidence is incomplete.

## Core scope and isolation caveat

Full core is intentionally NOT scheduled: the Go networking overlay cannot intercept OS filesystem networking, and unrelated later tests contain UNC-path filesystem branches. This is a structural isolation limitation, not evidence that an external request occurred; an inspected forward-slash UNC candidate currently skips at an earlier precondition. The fixed prefix manifest records the ordered 59 roots actually observed in failed main2895 before/including `TestAPIDeps_NilCoreDeps_Panics`.

Prefix source: run38717163556, artifact11963957379, original archive SHA256 `02213fe6bd975144abd642bfde8f852044d1bf4b72dac26ad21302c395bb4f18`. The exact root order is asserted against each compiled binary's listing before product execution and against run events afterward. This is a prefix SUBSET, never full-core coverage. Independent selected-path source audit found the three UNC-literal functions immediately skip on Windows; remaining active paths are local temp files, in-memory filesystems/databases or mocked HTTP. The seven exact existing Windows skips and all other 52 passing roots are required. This supports the bounded slice, not an OS-wide isolation claim.

The shared four existing core controls are `TestAPIDeps_NilCoreDeps_Panics`, `TestAPIDeps_SetConfig_NilCoreDeps`, `TestAPIRuntime_ReloadConfig_NilCoreDeps`, and `TestAPIRuntime_ReloadConfig_NilCoreDepsGuard`. The original single recovered-nil test is ordinary existing product coverage, not a new fault probe.

## Offline safety gates and retention

Official actions/toolchain and module downloads are setup-only. Offline dependency metadata is limited to the two selected Windows test packages using `go list -mod=readonly -deps -test -json ./internal/api/server ./internal/api/core`; it does not enumerate the unrelated whole module graph. A metadata failure stops the comparison with all unstarted attempts preserved. Successful dependency metadata is not a compile or runtime result. Runtime fixes GOPROXY=off, GONOPROXY=none, GOSUMDB=off, GOVCS=*:off, GOTOOLCHAIN=local; removes both existing live-test opt-ins; disables persistent Go user config; removes GC/GOMAXPROCS/experiment overrides. Existing GODEBUG=netdns=go is retained for the existing strict guard. Each revision's unchanged guard must show seven exact passing roots and precisely 26 OFFLINE279_BLOCKED plus four OFFLINE279_BLOCKED_HTTP_TARGET markers. All three guards must pass before any product binary runs. Runtime/build/inventory/conversion audits must otherwise be empty.

Both product binaries are compiled once per revision under its own identical guard source. Inventory invocations execute package initialization and therefore also use that guarded environment, with a separate mandatory-empty audit. SHA256 is checked before every attempt and after all attempts. Direct execution preserves the actual native test executable return code, rather than only test2json's wrapper code. Raw stdout/stderr are written first; a separate bounded official test2json conversion follows. Malformed JSON, test failure, unexpected skip, unexpected test, missing/duplicated run or terminal, audit markers, changed identity, launch failure, or incomplete slot cannot become green because a later control passes. Existing permitted platform/short-mode skips are enumerated with source evidence; critical nil/auth controls cannot skip.

Timeout cleanup uses only normal `taskkill /PID <owned child> /T /F`, bounded wait, and prohibits subsequent test processes even if cleanup appears successful. No probing or machine reconfiguration is used.

Every planned attempt is recorded before execution. Per-command outputs, exit/timeout/launch data, timing, audit, source/toolchain identity, overlay hashes, full root inventories, observed test events, binary hashes and ordinary binaries are retained in GitHub Actions artifacts for 7 days; text command evidence is also emitted in CI logs. These artifacts are diagnostic build outputs, not GitHub Releases/packages/containers. No credentials, whole-environment dump, proxy URLs, user identities, serial numbers, full-memory dump, or debugger capture is requested. Read-only metadata is limited to image/build, OS, CPU name/core counts, total RAM, selected Go configuration and compiler versions. Missing metadata is recorded explicitly and makes the overall diagnostic incomplete.

## First-run feasibility and interpretation

GitHub documents that workflow_dispatch requires the workflow to be on the default branch, while pull_request uses the PR merge context (https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows and https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow). The workflow therefore permits path-filtered `pull_request: opened` and `synchronize` events. Opening the diagnostic PR or publishing a reviewed controller correction starts one fixed sample; there is no automatic failed-job retry or sampling-until-green policy. Every update that would start another sample requires maintainer approval. This is an operational approval requirement, not a workflow-enforced gate: any matching synchronize event starts the three-host sample. All five controller/test/manifest/workflow paths are explicit; checkout credentials are not persisted; permissions are contents:read; hosted ephemeral runners receive no configured secrets. Opening or updating the PR is itself execution authorization and requires maintainer approval after review. Manual dispatch has no inputs and requires the workflow to be available through GitHub's normal workflow registration. No release workflows are invoked.

Recovered Gin nil/interface panic log occurrences are recorded separately; historical baseline fixture recoveries alone are not a native process failure. Counts are log-text occurrences, not measurements of hardware exceptions or assertions that internally recover panics. Native exits and test terminals remain authoritative.

Full success means only bounded non-reproduction in this guarded comparison. The overlay alters standard-library compilation and therefore binary layout/timing compared with the original ordinary run. The original, cleanup, and fixed source trees include broader code/fixture changes: a difference is useful comparative evidence, not pure upstream/runtime causality. Passing isolated auth after a failing full suite can suggest history/pressure effects but does not identify a corruptor. No observation here measures XSTATE, exception-context size, or registers absent from original logs. Release/reliability decisions remain separate.

## Initial prerequisite failure and bounded recovery

The first hosted run, https://github.com/javinizer/javinizer-go/actions/runs/38734756609, stopped on all three hosts at the offline whole-module-graph metadata query. Dependency downloads returned success, but `go list -m -json all` attempted to resolve additional module metadata unavailable in the cache. All 54 planned product attempts were not run; this was a prerequisite failure, not evidence of a product crash or pass. The original logs and outcomes remain part of the investigation.

The correction requests metadata only for the actual Windows-selected server/core test dependencies, with module edits prohibited and the same offline environment/audit. A reviewed update may initiate one new fixed sample after maintainer approval. This does not relax networking, alter pinned product sources, fetch optional modules solely for metadata, increase hosts/budgets, or erase the original failed setup outcome. Controller-only standard-library mocks run before the comparison in CI.

## Local checks

Run `python -m unittest discover -s scripts/diagnostics -p test_windows_paired_ordinary.py -v` for controller-only mocks. These never invoke Go, native Windows tests, custom faults, or remote actions. Native Windows process management and actual CI behavior remain unverified until the separately authorized hosted run.
