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

function Assert-EffectiveFirewallProfiles($Profiles) {
    Assert-SameSet $Profiles.Name @('Domain', 'Private', 'Public') 'Could not inspect every effective firewall profile.'
    foreach ($profile in $Profiles) {
        Assert-That ([string]$profile.Enabled -eq 'True') "Firewall profile is disabled: $($profile.Name)"
        Assert-That ([string]$profile.AllowLocalFirewallRules -eq 'True') "Local firewall rule enforcement is not enabled: $($profile.Name)"
        Assert-That (@($profile.DisabledInterfaceAliases | Where-Object { $null -ne $_ }).Count -eq 0) "Firewall profile has excluded interfaces: $($profile.Name)"
    }
}

function Assert-ServicesAndProfiles {
    # Fail rather than starting services or enabling a firewall on an unexpected
    # host. There is consequently no service state to restore in cleanup.
    foreach ($name in @('LanmanServer', 'LanmanWorkstation', 'MpsSvc', 'BFE')) {
        Assert-That ((Get-Service -Name $name).Status -eq 'Running') "Required service is not already running: $name"
    }
    Assert-EffectiveFirewallProfiles @(Get-NetFirewallProfile -PolicyStore ActiveStore)
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

function Write-EnforcementDiagnostics($Rule) {
    # Read-only evidence for a rejected guard. Providers can expose formatted
    # strings instead of numeric enums; retain the value and type without casts.
    # Emit each section independently so unavailable metadata cannot hide the
    # profile/interface evidence. The caller still rejects the original guard.
    function Describe-Value($Value) {
        [ordered]@{
            Name = [string]$Value
            Value = $Value
            Type = if ($null -eq $Value) { '<null>' } else { $Value.GetType().FullName }
        }
    }
    $sections = [ordered]@{
        Enforcement = { @($Rule.EnforcementStatus | ForEach-Object { Describe-Value $_ }) }
        Rule = { $Rule | Select-Object -Property @('Name', 'Enabled', 'Direction', 'Action', 'Profile', 'PrimaryStatus', 'Status', 'StatusCode') }
        EnforcementCimInstanceProperty = {
            $property = $Rule.CimInstanceProperties['EnforcementStatus']
            if ($null -eq $property) { throw 'EnforcementStatus CIM instance property is unavailable.' }
            [ordered]@{
                Name = $property.Name
                CimType = [string]$property.CimType
                Value = (Describe-Value $property.Value)
                Elements = @($property.Value | ForEach-Object { Describe-Value $_ })
            }
        }
        EnforcementCimClassProperty = {
            $class = Get-CimClass -Namespace root/standardcimv2 -ClassName MSFT_NetFirewallRule
            $property = $class.CimClassProperties['EnforcementStatus']
            if ($null -eq $property) { throw 'EnforcementStatus CIM class property is unavailable.' }
            [ordered]@{
                Name = $property.Name
                CimType = [string]$property.CimType
                Qualifiers = @($property.Qualifiers | ForEach-Object {
                    [ordered]@{ Name = $_.Name; Value = (Describe-Value $_.Value) }
                })
            }
        }
        CurrentProfileTypes = {
            $firewall = New-Object -ComObject HNetCfg.FwPolicy2
            try { Describe-Value $firewall.CurrentProfileTypes } finally { $null = [Runtime.InteropServices.Marshal]::FinalReleaseComObject($firewall) }
        }
        FirewallProfiles = { @(Get-NetFirewallProfile -PolicyStore ActiveStore | Select-Object -Property @('Name', 'Enabled', 'AllowLocalFirewallRules', 'DisabledInterfaceAliases')) }
        ConnectionProfiles = { @(Get-NetConnectionProfile | Select-Object -Property @('InterfaceIndex', 'InterfaceAlias', 'NetworkCategory', 'IPv4Connectivity', 'IPv6Connectivity')) }
        IPInterfaces = { @(Get-NetIPInterface | Select-Object -Property @('InterfaceIndex', 'InterfaceAlias', 'AddressFamily', 'ConnectionState')) }
        IPAddresses = { @(Get-NetIPAddress | Select-Object -Property @('InterfaceIndex', 'InterfaceAlias', 'AddressFamily', 'IPAddress', 'AddressState')) }
    }
    foreach ($section in $sections.GetEnumerator()) {
        try {
            $value = & $section.Value
            Write-Host ("Rejected enforcement guard diagnostics [$($section.Key)]: " + ($value | ConvertTo-Json -Depth 8 -Compress))
        } catch {
            Write-Host ("Rejected enforcement guard diagnostics [$($section.Key)] unavailable: " + $_.Exception.Message)
        }
    }
}

function Get-NetworkCoverageSnapshot {
    $firewall = New-Object -ComObject HNetCfg.FwPolicy2
    try {
        $activeProfile = $firewall.CurrentProfileTypes
        $comProfiles = @(foreach ($profileBit in @(1, 2, 4)) {
            $excluded = @($firewall.ExcludedInterfaces($profileBit) | Where-Object { $null -ne $_ })
            [ordered]@{ Profile = $profileBit; ExcludedInterfaces = $excluded }
        })
    } finally { $null = [Runtime.InteropServices.Marshal]::FinalReleaseComObject($firewall) }
    $connections = @(Get-NetConnectionProfile | Sort-Object InterfaceIndex | Select-Object -Property @('InterfaceIndex', 'InterfaceAlias', 'NetworkCategory'))
    $interfaces = @(Get-NetIPInterface | Sort-Object -Property @('InterfaceIndex', 'AddressFamily') | Select-Object -Property @('InterfaceIndex', 'InterfaceAlias', 'AddressFamily', 'ConnectionState'))
    $addresses = @(Get-NetIPAddress | Sort-Object -Property @('InterfaceIndex', 'AddressFamily', 'IPAddress') | Select-Object -Property @('InterfaceIndex', 'InterfaceAlias', 'AddressFamily', 'IPAddress', 'AddressState'))
    return ConvertTo-NetworkCoverageSnapshot $activeProfile $comProfiles $connections $interfaces $addresses
}

function ConvertTo-NetworkCoverageSnapshot($activeProfile, $comProfiles, $connections, $interfaces, $addresses) {
    # A single active profile makes the status set unambiguous. Do not infer
    # array-position/profile associations or accept multiple/unknown profiles.
    Assert-That ($activeProfile -is [int] -and $activeProfile -in @(1, 2, 4)) 'Exactly one known active firewall profile is required.'
    Assert-SameSet $comProfiles.Profile @(1, 2, 4) 'Could not inspect COM exclusions for every firewall profile.'
    foreach ($profile in $comProfiles) {
        Assert-That (@($profile.ExcludedInterfaces | Where-Object { $null -ne $_ }).Count -eq 0) "COM firewall profile $($profile.Profile) has excluded interfaces."
    }
    Assert-That ($connections.Count -gt 0 -and $interfaces.Count -gt 0 -and $addresses.Count -gt 0) 'Network coverage information is incomplete.'
    $profileBits = @{ DomainAuthenticated = 1; Private = 2; Public = 4 }
    foreach ($connection in $connections) {
        $category = [string]$connection.NetworkCategory
        Assert-That ($profileBits.ContainsKey($category) -and $profileBits[$category] -eq $activeProfile) 'A connection profile disagrees with the active firewall profile.'
        Assert-That ($connection.InterfaceIndex -in $interfaces.InterfaceIndex) 'A connection profile has no inspected IP interface.'
    }
    foreach ($address in $addresses) {
        Assert-That ($address.InterfaceIndex -in $interfaces.InterfaceIndex) 'An IP address has no inspected interface.'
        $null = [Net.IPAddress]::Parse($address.IPAddress)
    }
    # Get-NetConnectionProfile describes physical-adapter connection profiles,
    # not every host IP interface. Keep unclassified host interfaces (including
    # vEthernet NAT) in the snapshot: no interface is omitted from the proof.
    # Coverage comes from all three enabled profiles with NO exclusions, the
    # single authoritative active-profile mask, and each rule's Any profile,
    # interface alias and interface type, not an invented NAT network category.
    $unclassified = @($interfaces | Where-Object { $_.InterfaceIndex -notin $connections.InterfaceIndex } | Select-Object -Property @('InterfaceIndex', 'InterfaceAlias') -Unique)
    return [ordered]@{
        CurrentProfileTypes = $activeProfile
        COMProfiles = $comProfiles
        ConnectionProfiles = $connections
        IPInterfaces = $interfaces
        IPAddresses = $addresses
        InterfacesWithoutConnectionProfile = $unclassified
    } | ConvertTo-Json -Depth 5 -Compress
}

function Test-FullActiveEnforcement($Property) {
    # MSFT_NetFirewallRule documents UInt16[]: Full=1, InactiveProfile=5.
    # MS-FASP defines 5 as at least one target profile inactive; it is not an
    # array-position mapping. Require actual enforcement (1), never 5 alone.
    # Native NetSecurity formats these as strings Enforced/ProfileInactive.
    # Read the raw CIM value; never coerce names or assume status array order.
    return ($null -ne $Property -and [string]$Property.CimType -eq 'UInt16Array' -and
        $Property.Value -is [uint16[]] -and $Property.Value.Count -gt 0 -and
        $Property.Value -contains 1 -and @($Property.Value | Where-Object { $_ -notin @(1, 5) }).Count -eq 0)
}

function Assert-StableNetworkCoverage([string]$Current, [string]$Expected) {
    Assert-That ($Current -ceq $Expected) 'Network profile/interface coverage changed.'
}

function Assert-IsolationGuardContracts {
    # Pure synthetic guard checks: no services, firewall, share or network I/O.
    $cases = @(
        @{ Name = 'full'; Value = [uint16[]]@(1); Accept = $true },
        @{ Name = 'inactive-and-full'; Value = [uint16[]]@(5, 1); Accept = $true },
        @{ Name = 'full-and-inactive'; Value = [uint16[]]@(1, 5); Accept = $true },
        @{ Name = 'inactive-only'; Value = [uint16[]]@(5); Accept = $false },
        @{ Name = 'empty'; Value = [uint16[]]@(); Accept = $false },
        @{ Name = 'null'; Value = $null; Accept = $false },
        @{ Name = 'formatted-strings'; Value = @('ProfileInactive', 'Enforced'); Accept = $false },
        @{ Name = 'numeric-strings'; Value = @('5', '1'); Accept = $false },
        @{ Name = 'wrong-numeric-type'; Value = [int[]]@(5, 1); Accept = $false },
        @{ Name = 'scalar'; Value = [uint16]1; Accept = $false },
        @{ Name = 'enum'; Value = [DayOfWeek]::Monday; Accept = $false }
    )
    foreach ($code in @(0..25) + @(65535)) {
        if ($code -notin @(1, 5)) { $cases += @{ Name = "error-$code"; Value = [uint16[]]@(1, $code); Accept = $false } }
    }
    foreach ($case in $cases) {
        $property = [pscustomobject]@{ CimType = 'UInt16Array'; Value = $case.Value }
        Assert-That ((Test-FullActiveEnforcement $property) -eq $case.Accept) "Enforcement decoder contract failed: $($case.Name)"
    }
    Assert-That (-not (Test-FullActiveEnforcement $null)) 'Missing enforcement property was accepted.'
    Assert-That (-not (Test-FullActiveEnforcement ([pscustomobject]@{ CimType = 'StringArray'; Value = [uint16[]]@(1) }))) 'Incorrect CIM type was accepted.'
    $profiles = @(foreach ($name in @('Domain', 'Private', 'Public')) { [pscustomobject]@{ Name = $name; Enabled = 'True'; AllowLocalFirewallRules = 'True'; DisabledInterfaceAliases = $null } })
    $com = @(foreach ($bit in @(1, 2, 4)) { [pscustomobject]@{ Profile = $bit; ExcludedInterfaces = $null } })
    $connections = @([pscustomobject]@{ InterfaceIndex = 14; InterfaceAlias = 'test-ethernet'; NetworkCategory = 'Public' })
    $interfaces = @([pscustomobject]@{ InterfaceIndex = 14; InterfaceAlias = 'test-ethernet' }, [pscustomobject]@{ InterfaceIndex = 10; InterfaceAlias = 'test-unclassified-nat' })
    $addresses = @([pscustomobject]@{ InterfaceIndex = 14; IPAddress = '10.1.0.1' }, [pscustomobject]@{ InterfaceIndex = 10; IPAddress = '172.27.0.1' })
    Assert-EffectiveFirewallProfiles $profiles
    $snapshot = ConvertTo-NetworkCoverageSnapshot 4 $com $connections $interfaces $addresses
    Assert-StableNetworkCoverage $snapshot $snapshot
    $rejects = [ordered]@{
        'no-active-profile' = { $null = ConvertTo-NetworkCoverageSnapshot 0 $com $connections $interfaces $addresses }
        'missing-effective-profile' = { Assert-EffectiveFirewallProfiles @($profiles[0], $profiles[1]) }
        'restricted-interface' = { Assert-SameSet @('test-unclassified-nat') @('Any') 'Restricted interface must reject.' }
        'restricted-interface-type' = { Assert-SameSet @('Wired') @('Any') 'Restricted interface type must reject.' }
        'mixed-active-profiles' = { $null = ConvertTo-NetworkCoverageSnapshot 6 $com $connections $interfaces $addresses }
        'unknown-active-profile' = { $null = ConvertTo-NetworkCoverageSnapshot 8 $com $connections $interfaces $addresses }
        'string-active-profile' = { $null = ConvertTo-NetworkCoverageSnapshot '4' $com $connections $interfaces $addresses }
        'disabled-profile' = { try { $profiles[0].Enabled = 'False'; Assert-EffectiveFirewallProfiles $profiles } finally { $profiles[0].Enabled = 'True' } }
        'local-rules-disallowed' = { try { $profiles[0].AllowLocalFirewallRules = 'False'; Assert-EffectiveFirewallProfiles $profiles } finally { $profiles[0].AllowLocalFirewallRules = 'True' } }
        'local-rules-unknown' = { try { $profiles[0].AllowLocalFirewallRules = 'NotConfigured'; Assert-EffectiveFirewallProfiles $profiles } finally { $profiles[0].AllowLocalFirewallRules = 'True' } }
        'excluded-effective-interface' = { try { $profiles[0].DisabledInterfaceAliases = @('test-unclassified-nat'); Assert-EffectiveFirewallProfiles $profiles } finally { $profiles[0].DisabledInterfaceAliases = $null } }
        'excluded-com-interface' = { try { $com[0].ExcludedInterfaces = @('test-unclassified-nat'); $null = ConvertTo-NetworkCoverageSnapshot 4 $com $connections $interfaces $addresses } finally { $com[0].ExcludedInterfaces = $null } }
        'conflicting-nlm-category' = { try { $connections[0].NetworkCategory = 'Private'; $null = ConvertTo-NetworkCoverageSnapshot 4 $com $connections $interfaces $addresses } finally { $connections[0].NetworkCategory = 'Public' } }
        'changed-snapshot' = { Assert-StableNetworkCoverage ($snapshot + 'changed') $snapshot }
    }
    foreach ($case in $rejects.GetEnumerator()) {
        $rejected = $false
        try { & $case.Value } catch { $rejected = $true }
        Assert-That $rejected "Isolation guard contract did not reject: $($case.Key)"
    }
    Write-Host "Isolation guard contracts passed: $($cases.Count + 2) decoder cases, valid unclassified-interface coverage, and $($rejects.Count) rejected policy/snapshot cases."
}

function Assert-Isolation($State) {
    Assert-ServicesAndProfiles
    $networkBefore = Get-NetworkCoverageSnapshot
    foreach ($spec in $ruleSpecs) {
        $name = "$($State.Share)-$($spec.Suffix)"
        $rules = @(Get-NetFirewallRule -PolicyStore ActiveStore -Name $name)
        Assert-That ($rules.Count -eq 1) "Isolation rule missing or ambiguous: $name"
        $rule = $rules[0]
        Assert-That ([string]$rule.Enabled -eq 'True' -and [string]$rule.Action -eq 'Block' -and [string]$rule.Direction -eq $spec.Direction -and [string]$rule.Profile -eq 'Any') "Incorrect effective isolation rule: $name"
        Assert-That ($rule.Description -ceq $State.Token) "Isolation rule has the wrong ownership marker: $name"
        $fullyEnforced = [string]$rule.PrimaryStatus -eq 'OK' -and (Test-FullActiveEnforcement $rule.CimInstanceProperties['EnforcementStatus'])
        if (-not $fullyEnforced) { Write-EnforcementDiagnostics $rule }
        Assert-That $fullyEnforced "Isolation rule is not fully enforced: $name ($($rule.EnforcementStatus))"
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
        Assert-SameSet ($rule | Get-NetFirewallInterfaceFilter).InterfaceAlias @('Any') "Unexpected interface restriction: $name"
        Assert-SameSet ($rule | Get-NetFirewallInterfaceTypeFilter).InterfaceType @('Any') "Unexpected interface-type restriction: $name"
        Write-Host "Verified enforced isolation: $name, $($spec.Direction), $($spec.Protocol), ports $($spec.Ports -join ',')"
    }
    Assert-ServicesAndProfiles
    Assert-StableNetworkCoverage (Get-NetworkCoverageSnapshot) $networkBefore
    Write-Host "Verified all-profile/all-interface coverage (including unclassified host interfaces): $networkBefore"
    return $networkBefore
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

Assert-IsolationGuardContracts
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
    $networkCoverage = Assert-Isolation $state
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
    Assert-StableNetworkCoverage (Assert-Isolation $state) $networkCoverage
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
