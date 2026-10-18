# Full platform accounting (no exclusions)

This inventory is derived by Go's coverage AST instrumenter from the actual
source files. Static instrumentation identifies executable statements; it does
not prove execution. `platform-executables.json` contains every block and its
source span. The 488-statement earlier Linux denominator omitted entire
platform-excluded files. It must not be called whole-patch coverage.

## New Windows file: 29 statements, 6 functions

`internal/history/primary_undo_platform_windows.go`:

- primaryUndoCrossDevice: 1 statement. Existing Windows classification test
  executes ERROR_NOT_SAME_DEVICE, EXDEV, access-denied and EINVAL cases.
- primaryUndoDurabilitySupported: 1. Existing Windows refusal test really calls
  beginPrimaryUndo and resumePrimaryUndo and verifies bytes retained. It also
  covers the two shared refusal returns in primary_undo.go.
- primaryUndoSyncDir: 1. New Windows native-publication/sync-refusal test invokes
  it directly and requires the manual-recovery error.
- primaryUndoPublishNoReplace: 12. Existing flags/error test covers native branch
  flags, collision, permission error and invalid UTF16 input. New tests exercise
  real native no-replace collision/success and the virtual-filesystem fallback.
- primaryUndoObjectKey: 7. Existing full-128-bit test injects upper-bit changes
  and unsupported API failure. Weak-handle refusal is exercised by the existing
  admission test. New native identity/witness test requires actual FILE_ID_INFO
  from a real native handle, rather than accepting only mocked or compiled code.
- primaryUndoObserveChange: 7. Weak-handle refusal is exercised by the existing
  generic weak-witness test. New native identity/witness test verifies real
  change-time observation, same-size/mtime-restamped mutation detection, and
  closed-handle failure.

Native targets:

- TestPrimaryUndoWindowsPublicationRequestsNoReplaceWriteThrough
- TestPrimaryUndoWindowsCrossDeviceClassification
- TestPrimaryUndoWindowsFailsClosedBeforeAdmission
- TestPrimaryUndoWindowsSameVolumeRenameBypassesAdmissionGate
- TestPrimaryUndoWindowsIdentityUsesAll128BitsAndFailsClosed
- TestPrimaryUndoWindowsNativeIdentityAndMutationWitness
- TestPrimaryUndoWindowsNativePublicationCollisionAndSyncRefusal
- TestPrimaryUndoWindowsVirtualPublicationFallback
- TestPrimaryUndoIdentityAdmissionRefusals (its weak identity assertions run
  before the fixture skips unsupported Windows protocol execution)
- TestPrimaryUndoWeakMutationWitnessRefused

Status: not executed natively in this Linux environment. No Windows coverage
claim is made. Direct primitive tests do not enable Windows EXDEV Undo; its
complete protocol still refuses admission and resume.

## BSD witness file: 7 statements, 1 function

`primary_undo_change_bsd.go`, build constraint darwin || freebsd || netbsd:
primaryUndoObserveChange uses Stat_t.Ctimespec. Its success, Stat failure and
missing-native-Stat_t refusal require execution of this actual build-selected
file on a supported BSD-family system. The generic late-stat/weak-witness and
restamped-source/destination tests target these behaviors, but Linux/Windows
runs do not execute this file. A native macOS/BSD profile is still required for
these seven statements. No new ignore or threshold exemption is proposed.

## Unsupported-platform witness: 1 statement, 1 function

`primary_undo_change_other.go`, selected outside the listed Windows/Linux/BSD/
Solaris/DragonFly targets: primaryUndoObserveChange returns an unsupported-platform
error. Linux and Windows cannot execute this build-selected definition. A native
supported build target selecting this file, with a direct refusal assertion,
is still needed for its single statement. No such native environment is
available in this task. Do not silently drop it from the denominator.

## Linux/POSIX files: 23 statements, 6 definitions

`primary_undo_change_unix.go` (linux/openbsd/solaris/dragonfly): 7 statements in
primaryUndoObserveChange. `primary_undo_platform_posix.go`: 16 statements across
primaryUndoCrossDevice (1), primaryUndoDurabilitySupported (1), primaryUndoSyncDir
(11), primaryUndoObjectKey (2), primaryUndoPublishNoReplace (1).

All 23 were exercised on Linux in the reviewed tests delta. The deleted-cwd
branch's child-process counters are recorded in a separate real coverage
profile and unioned with the parent profile. That subprocess creates/removes
only its own new empty temporary directory. Coverage from Linux does not claim
native semantics have been validated separately on every other tagged OS.

## Accounting boundary

Before the composition guard additions, the ordinary-Undo source had 488
Linux-instrumented changed statements, plus 29 Windows, 7 BSD and 1 unsupported
platform statements: 525 total, not 488. Two shared refusal statements inside
that 488 are Windows-executable only. The final composition has additional
shared fail-closed guards; the final exact diff must be measured again rather
than reusing 525 as its final total. PR291's already-reviewed imported changes
must also be included if the eventual PR base makes them part of that PR's diff.

Adding a Windows Codecov upload can supply genuine Windows execution counters,
but cannot by itself close the BSD/unsupported-target gaps. No profile, helper
function, platform file, or threshold may be excluded to manufacture 100%.
