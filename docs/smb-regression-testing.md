# Synthetic Windows SMB regression

The dedicated `Synthetic Windows SMB Regression` workflow exercises native UNC
I/O against a temporary share on a fresh `windows-2025` GitHub-hosted VM. It uses
only small synthetic byte fixtures, not real media, credentials, or a NAS.

## Review before execution

Opening or updating a pull request touching the workflow's configured paths runs
the job. Independently review the **exact workflow, script, tests, and backend
snapshot before publishing it**. Corrected setup scripts need the same review
before another push. The workflow uses a read-only repository token, no secrets,
no service containers, and the repository's existing pinned checkout/setup-go
actions. It never merges a PR. `workflow_dispatch` is also available once GitHub
has registered the workflow on the default branch.

Do not run the setup script on a workstation or self-hosted runner. It requires
an elevated GitHub-hosted Windows 2025 runner and rejects other environments.
Compile the native test binary before setup; compilation does not create a
share. The workflow performs a native PowerShell parse before compilation.

## Isolation and cleanup

- Require the current elevated runner account, running SMB/firewall services,
  enabled effective firewall profiles, SMB1 already disabled, and insecure guest
  logons already disabled. Fail rather than repair an unexpected environment.
- Use a GUID-named tree inside local `RUNNER_TEMP`, private NTFS ACLs, and an
  ownership marker. Share only its `share` child. The separate local input child
  is never shared. The only share ACL principal is the current runner account.
- Before sharing, add four uniquely named **Block** rules in every firewall
  profile: inbound local/outbound remote TCP 139/445 and UDP 137/138. Address
  ranges cover IPv4 and IPv6 except 127.0.0.0/8 and ::1. Verify the effective
  ActiveStore rules, exact ports/addresses, and full enforcement. Reject enabled
  authenticated bypass rules. A loopback UNC path alone does not restrict a
  server listener. Read-only checks also reject any SMB-over-QUIC certificate
  mapping or RDMA-capable SMB server interface, since QUIC and SMB Direct can
  bypass these TCP/NetBIOS rules. Missing inspection support fails closed. These
  checks run during preflight and both isolation verifications; neither feature
  is disabled or reconfigured.
- Create a temporary, encrypted, non-caching share. Use only
  `\\127.0.0.1\javinizer-smb-<GUID>\fixtures`. Keep its marker open over UNC while
  proving a real `Get-SmbConnection` session for this share and account, an SMB3
  dialect, encryption, and an open handle. Repeat isolation, ACL, and session
  checks after the tests. There is no local-path fallback.
- Do not add accounts, passwords, mappings, firewall allow rules, SMB1, guest
  access, signing/encryption exceptions, loopback-authentication registry changes,
  or external endpoints. Do not change service state or global SMB configuration.
- Cleanup runs in `finally`, with a second `always()` cleanup step. Remove the
  exact share **first**, then the marker-verified synthetic tree and exact four
  rules. Never traverse junctions or symlinks. Never use wildcard share/rule
  removal. If share deletion cannot be proved, retain its blocks rather than
  expose it. Cleanup failure fails the job. The fresh VM's destruction is the
  final fallback if GitHub forcibly terminates execution before cleanup can run.

`New-SmbShare -Temporary` means “until reboot,” not “until script exit.” Explicit
cleanup is required. `New-SmbShare -WhatIf` is documented to be ineffective and
must not be used as a dry run.

## What counts as a pass

Ordinary Windows tests skip `TestSMBRegression` only when its four opt-in
variables are completely absent. Partial setup fails. The harness supplies the
exact UNC/local roots and matching token, then invokes the prebuilt test binary
through `go tool test2json` with an eight-minute test timeout. Each required leaf
must emit exactly one run and one pass event; a skipped or zero-test run fails.

The 18 required core cases cover:

- Local-to-UNC and UNC-to-UNC organize, copy and move
- UNC in-place copy and move without renaming the source directory
- Post-publication-fence rollback for all six combinations, verifying that
  publication really occurred before rejection, original bytes survive, and no
  transaction outputs remain
- Selected SRT subtitles, excluded ASS and unrelated subtitles, multipart video,
  generated NFO content, and expected source retention/removal
- In-place copy ignores hard/soft `LinkMode`, including multipart output
- Late foreign primary and subtitle occupants retain their bytes; the refused
  subtitle is explicitly reported as skipped and its source is retained
- Same-byte foreign primary/subtitle occupants retain their eagerly captured
  native identity, preventing a replace-with-equal-bytes false pass. Copy/link
  sources and unrelated retained files also keep their admitted native identity

Optional hard/soft-link tests first probe their native UNC capability without
changing Windows settings. Only named unsupported/privilege/symlink-policy errors
are skips; unexpected errors fail. A supported mode must preserve source videos
and publish selected subtitles as independent regular copies. The job summary
lists each optional result and the skip reason. Supported link behavior uses
`MoveFiles=false` with the chosen `LinkMode`, matching the organizer contract. Optional organize links retain multipart source
files; multipart destination publication is required by the core copy/move cases,
not asserted as a new organize-link behavior.

## Boundaries of this evidence

A successful run proves Windows MS-SMB over loopback backed by NTFS. It does not
prove Samba, TrueNAS, NFS, production NAS permissions, LAN latency, disconnect or
reconnect recovery, or a different SMB dialect. Local-to-UNC path crossing does
not itself prove distinct physical volumes or a particular EXDEV branch. The
known preexisting cross-device **history Undo** limitation is not fixed or tested
here; publication-fence rollback is a separate operation.

Linux fixture checks and Windows cross-compilation are useful static/contract
checks, but neither is evidence of an actual SMB pass. Record the Windows run's
UNC root, negotiated session/dialect, named case outcomes, optional skips, and
cleanup outcome when reporting results.

## Official references

- [GitHub-hosted runners and administrator privileges](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)
- [GitHub-hosted jobs use fresh VMs](https://docs.github.com/en/actions/how-tos/write-workflows/choose-where-workflows-run/choose-the-runner-for-a-job)
- [New-SmbShare, including Temporary and ineffective WhatIf](https://learn.microsoft.com/en-us/powershell/module/smbshare/new-smbshare?view=windowsserver2025-ps)
- [Get-SmbConnection session and dialect information](https://learn.microsoft.com/en-us/powershell/module/smbshare/get-smbconnection?view=windowsserver2025-ps)
- [New-NetFirewallRule address ranges and block semantics](https://learn.microsoft.com/en-us/powershell/module/netsecurity/new-netfirewallrule?view=windowsserver2025-ps)
- [Effective firewall enforcement status](https://learn.microsoft.com/en-us/windows/win32/fwp/wmi/wfascimprov/msft-netfirewallrule)
- [Protect SMB traffic from external networks](https://learn.microsoft.com/en-us/windows-server/storage/file-server/smb-secure-traffic)
- [SMB-over-QUIC certificate mappings](https://learn.microsoft.com/en-us/powershell/module/smbshare/get-smbservercertificatemapping?view=windowsserver2025-ps)
- [SMB server interfaces and RDMA capability](https://learn.microsoft.com/en-us/powershell/module/smbshare/get-smbservernetworkinterface?view=windowsserver2025-ps)
