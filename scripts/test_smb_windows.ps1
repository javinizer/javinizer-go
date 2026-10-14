#Requires -Version 7.0
#Requires -RunAsAdministrator
[CmdletBinding()]
param([switch]$CleanupOnly)

# Reviewed, synthetic-only GitHub-hosted Windows job. Never run on a workstation
# or against a NAS. No accounts, credentials, guest access, SMB1, authentication
# workarounds, global SMB settings, or service configuration are changed.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false

function Assert-That([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

Assert-That ($IsWindows -and $env:GITHUB_ACTIONS -eq 'true' -and $env:RUNNER_ENVIRONMENT -eq 'github-hosted') 'Only a fresh GitHub-hosted Windows runner is allowed.'
# Both exact ImageOS values are official Windows Server 2025 image variants.
# Validate the actual OS as well; never accept arbitrary win25-* labels.
Assert-That ($env:ImageOS -ceq 'win25' -or $env:ImageOS -ceq 'win25-vs2026') 'This harness requires an approved Windows Server 2025 image variant.'
$os = Get-CimInstance -ClassName Win32_OperatingSystem -ErrorAction Stop
$osVersion = [version]$os.Version
Assert-That ($os.ProductType -eq 3 -and $os.Caption -match '^Microsoft Windows Server 2025(?:\s|$)' -and $os.BuildNumber -eq '26100' -and $osVersion.Major -eq 10 -and $osVersion.Minor -eq 0 -and $osVersion.Build -eq 26100) 'The actual OS must be Windows Server 2025, version 10.0.26100.'
Write-Host "Verified runner image: ImageOS=$($env:ImageOS), ImageVersion=$($env:ImageVersion), OS=$($os.Caption), Version=$($os.Version), Build=$($os.BuildNumber)"
Assert-That ($env:GITHUB_RUN_ID -match '^\d+$' -and $env:GITHUB_RUN_ATTEMPT -match '^\d+$') 'Missing GitHub run identity.'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
Assert-That ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) 'An elevated runner identity is required.'
$runnerTemp = [IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd('\')
Assert-That ($runnerTemp -match '^[A-Za-z]:\\' -and (Test-Path -LiteralPath $runnerTemp -PathType Container)) 'RUNNER_TEMP must be a local directory.'
Assert-That (-not ((Get-Item -LiteralPath $runnerTemp).Attributes -band [IO.FileAttributes]::ReparsePoint)) 'RUNNER_TEMP cannot be a reparse point.'
$statePath = Join-Path $runnerTemp "javinizer-smb-$($env:GITHUB_RUN_ID)-$($env:GITHUB_RUN_ATTEMPT).json"
$binaryPath = Join-Path $runnerTemp 'javinizer-smb-tests.exe'
$logPath = Join-Path $runnerTemp 'javinizer-smb-tests.jsonl'
$remoteAddresses = @('0.0.0.0-126.255.255.255', '128.0.0.0-255.255.255.255', '::', '::2-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff')
$ruleSpecs = @(
    @{ Suffix = 'in-tcp'; Direction = 'Inbound'; Protocol = 'TCP'; Ports = @('139', '445') },
    @{ Suffix = 'out-tcp'; Direction = 'Outbound'; Protocol = 'TCP'; Ports = @('139', '445') },
    @{ Suffix = 'in-udp'; Direction = 'Inbound'; Protocol = 'UDP'; Ports = @('137', '138') },
    @{ Suffix = 'out-udp'; Direction = 'Outbound'; Protocol = 'UDP'; Ports = @('137', '138') }
)

function Get-SecurityDefaults {
    $server = Get-SmbServerConfiguration
    $client = Get-SmbClientConfiguration
    Assert-That (-not $server.EnableSMB1Protocol -and $server.EnableSMB2Protocol) 'SMB1 must already be disabled and SMB2/3 enabled; refusing to change security defaults.'
    Assert-That (-not $client.EnableInsecureGuestLogons) 'Insecure guest logons must already be disabled.'
    return [ordered]@{
        Server = ($server | Select-Object -Property @('EnableSMB1Protocol', 'EnableSMB2Protocol', 'RequireSecuritySignature', 'EnableSecuritySignature', 'EncryptData', 'RejectUnencryptedAccess'))
        Client = ($client | Select-Object -Property @('EnableInsecureGuestLogons', 'RequireSecuritySignature', 'EnableSecuritySignature', 'RequireEncryption'))
    } | ConvertTo-Json -Compress -Depth 4
}

function Assert-NoAlternateSMBTransports {
    # TCP/NetBIOS blocks do not cover SMB-over-QUIC (UDP, including alternate
    # ports) or SMB Direct (RDMA). Inspect all server mappings/interfaces and
    # refuse either capability; never disable or reconfigure it to pass.
    $quicMappings = @(Get-SmbServerCertificateMapping -ErrorAction Stop)
    Assert-That ($quicMappings.Count -eq 0) 'SMB-over-QUIC certificate mappings prevent proving TCP-only isolation.'
    $interfaces = @(Get-SmbServerNetworkInterface -ErrorAction Stop)
    Assert-That ($interfaces.Count -gt 0) 'Could not inspect any SMB server network interfaces.'
    foreach ($interface in $interfaces) {
        Assert-That ($interface.RdmaCapable -is [bool]) 'SMB interface RDMA capability is unknown.'
        Assert-That (-not $interface.RdmaCapable) 'An RDMA-capable SMB server interface prevents proving TCP-only isolation.'
    }
    Write-Host 'Verified alternate SMB transports absent: no QUIC certificate mappings or RDMA-capable server interfaces.'
}

function Assert-ServicesAndProfiles {
    # Fail rather than starting services or enabling a firewall on an unexpected
    # host. There is consequently no service state to restore in cleanup.
    foreach ($name in @('LanmanServer', 'LanmanWorkstation', 'MpsSvc', 'BFE')) {
        Assert-That ((Get-Service -Name $name).Status -eq 'Running') "Required service is not already running: $name"
    }
    $profiles = @(Get-NetFirewallProfile -PolicyStore ActiveStore)
    Assert-That ($profiles.Count -eq 3) 'Could not inspect every effective firewall profile.'
    foreach ($profile in $profiles) {
        Assert-That ([string]$profile.Enabled -eq 'True') "Firewall profile is disabled: $($profile.Name)"
        Assert-That ([string]$profile.AllowLocalFirewallRules -ne 'False') "Local firewall rules are disallowed: $($profile.Name)"
    }
    # Authenticated bypass rules can override blocks. This fresh test VM must
    # have none; do not delete or alter somebody else's rule to make it pass.
    $bypass = @(Get-NetFirewallRule -PolicyStore ActiveStore -Enabled True |
        Get-NetFirewallSecurityFilter | Where-Object { [string]$_.OverrideBlockRules -eq 'True' })
    Assert-That ($bypass.Count -eq 0) 'An enabled firewall bypass rule prevents proving isolation.'
    Assert-NoAlternateSMBTransports
}

function Normalize-Address([string]$Address) {
    $parts = $Address -split '-'
    return (($parts | ForEach-Object {
        $single = $_ -replace '/(32|128)$', ''
        ([Net.IPAddress]::Parse($single)).ToString().ToLowerInvariant()
    }) -join '-')
}

function Assert-SameSet($Actual, $Expected, [string]$Message) {
    Assert-That ((@($Actual | Sort-Object) -join ',') -eq (@($Expected | Sort-Object) -join ',')) $Message
}

function Assert-Isolation($State) {
    Assert-ServicesAndProfiles
    foreach ($spec in $ruleSpecs) {
        $name = "$($State.Share)-$($spec.Suffix)"
        $rules = @(Get-NetFirewallRule -PolicyStore ActiveStore -Name $name)
        Assert-That ($rules.Count -eq 1) "Isolation rule missing or ambiguous: $name"
        $rule = $rules[0]
        Assert-That ([string]$rule.Enabled -eq 'True' -and [string]$rule.Action -eq 'Block' -and [string]$rule.Direction -eq $spec.Direction -and [string]$rule.Profile -eq 'Any') "Incorrect effective isolation rule: $name"
        Assert-That ($rule.Description -ceq $State.Token) "Isolation rule has the wrong ownership marker: $name"
        Assert-That ([string]$rule.PrimaryStatus -eq 'OK' -and @($rule.EnforcementStatus).Count -eq 1 -and [string]$rule.EnforcementStatus[0] -eq 'Full') "Isolation rule is not fully enforced: $name ($($rule.EnforcementStatus))"
        $address = $rule | Get-NetFirewallAddressFilter
        Assert-SameSet ($address.RemoteAddress | ForEach-Object { Normalize-Address $_ }) ($remoteAddresses | ForEach-Object { Normalize-Address $_ }) "Wrong address coverage: $name"
        Assert-SameSet $address.LocalAddress @('Any') "Local addresses restricted unexpectedly: $name"
        $port = $rule | Get-NetFirewallPortFilter
        $number = if ($spec.Protocol -eq 'TCP') { '6' } else { '17' }
        Assert-That ([string]$port.Protocol -in @($spec.Protocol, $number)) "Wrong isolation protocol: $name"
        if ($spec.Direction -eq 'Inbound') {
            Assert-SameSet $port.LocalPort $spec.Ports "Wrong inbound ports: $name"
            Assert-SameSet $port.RemotePort @('Any') "Unexpected inbound source-port filter: $name"
        } else {
            Assert-SameSet $port.RemotePort $spec.Ports "Wrong outbound ports: $name"
            Assert-SameSet $port.LocalPort @('Any') "Unexpected outbound source-port filter: $name"
        }
        Assert-SameSet ($rule | Get-NetFirewallApplicationFilter).Program @('Any') "Unexpected application filter: $name"
        Assert-SameSet ($rule | Get-NetFirewallServiceFilter).Service @('Any') "Unexpected service filter: $name"
        Write-Host "Verified enforced isolation: $name, $($spec.Direction), $($spec.Protocol), ports $($spec.Ports -join ',')"
    }
}

function Set-PrivateDirectory([string]$Path) {
    $null = New-Item -ItemType Directory -Path $Path
    $acl = [Security.AccessControl.DirectorySecurity]::new()
    $acl.SetAccessRuleProtection($true, $false)
    $acl.SetOwner($identity.User)
    $rule = [Security.AccessControl.FileSystemAccessRule]::new($identity.User, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
    $acl.AddAccessRule($rule)
    Set-Acl -LiteralPath $Path -AclObject $acl
}

function Assert-PrivateDirectory([string]$Path) {
    $acl = Get-Acl -LiteralPath $Path
    $rules = @($acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier]))
    Assert-That ($acl.AreAccessRulesProtected -and $rules.Count -eq 1) "Directory ACL is not private: $Path"
    Assert-That ($rules[0].IdentityReference.Value -eq $identity.User.Value -and [string]$rules[0].AccessControlType -eq 'Allow' -and [string]$rules[0].FileSystemRights -eq 'FullControl') "Directory grants unexpected access: $Path"
}

function Assert-Share($State) {
    $shares = @(Get-SmbShare | Where-Object Name -eq $State.Share)
    Assert-That ($shares.Count -eq 1 -and $shares[0].Path -eq (Join-Path $State.Root 'share') -and $shares[0].Temporary -and $shares[0].EncryptData -and $shares[0].Description -ceq $State.Token) 'Share path, temporary status, or encryption is wrong.'
    $access = @(Get-SmbShareAccess -Name $State.Share)
    Assert-That ($access.Count -eq 1) 'The current runner account must be the ONLY share principal.'
    $sid = ([Security.Principal.NTAccount]::new($access[0].AccountName)).Translate([Security.Principal.SecurityIdentifier]).Value
    Assert-That ($sid -eq $identity.User.Value -and [string]$access[0].AccessControlType -eq 'Allow' -and [string]$access[0].AccessRight -eq 'Full') 'Unexpected share principal or rights.'
    Assert-PrivateDirectory $State.Root
    Assert-PrivateDirectory (Join-Path $State.Root 'share')
    Write-Host "Verified share ACL: $($State.Share), principal $($identity.Name), SID $sid"
}

function Assert-Session($State) {
    # The caller holds a file open through the UNC path during this query.
    $connections = @(Get-SmbConnection -ServerName '127.0.0.1' | Where-Object ShareName -eq $State.Share)
    Assert-That ($connections.Count -eq 1) 'No unique real SMB connection for this share; a local-path fallback is forbidden.'
    $connection = $connections[0]
    $sid = ([Security.Principal.NTAccount]::new($connection.UserName)).Translate([Security.Principal.SecurityIdentifier]).Value
    Assert-That ($sid -eq $identity.User.Value -and $connection.NumOpens -ge 1) 'SMB connection is not held open by the runner account.'
    Assert-That ([version]$connection.Dialect -ge [version]'3.0' -and $connection.Encrypted) 'An encrypted SMB3 session is required; refusing to weaken security.'
    Write-Host "Actual SMB session: server=$($connection.ServerName) share=$($connection.ShareName) dialect=$($connection.Dialect) encrypted=$($connection.Encrypted) opens=$($connection.NumOpens) user=$($connection.UserName)"
    return "SMB $($connection.Dialect), encrypted=$($connection.Encrypted), server=$($connection.ServerName), share=$($connection.ShareName)"
}

function Remove-OwnedTree([string]$Path, [string]$Root) {
    $full = [IO.Path]::GetFullPath($Path)
    Assert-That ($full -eq $Root -or $full.StartsWith($Root + '\', [StringComparison]::OrdinalIgnoreCase)) 'Cleanup path escaped the owned subtree.'
    $item = Get-Item -LiteralPath $full -Force
    # Never follow junctions/symlinks, including optional link-test residue.
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        if ($item.PSIsContainer) { [IO.Directory]::Delete($full) } else { [IO.File]::Delete($full) }
    } elseif ($item.PSIsContainer) {
        foreach ($child in Get-ChildItem -LiteralPath $full -Force) { Remove-OwnedTree $child.FullName $Root }
        [IO.Directory]::Delete($full)
    } else {
        [IO.File]::Delete($full)
    }
}

function Remove-TestResources {
    if (-not (Test-Path -LiteralPath $statePath)) { return }
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    Assert-That ($state.Token -cmatch '^[0-9a-f]{32}$' -and $state.Version -eq 1 -and $state.Sid -eq $identity.User.Value) 'Invalid cleanup ownership state.'
    $expectedShare = 'javinizer-smb-' + $state.Token
    $expectedRoot = Join-Path $runnerTemp $expectedShare
    Assert-That ($state.Share -ceq $expectedShare -and $state.Root -ceq $expectedRoot) 'Invalid cleanup resource names.'
    # FIRST remove this exact share. If removal cannot be proved, retain the
    # block rules and data until the fresh VM is discarded; never expose it.
    $shares = @(Get-SmbShare | Where-Object Name -eq $state.Share)
    Assert-That ($shares.Count -le 1) 'Ambiguous cleanup share.'
    if ($shares.Count -eq 1) {
        Assert-That ($shares[0].Path -eq (Join-Path $expectedRoot 'share') -and $shares[0].Description -ceq $state.Token) 'Refusing to delete a share with an unexpected path.'
        Remove-SmbShare -Name $state.Share -Force -Confirm:$false
    }
    Assert-That (@(Get-SmbShare | Where-Object Name -eq $state.Share).Count -eq 0) 'Share cleanup failed; isolation rules are retained.'
    $failures = [Collections.Generic.List[string]]::new()
    try {
        if (Test-Path -LiteralPath $expectedRoot) {
            Assert-That (-not ((Get-Item -LiteralPath $expectedRoot).Attributes -band [IO.FileAttributes]::ReparsePoint)) 'Owned root became a reparse point.'
            $marker = Join-Path $expectedRoot '.javinizer-smb-marker'
            Assert-That (-not ((Get-Item -LiteralPath $marker).Attributes -band [IO.FileAttributes]::ReparsePoint)) 'Cleanup marker became a reparse point.'
            Assert-That ((Get-Content -LiteralPath $marker -Raw) -ceq $state.Token) 'Cleanup marker does not match.'
            Remove-OwnedTree $expectedRoot $expectedRoot
        }
    } catch { $failures.Add($_.Exception.Message) }
    foreach ($spec in $ruleSpecs) {
        $name = "$expectedShare-$($spec.Suffix)"
        try {
            $rules = @(Get-NetFirewallRule -PolicyStore PersistentStore | Where-Object Name -eq $name)
            if ($rules.Count -gt 0) {
                Assert-That ($rules.Count -eq 1 -and $rules[0].Description -ceq $state.Token) "Refusing to remove an unowned rule: $name"
                Remove-NetFirewallRule -PolicyStore PersistentStore -Name $name
            }
            Assert-That (@(Get-NetFirewallRule -PolicyStore ActiveStore | Where-Object Name -eq $name).Count -eq 0) "Rule cleanup failed: $name"
        } catch { $failures.Add($_.Exception.Message) }
    }
    try { Assert-That ((Get-SecurityDefaults) -ceq $state.SecurityDefaults) 'Global SMB security defaults changed during the job.' } catch { $failures.Add($_.Exception.Message) }
    if ($failures.Count -gt 0) { throw "Cleanup failed: $($failures -join '; ')" }
    Remove-Item -LiteralPath $statePath
    Write-Host 'Cleanup verified: exact share, synthetic tree, and four isolation rules removed; service and SMB security settings unchanged.'
    if ($env:GITHUB_STEP_SUMMARY) { 'Cleanup: exact share, synthetic tree, and isolation rules removed; SMB security settings unchanged.' | Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY }
}

if ($CleanupOnly) {
    Remove-TestResources
    return
}

Assert-That (-not (Test-Path -LiteralPath $statePath)) 'Previous state exists; cleanup is required before another run.'
Assert-That (Test-Path -LiteralPath $binaryPath -PathType Leaf) 'Compile the Windows integration test binary before exposing any share.'
Assert-ServicesAndProfiles
$defaults = Get-SecurityDefaults
$token = [Guid]::NewGuid().ToString('N')
$state = [ordered]@{ Version = 1; Token = $token; Share = "javinizer-smb-$token"; Root = (Join-Path $runnerTemp "javinizer-smb-$token"); Sid = $identity.User.Value; SecurityDefaults = $defaults }
Assert-That (-not (Test-Path -LiteralPath $state.Root)) 'Unique synthetic root already exists.'
# Persist exact cleanup ownership BEFORE making a directory, rule, or share.
$stateStream = [IO.File]::Open($statePath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
try {
    $bytes = [Text.Encoding]::UTF8.GetBytes(($state | ConvertTo-Json -Depth 4))
    $stateStream.Write($bytes, 0, $bytes.Length)
} finally { $stateStream.Dispose() }
$handle = $null
try {
    Set-PrivateDirectory $state.Root
    [IO.File]::WriteAllText((Join-Path $state.Root '.javinizer-smb-marker'), $token)
    $shareRoot = Join-Path $state.Root 'share'
    Set-PrivateDirectory $shareRoot
    [IO.File]::WriteAllText((Join-Path $shareRoot '.javinizer-smb-marker'), $token)
    $null = New-Item -ItemType Directory -Path (Join-Path $shareRoot 'fixtures'), (Join-Path $state.Root 'local')
    foreach ($spec in $ruleSpecs) {
        $name = "$($state.Share)-$($spec.Suffix)"
        Assert-That (@(Get-NetFirewallRule -PolicyStore ActiveStore | Where-Object Name -eq $name).Count -eq 0) "Firewall name collision: $name"
        $params = @{ Name = $name; DisplayName = $name; Description = $token; PolicyStore = 'PersistentStore'; Direction = $spec.Direction; Action = 'Block'; Enabled = 'True'; Profile = 'Any'; Protocol = $spec.Protocol; RemoteAddress = $remoteAddresses }
        if ($spec.Direction -eq 'Inbound') { $params.LocalPort = $spec.Ports } else { $params.RemotePort = $spec.Ports }
        $null = New-NetFirewallRule @params
    }
    Assert-Isolation $state
    Assert-That (@(Get-SmbShare | Where-Object Name -eq $state.Share).Count -eq 0) 'Share name collision.'
    # New-SmbShare -WhatIf is NOT a dry run (documented ineffective). Only this
    # reviewed execution path creates the temporary, current-account-only share.
    $null = New-SmbShare -Name $state.Share -Path $shareRoot -Description $token -FullAccess $identity.Name -Temporary -EncryptData $true -CachingMode None -FolderEnumerationMode AccessBased
    Assert-Share $state
    $unc = "\\127.0.0.1\$($state.Share)"
    $handle = [IO.File]::Open((Join-Path $unc '.javinizer-smb-marker'), [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
    $session = Assert-Session $state
    $env:JAVINIZER_TEST_SMB_UNC = Join-Path $unc 'fixtures'
    $env:JAVINIZER_TEST_SMB_LOCAL = Join-Path $state.Root 'local'
    $env:JAVINIZER_TEST_SMB_TOKEN = $token
    $env:JAVINIZER_SMB_TEST_REQUIRED = '1'
    Write-Host "Running synthetic UNC integration cases at $($env:JAVINIZER_TEST_SMB_UNC)"
    & go tool test2json -t -p github.com/javinizer/javinizer-go/internal/workflow $binaryPath '-test.v=test2json' '-test.run=^TestSMBRegression$' '-test.count=1' '-test.timeout=8m' | Tee-Object -FilePath $logPath
    $testExit = $LASTEXITCODE
    $events = @(Get-Content -LiteralPath $logPath | ForEach-Object { $_ | ConvertFrom-Json -AsHashtable })
    Assert-That ($testExit -eq 0) "Native SMB tests failed with exit code $testExit; see named case output above."
    $coreNames = @()
    foreach ($source in @('local_to_unc', 'unc_to_unc', 'unc_inplace')) {
        foreach ($mode in @('copy', 'move')) {
            $coreNames += "${source}_${mode}", "${source}_${mode}_rollback"
        }
    }
    $coreNames += 'unc_inplace_ignores_hard', 'unc_inplace_ignores_soft', 'foreign_primary_no_clobber', 'foreign_subtitle_no_clobber', 'same_byte_primary_identity', 'same_byte_subtitle_identity'
    $coreRuns = @($events | Where-Object { $_.Action -eq 'run' -and $_['Test'] -like 'TestSMBRegression/Core/*' })
    $coreSkips = @($events | Where-Object { $_.Action -eq 'skip' -and $_['Test'] -like 'TestSMBRegression/Core*' })
    Assert-That ($coreRuns.Count -eq $coreNames.Count -and $coreSkips.Count -eq 0) 'Core test manifest changed or a required case was skipped.'
    foreach ($name in $coreNames) {
        $fullName = "TestSMBRegression/Core/$name"
        $runs = @($events | Where-Object { $_.Action -eq 'run' -and $_['Test'] -eq $fullName })
        $passes = @($events | Where-Object { $_.Action -eq 'pass' -and $_['Test'] -eq $fullName })
        Assert-That ($runs.Count -eq 1 -and $passes.Count -eq 1) "Required case did not execute and pass exactly once: $fullName"
    }
    Assert-That (@($events | Where-Object { $_.Action -eq 'pass' -and $_['Test'] -eq 'TestSMBRegression' }).Count -eq 1) 'Top-level real SMB suite did not pass.'
    Assert-Isolation $state
    Assert-Share $state
    $null = Assert-Session $state
    Assert-That ((Get-SecurityDefaults) -ceq $defaults) 'Global SMB security defaults changed during execution.'
    $summary = @('### Synthetic Windows loopback SMB', $session, "UNC root: $($env:JAVINIZER_TEST_SMB_UNC)", "Required core cases: $($coreNames.Count) executed and passed.")
    foreach ($mode in @('hard', 'soft')) {
        $name = "TestSMBRegression/OptionalLinks/$mode"
        $runs = @($events | Where-Object { $_.Action -eq 'run' -and $_['Test'] -eq $name })
        Assert-That ($runs.Count -eq 1) "Optional link case did not execute: $mode"
        $outcomes = @($events | Where-Object { $_.Action -in @('pass', 'skip') -and $_['Test'] -eq $name })
        Assert-That ($outcomes.Count -eq 1) "Optional link result is missing: $mode"
        $summary += "Optional $mode link capability: $($outcomes[0].Action)."
        if ($outcomes[0].Action -eq 'skip') {
            $reasons = @($events | Where-Object { $_.Action -eq 'output' -and $_['Test'] -eq $name -and $_['Output'] -match 'OPTIONAL SMB' } | ForEach-Object { $_['Output'].Trim() })
            Assert-That ($reasons.Count -gt 0) "Optional link skip has no capability reason: $mode"
            $summary += $reasons
        }
    }
    $summary += 'This covers Windows MS-SMB/NTFS loopback only. It does not validate Samba, TrueNAS, a LAN, disconnect recovery, or cross-device history Undo.'
    $summary | ForEach-Object { Write-Host $_ }
    if ($env:GITHUB_STEP_SUMMARY) { $summary | Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY }
} finally {
    if ($null -ne $handle) { $handle.Dispose() }
    foreach ($name in @('JAVINIZER_TEST_SMB_UNC', 'JAVINIZER_TEST_SMB_LOCAL', 'JAVINIZER_TEST_SMB_TOKEN', 'JAVINIZER_SMB_TEST_REQUIRED')) { [Environment]::SetEnvironmentVariable($name, $null, 'Process') }
    Remove-TestResources
}
