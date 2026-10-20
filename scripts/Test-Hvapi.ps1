<#
.SYNOPSIS
    Calls every hvapi endpoint and reports which returned live data.

.DESCRIPTION
    Reads the endpoint list from the running service's /openapi.json, so it
    covers whatever this build serves, fills in the path parameters from the
    host's own data (a VM, one of its disks, a switch, an event log, a WMI
    class), calls each endpoint with GET and prints a table and a summary.

    Every JSON response is then checked three ways, so the result says the
    data is reliable and not only that the service answered:

      Schema       Against the service's own OpenAPI document: every required
                   field present and not null, every value of the documented
                   type, timestamps parseable. A null or a missing field where
                   the API promises one is a FAIL.
      Plausibility Rules in $Rules below: values that can never be right on a
                   working host, such as a running VM with 0 processors or 0 MB
                   of memory, a volume with more free space than size, an
                   unknown state name, a host with no logical processors.
      Consistency  Figures that must agree across endpoints: VM counts between
                   /vms, /vms/summary, /host and /metrics; the chosen VM's state
                   and memory between its endpoints.

    Results:
      PASS  200, schema valid, every rule satisfied
      WARN  200 and valid, but something was missing (notes present, empty list,
            or an expected condition such as a thumbnail of a VM that is off)
      FAIL  error status, timeout, bad JSON, or any data problem (each problem
            is printed under the endpoint)
      SKIP  no suitable parameter value on this host (no VM, no VHD, no switch)

    It also runs three security checks: /health without a key, a wrong key
    (expects 401) and a POST (expects 405).

    Works in Windows PowerShell 5.1 and PowerShell 7.

.PARAMETER Uri
    Base address of the service, for example https://10.0.0.10:8085

.PARAMETER ApiKey
    The API key (the value shown once by 'hvapi configure' or 'hvapi hash-key').

.PARAMETER SkipCertificateCheck
    Accept a self-signed or otherwise untrusted certificate. Fine for a test,
    not for monitoring.

.PARAMETER VM
    Name or ID of the VM to use for the /vms/{id}/* endpoints. Default: the
    first running VM, or the first VM if none runs.

.PARAMETER TimeoutSec
    Per-request timeout. Default 60 (on-demand calls can take a while).

.PARAMETER Json
    Print the results as JSON instead of a table.

.PARAMETER SelfTest
    Run the validators against built-in samples (a good VM, and one with a
    null name and 0 MB of memory) without contacting a service, and exit.

.EXAMPLE
    .\Test-Hvapi.ps1 -Uri https://10.0.0.10:8085 -ApiKey 0123abcd... -SkipCertificateCheck

.EXAMPLE
    .\Test-Hvapi.ps1 -Uri http://127.0.0.1:8085 -ApiKey (Get-Content key.txt) -VM "Web Server"
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)] [string] $Uri = '',
    [Parameter(Mandatory = $false)] [string] $ApiKey = '',
    [switch] $SkipCertificateCheck,
    [string] $VM,
    [int] $TimeoutSec = 60,
    [switch] $Json,
    [switch] $SelfTest
)

$ErrorActionPreference = 'Stop'
$Uri = $Uri.TrimEnd('/')
$script:results = New-Object System.Collections.Generic.List[object]

# ---- HTTP helpers -----------------------------------------------------------

$script:isCore = $PSVersionTable.PSVersion.Major -ge 6
if ($SkipCertificateCheck -and -not $script:isCore) {
    # Windows PowerShell 5.1 has no -SkipCertificateCheck; relax validation process-wide.
    [System.Net.ServicePointManager]::ServerCertificateValidationCallback = { $true }
}
if (-not $script:isCore) {
    # Windows PowerShell defaults to old protocols; hvapi needs TLS 1.2 or later.
    try { [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12 -bor [System.Net.SecurityProtocolType]::Tls13 }
    catch { [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12 }
}

function Invoke-Hvapi {
    <# Returns @{ Status; Body; Raw; ContentType; Ms; Error; Retries } and never throws.
       A 429 (per-key rate limit, default 10/s with a burst of 20) is retried after
       Retry-After, up to 5 times, because this script calls every endpoint in a row. #>
    param([string] $Path, [string] $Key = $ApiKey, [string] $Method = 'GET', [switch] $NoKey)
    $out = $null
    for ($attempt = 0; $attempt -le 5; $attempt++) {
        $out = Invoke-HvapiOnce -Path $Path -Key $Key -Method $Method -NoKey:$NoKey
        $out.Retries = $attempt
        if ($out.Status -ne 429) { break }
        $wait = 1
        if ($out.RetryAfter -gt 0) { $wait = $out.RetryAfter }
        Start-Sleep -Seconds $wait
    }
    return $out
}

function Invoke-HvapiOnce {
    param([string] $Path, [string] $Key, [string] $Method, [switch] $NoKey)
    $headers = @{}
    if (-not $NoKey) { $headers['X-API-Key'] = $Key }
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $out = @{ Status = 0; Body = $null; Raw = ''; ContentType = ''; Ms = 0; Error = ''; Retries = 0; RetryAfter = 0 }
    try {
        $args = @{ Uri = "$Uri$Path"; Method = $Method; Headers = $headers; TimeoutSec = $TimeoutSec; UseBasicParsing = $true }
        if ($script:isCore -and $SkipCertificateCheck) { $args['SkipCertificateCheck'] = $true }
        $resp = Invoke-WebRequest @args
        $out.Status = [int]$resp.StatusCode
        $out.ContentType = [string]$resp.Headers['Content-Type']
        if ($resp.Content -is [byte[]]) { $out.Raw = [System.Text.Encoding]::UTF8.GetString($resp.Content) } else { $out.Raw = [string]$resp.Content }
    } catch {
        $r = $_.Exception.Response
        if ($r) {
            try { $out.Status = [int]$r.StatusCode } catch { $out.Status = 0 }
            try {
                $ra = if ($script:isCore) { [string]$r.Headers.GetValues('Retry-After') } else { [string]$r.Headers['Retry-After'] }
                if ($ra) { $out.RetryAfter = [int]$ra }
            } catch { }
            try {
                if ($script:isCore) {
                    $out.Raw = [string]$_.ErrorDetails.Message
                } else {
                    $stream = $r.GetResponseStream()
                    $reader = New-Object System.IO.StreamReader($stream)
                    $out.Raw = $reader.ReadToEnd()
                }
            } catch { }
        }
        $out.Error = $_.Exception.Message
    }
    $sw.Stop()
    $out.Ms = [int]$sw.ElapsedMilliseconds
    if ($out.Raw -and ($out.ContentType -like 'application/json*' -or $out.Raw.TrimStart().StartsWith('{') -or $out.Raw.TrimStart().StartsWith('['))) {
        try { $out.Body = $out.Raw | ConvertFrom-Json } catch { }
    }
    return $out
}

function Add-Result {
    param([string] $Endpoint, [string] $Result, [int] $Status, [int] $Ms, [string] $Detail, [string[]] $Problems = @())
    $script:results.Add([pscustomobject]@{ Endpoint = $Endpoint; Result = $Result; Status = $Status; Ms = $Ms; Detail = $Detail; Problems = @($Problems) })
    if (-not $Json) {
        $color = switch ($Result) { 'PASS' { 'Green' } 'WARN' { 'Yellow' } 'FAIL' { 'Red' } default { 'DarkGray' } }
        Write-Host ("{0,-5} {1,3} {2,6}ms  {3,-44} {4}" -f $Result, $Status, $Ms, $Endpoint, $Detail) -ForegroundColor $color
        foreach ($pr in $Problems) { Write-Host ("      {0}" -f $pr) -ForegroundColor Red }
    }
}

function Describe-Body {
    <# One line about what came back, and whether it looks like live data. #>
    param($Body)
    if ($null -eq $Body) { return @{ Live = $false; Text = 'no JSON body' } }
    $parts = @()
    $live = $true
    $props = $Body.PSObject.Properties
    foreach ($name in 'count', 'vm_count', 'running', 'failed', 'stuck', 'critical', 'warnings') {
        $p = $props[$name]
        if ($p) { $parts += "$name=$($p.Value)" }
    }
    foreach ($name in 'status', 'state', 'health', 'name', 'vm_name', 'type', 'os', 'scheduler_type', 'all_running', 'clustered', 'enabled', 'tls', 'wmi_available') {
        $p = $props[$name]
        if ($p -and $null -ne $p.Value -and "$($p.Value)" -ne '') { $parts += "$name=$($p.Value)" }
    }
    foreach ($name in 'vms', 'volumes', 'adapters', 'switches', 'controllers', 'checkpoints', 'services', 'nodes', 'events', 'jobs', 'findings', 'processors', 'disks', 'classes', 'rows', 'metrics', 'logical_processors', 'ports', 'networks', 'logs', 'sources') {
        $p = $props[$name]
        if ($p -and $p.Value -is [array]) { $parts += "$name[$($p.Value.Count)]" }
    }
    $notes = $props['notes']
    if ($notes -and $notes.Value) {
        $n = @($notes.Value)
        if ($n.Count -gt 0) { $live = $false; $parts += "notes: " + (($n | Select-Object -First 2) -join ' | ') }
    }
    $warn = $props['warnings']
    if ($warn -and $warn.Value -is [array] -and $warn.Value.Count -gt 0) { $live = $false; $parts += "warnings: " + ($warn.Value -join ' | ') }
    if ($parts.Count -eq 0) { $parts += "fields=" + @($props).Count }
    return @{ Live = $live; Text = ($parts -join ', ') }
}


# ---- Validation: schema -----------------------------------------------------

function Resolve-Schema {
    param($Schema, $Spec)
    if ($null -eq $Schema) { return $null }
    if ($Schema.'$ref') {
        $name = ([string]$Schema.'$ref') -replace '^#/components/schemas/', ''
        return $Spec.components.schemas.$name
    }
    if ($Schema.allOf) { return Resolve-Schema -Schema (@($Schema.allOf)[0]) -Spec $Spec }
    return $Schema
}

function Test-Schema {
    <# Walks a response against its OpenAPI schema. Returns problem strings. #>
    param($Value, $Schema, $Spec, [string] $Path = '', [int] $Depth = 0)
    $problems = New-Object System.Collections.Generic.List[string]
    if ($Depth -gt 12) { return $problems }
    $schema = Resolve-Schema -Schema $Schema -Spec $Spec
    if ($null -eq $schema) { return $problems }
    $type = [string]$schema.type
    switch ($type) {
        'object' {
            if ($null -eq $Value) { $problems.Add("$Path is null but the API promises an object"); return $problems }
            if ($Value -is [array] -or $Value -is [string] -or $Value -is [ValueType]) { $problems.Add("$Path should be an object"); return $problems }
            $props = $Value.PSObject.Properties
            foreach ($req in @($schema.required)) {
                $p = $props[$req]
                if (-not $p) { $problems.Add("$Path.$req is missing but the API marks it required"); continue }
                if ($null -eq $p.Value) { $problems.Add("$Path.$req is null but the API marks it required") }
            }
            if ($schema.properties) {
                foreach ($p in $props) {
                    $ps = $schema.properties.($p.Name)
                    if ($ps -and $null -ne $p.Value) {
                        foreach ($x in (Test-Schema -Value $p.Value -Schema $ps -Spec $Spec -Path "$Path.$($p.Name)" -Depth ($Depth + 1))) { $problems.Add($x) }
                    }
                }
            } elseif ($schema.additionalProperties -and $schema.additionalProperties.type) {
                foreach ($p in $props) {
                    if ($null -ne $p.Value) {
                        foreach ($x in (Test-Schema -Value $p.Value -Schema $schema.additionalProperties -Spec $Spec -Path "$Path.$($p.Name)" -Depth ($Depth + 1))) { $problems.Add($x) }
                    }
                }
            }
        }
        'array' {
            if ($null -eq $Value) { $problems.Add("$Path is null but the API promises an array"); return $problems }
            if (-not ($Value -is [array] -or $Value -is [System.Collections.IList])) { $problems.Add("$Path should be an array"); return $problems }
            $i = 0
            foreach ($item in $Value) {
                if ($null -eq $item) { $problems.Add("$Path[$i] is null") }
                else { foreach ($x in (Test-Schema -Value $item -Schema $schema.items -Spec $Spec -Path "$Path[$i]" -Depth ($Depth + 1))) { $problems.Add($x) } }
                $i++
                if ($i -ge 200) { break }
            }
        }
        'string' {
            if ($Value -is [datetime]) {
                # PowerShell 7 turns ISO timestamps into DateTime while parsing JSON.
                if ([string]$schema.format -ne 'date-time') { $problems.Add("$Path should be a string, got a timestamp") }
                elseif ($Value.Year -lt 2000) { $problems.Add("$Path is an implausible timestamp: '$Value'") }
                return $problems
            }
            if (-not ($Value -is [string])) { $problems.Add("$Path should be a string, got $($Value.GetType().Name)"); return $problems }
            if ([string]$schema.format -eq 'date-time') {
                $dt = [datetime]::MinValue
                if (-not [datetime]::TryParse($Value, [cultureinfo]::InvariantCulture, [System.Globalization.DateTimeStyles]::AdjustToUniversal, [ref]$dt)) { $problems.Add("$Path is not a timestamp: '$Value'") }
                elseif ($dt.Year -lt 2000) { $problems.Add("$Path is an implausible timestamp: '$Value'") }
            }
        }
        'integer' {
            if ($Value -is [string] -or $Value -is [bool] -or -not ($Value -is [ValueType])) { $problems.Add("$Path should be an integer, got '$Value'") }
            elseif ($Value -ne [math]::Floor([double]$Value)) { $problems.Add("$Path should be an integer, got $Value") }
        }
        'number' {
            if ($Value -is [string] -or $Value -is [bool] -or -not ($Value -is [ValueType])) { $problems.Add("$Path should be a number, got '$Value'") }
        }
        'boolean' {
            if (-not ($Value -is [bool])) { $problems.Add("$Path should be true or false, got '$Value'") }
        }
    }
    return $problems
}

# ---- Validation: plausibility rules ----------------------------------------
#
# Each rule: Path (dotted; [] walks every element), Test, and for some tests a
# Value. When restricts the rule to elements where a sibling field has a value
# ("state==Running") or to a chosen VM that is running ("vmRunning"). A rule on
# a field that is absent is skipped: presence is the schema's job.
#
#   nonempty          non-empty string
#   gt0 / gte0        number above / at least 0
#   range:a-b         number within a..b inclusive
#   in                one of Value (array)
#   guid              a GUID
#   mac               a MAC address xx:xx:xx:xx:xx:xx
#   lteField          at most the sibling field named in Value
#   count:n / count>n array length equals / exceeds n
#   eq                equals Value

$VmStates = 'Running', 'Off', 'Saved', 'Paused', 'Starting', 'Stopping', 'Saving', 'Pausing', 'Resuming', 'Snapshotting', 'FastSaved', 'FastSaving', 'Other', 'Unknown', 'Shutting Down'
$Healths = 'OK', 'Degraded', 'Minor Failure', 'Major Failure', 'Critical Failure', 'Unknown', 'Non-recoverable Error'
$Heartbeats = 'OK', 'Error', 'No Contact', 'Lost Communication', 'Dormant', 'Paused', 'Unknown', 'Disabled'

$Rules = @{
    '/health' = @(
        @{ Path = 'status'; Test = 'eq'; Value = 'ok' },
        @{ Path = 'uptime_seconds'; Test = 'gte0' }
    )
    '/version' = @(
        @{ Path = 'version'; Test = 'nonempty' },
        @{ Path = 'commit'; Test = 'nonempty' },
        @{ Path = 'go'; Test = 'nonempty' }
    )
    '/status' = @(
        @{ Path = 'wmi_available'; Test = 'eq'; Value = $true },
        @{ Path = 'state_poll.runs'; Test = 'gt0' },
        @{ Path = 'settings_poll.runs'; Test = 'gt0' },
        @{ Path = 'state_poll.sources[].error'; Test = 'eq'; Value = ''; When = 'optional==False' },
        @{ Path = 'settings_poll.sources[].error'; Test = 'eq'; Value = ''; When = 'optional==False' },
        @{ Path = 'state_poll.sources[].rows'; Test = 'gt0'; When = 'optional==False' }
    )
    '/vms' = @(
        @{ Path = 'count'; Test = 'gte0' },
        @{ Path = 'vms[].id'; Test = 'guid' },
        @{ Path = 'vms[].name'; Test = 'nonempty' },
        @{ Path = 'vms[].state'; Test = 'in'; Value = $VmStates },
        @{ Path = 'vms[].health'; Test = 'in'; Value = $Healths },
        @{ Path = 'vms[].generation'; Test = 'in'; Value = 1, 2 },
        @{ Path = 'vms[].version'; Test = 'nonempty' },
        @{ Path = 'vms[].uptime_seconds'; Test = 'gt0'; When = 'state==Running' },
        @{ Path = 'vms[].process_id'; Test = 'gt0'; When = 'state==Running' }
    )
    '/vms/summary' = @(
        @{ Path = 'vms[].id'; Test = 'guid' },
        @{ Path = 'vms[].name'; Test = 'nonempty' },
        @{ Path = 'vms[].state'; Test = 'in'; Value = $VmStates },
        @{ Path = 'vms[].processor_count'; Test = 'gt0' },
        @{ Path = 'vms[].memory_assigned_mb'; Test = 'gt0'; When = 'state==Running' },
        @{ Path = 'vms[].memory_startup_mb'; Test = 'gt0' },
        @{ Path = 'vms[].processor_load_percent'; Test = 'range:0-100' },
        @{ Path = 'vms[].guest.heartbeat'; Test = 'in'; Value = $Heartbeats },
        @{ Path = 'vms[].snapshot_count'; Test = 'gte0' }
    )
    '/vms/{id}' = @(
        @{ Path = 'vm.id'; Test = 'guid' },
        @{ Path = 'vm.name'; Test = 'nonempty' },
        @{ Path = 'summary.processor_count'; Test = 'gt0' },
        @{ Path = 'summary.memory_assigned_mb'; Test = 'gt0'; When = 'vmRunning' },
        @{ Path = 'config.generation'; Test = 'in'; Value = 1, 2 }
    )
    '/vms/{id}/summary' = @(
        @{ Path = 'id'; Test = 'guid' },
        @{ Path = 'processor_count'; Test = 'gt0' },
        @{ Path = 'memory_startup_mb'; Test = 'gt0' },
        @{ Path = 'memory_assigned_mb'; Test = 'gt0'; When = 'vmRunning' },
        @{ Path = 'guest.heartbeat'; Test = 'in'; Value = $Heartbeats }
    )
    '/vms/{id}/config' = @(
        @{ Path = 'generation'; Test = 'in'; Value = 1, 2 },
        @{ Path = 'version'; Test = 'nonempty' },
        @{ Path = 'configuration_id'; Test = 'guid' },
        @{ Path = 'automatic_start_action'; Test = 'nonempty' },
        @{ Path = 'automatic_stop_action'; Test = 'nonempty' }
    )
    '/vms/{id}/processor' = @(
        @{ Path = 'count'; Test = 'gt0' },
        @{ Path = 'reserve_percent'; Test = 'range:0-100' },
        @{ Path = 'limit_percent'; Test = 'range:0-100' },
        @{ Path = 'relative_weight'; Test = 'gt0' },
        @{ Path = 'load_percent'; Test = 'range:0-100' },
        @{ Path = 'virtual_processors[].load_percent'; Test = 'range:0-100' }
    )
    '/vms/{id}/memory' = @(
        @{ Path = 'startup_mb'; Test = 'gt0' },
        @{ Path = 'assigned_mb'; Test = 'gt0'; When = 'vmRunning' },
        @{ Path = 'weight'; Test = 'gt0' },
        @{ Path = 'minimum_mb'; Test = 'lteField'; Value = 'startup_mb' },
        @{ Path = 'maximum_mb'; Test = 'gt0' }
    )
    '/vms/{id}/disks' = @(
        @{ Path = 'controllers[].type'; Test = 'in'; Value = 'IDE', 'SCSI', 'Fibre Channel', 'Unknown' },
        @{ Path = 'controllers[].drives[].disk_id'; Test = 'nonempty' },
        @{ Path = 'controllers[].drives[].media'; Test = 'in'; Value = 'VHD', 'ISO', 'Floppy image', 'Physical', 'None' },
        @{ Path = 'controllers[].drives[].path'; Test = 'nonempty'; When = 'media==VHD' }
    )
    '/vms/{id}/network' = @(
        @{ Path = 'adapters[].name'; Test = 'nonempty' },
        @{ Path = 'adapters[].mac_address'; Test = 'mac' },
        @{ Path = 'adapters[].type'; Test = 'in'; Value = 'Synthetic', 'Legacy' },
        @{ Path = 'adapters[].switch'; Test = 'nonempty'; When = 'connected==True' }
    )
    '/vms/{id}/integration' = @(
        @{ Path = 'services'; Test = 'count:6' },
        @{ Path = 'services[].name'; Test = 'nonempty' }
    )
    '/vms/{id}/checkpoints' = @(
        @{ Path = 'checkpoints[].id'; Test = 'guid' },
        @{ Path = 'checkpoints[].name'; Test = 'nonempty' },
        @{ Path = 'checkpoints[].age_days'; Test = 'gte0' },
        @{ Path = 'checkpoints[].disks[].path'; Test = 'nonempty' }
    )
    '/vms/{id}/replication' = @(
        @{ Path = 'state'; Test = 'nonempty' },
        @{ Path = 'health'; Test = 'nonempty' },
        @{ Path = 'replica_server'; Test = 'nonempty'; When = 'enabled==True' }
    )
    '/vms/{id}/security' = @( @{ Path = 'generation'; Test = 'in'; Value = 1, 2 } )
    '/vms/{id}/perf' = @(
        @{ Path = 'processors[].total_run_percent'; Test = 'range:0-100' },
        @{ Path = 'cpu_total_percent'; Test = 'range:0-100' },
        @{ Path = 'adapters[].bytes_in_per_sec'; Test = 'gte0' },
        @{ Path = 'disks[].path'; Test = 'nonempty' }
    )
    '/vms/{id}/disks/{diskId}/state' = @(
        @{ Path = 'path'; Test = 'nonempty' },
        @{ Path = 'chain'; Test = 'count>0' },
        @{ Path = 'chain[].type'; Test = 'in'; Value = 'Fixed', 'Dynamic', 'Differencing', 'Unknown' },
        @{ Path = 'chain[].format'; Test = 'in'; Value = 'VHD', 'VHDX', 'VHDSet', 'Unknown' },
        @{ Path = 'chain[].max_size_mb'; Test = 'gt0' },
        @{ Path = 'file_size_mb'; Test = 'gt0' },
        @{ Path = 'chain_broken'; Test = 'eq'; Value = $false }
    )
    '/host' = @(
        @{ Path = 'name'; Test = 'nonempty' },
        @{ Path = 'os'; Test = 'nonempty' },
        @{ Path = 'os_version'; Test = 'nonempty' },
        @{ Path = 'logical_processors'; Test = 'gt0' },
        @{ Path = 'cores'; Test = 'gt0' },
        @{ Path = 'memory_total_mb'; Test = 'gt0' },
        @{ Path = 'memory_free_mb'; Test = 'lteField'; Value = 'memory_total_mb' },
        @{ Path = 'uptime_seconds'; Test = 'gt0' },
        @{ Path = 'hypervisor_present'; Test = 'eq'; Value = $true },
        @{ Path = 'vm_count'; Test = 'gte0' }
    )
    '/host/services' = @(
        @{ Path = 'services'; Test = 'count:3' },
        @{ Path = 'services[].name'; Test = 'in'; Value = 'vmms', 'vmcompute', 'hvhost' },
        @{ Path = 'services[].state'; Test = 'nonempty' },
        @{ Path = 'all_running'; Test = 'eq'; Value = $true }
    )
    '/host/storage' = @(
        @{ Path = 'volumes'; Test = 'count>0' },
        @{ Path = 'volumes[].size_mb'; Test = 'gt0' },
        @{ Path = 'volumes[].free_mb'; Test = 'lteField'; Value = 'size_mb' },
        @{ Path = 'volumes[].free_percent'; Test = 'range:0-100' },
        @{ Path = 'volumes[].path'; Test = 'nonempty' }
    )
    '/host/network' = @(
        @{ Path = 'adapters'; Test = 'count>0' },
        @{ Path = 'adapters[].name'; Test = 'nonempty' },
        @{ Path = 'adapters[].status'; Test = 'nonempty' }
    )
    '/host/perf' = @(
        @{ Path = 'cpu_total_percent'; Test = 'range:0-100' },
        @{ Path = 'logical_processors'; Test = 'count>0' },
        @{ Path = 'logical_processors[].total_run_percent'; Test = 'range:0-100' },
        @{ Path = 'memory_available_mb'; Test = 'gt0' },
        @{ Path = 'disks[].free_percent'; Test = 'range:0-100' }
    )
    '/host/numa' = @(
        @{ Path = 'nodes'; Test = 'count>0' },
        @{ Path = 'nodes[].logical_processors'; Test = 'gt0' },
        @{ Path = 'nodes[].memory_mb'; Test = 'gt0' }
    )
    '/host/hypervisor' = @(
        @{ Path = 'present'; Test = 'eq'; Value = $true },
        @{ Path = 'os_build'; Test = 'nonempty' },
        @{ Path = 'scheduler_type'; Test = 'in'; Value = 'Classic', 'Classic (SMT disabled)', 'Core', 'Root' }
    )
    '/host/migration' = @( @{ Path = 'authentication'; Test = 'nonempty'; When = 'enabled==True' } )
    '/host/cluster' = @(
        @{ Path = 'name'; Test = 'nonempty'; When = 'clustered==True' },
        @{ Path = 'nodes'; Test = 'count>0'; When = 'clustered==True' },
        @{ Path = 'nodes[].state'; Test = 'in'; Value = 'Up', 'Down', 'Paused', 'Joining', 'Unknown' }
    )
    '/switches' = @(
        @{ Path = 'switches[].id'; Test = 'guid' },
        @{ Path = 'switches[].name'; Test = 'nonempty' },
        @{ Path = 'switches[].type'; Test = 'in'; Value = 'External', 'Internal', 'Private' },
        @{ Path = 'switches[].external_adapters'; Test = 'count>0'; When = 'type==External' }
    )
    '/switches/{id}' = @(
        @{ Path = 'id'; Test = 'guid' },
        @{ Path = 'type'; Test = 'in'; Value = 'External', 'Internal', 'Private' },
        @{ Path = 'ports[].kind'; Test = 'in'; Value = 'External', 'Internal', 'VM' },
        @{ Path = 'ports[].mac_address'; Test = 'mac'; When = 'kind==VM' }
    )
    '/switches/{id}/ports' = @(
        @{ Path = 'switch_name'; Test = 'nonempty' },
        @{ Path = 'ports[].kind'; Test = 'in'; Value = 'External', 'Internal', 'VM' },
        @{ Path = 'ports[].vm_name'; Test = 'nonempty'; When = 'kind==VM' }
    )
    '/jobs' = @(
        @{ Path = 'jobs[].id'; Test = 'nonempty' },
        @{ Path = 'jobs[].state'; Test = 'nonempty' },
        @{ Path = 'jobs[].percent_complete'; Test = 'range:0-100' }
    )
    '/events' = @(
        @{ Path = 'logs'; Test = 'count:6' },
        @{ Path = 'hours'; Test = 'gt0' },
        @{ Path = 'events[].id'; Test = 'gt0' },
        @{ Path = 'events[].level'; Test = 'in'; Value = 'Critical', 'Error', 'Warning', 'Information', 'Verbose' },
        @{ Path = 'events[].provider'; Test = 'nonempty' },
        @{ Path = 'logs[].error'; Test = 'eq'; Value = '' }
    )
    '/events/{log}' = @(
        @{ Path = 'logs'; Test = 'count:6' },
        @{ Path = 'events[].log'; Test = 'eq'; Value = 'vmms' }
    )
    '/diagnostics' = @(
        @{ Path = 'status'; Test = 'in'; Value = 'ok', 'warning', 'critical' },
        @{ Path = 'checks'; Test = 'count>14' },
        @{ Path = 'findings[].severity'; Test = 'in'; Value = 'critical', 'warning', 'info' },
        @{ Path = 'findings[].rule'; Test = 'nonempty' },
        @{ Path = 'findings[].message'; Test = 'nonempty' }
    )
    '/wmi/classes' = @( @{ Path = 'classes'; Test = 'count>50' }, @{ Path = 'classes[].namespace'; Test = 'nonempty' } )
    '/wmi/{class}' = @( @{ Path = 'count'; Test = 'gt0' }, @{ Path = 'rows[].__PATH'; Test = 'nonempty' } )
}

function Get-Field {
    param($Object, [string] $Name)
    if ($null -eq $Object) { return $null }
    $p = $Object.PSObject.Properties[$Name]
    if ($p) { return $p.Value } else { return $null }
}

function Test-When {
    <# "field==value" on the current element, or "vmRunning" for the chosen VM. #>
    param([string] $When, $Element, $Context)
    if (-not $When) { return $true }
    if ($When -eq 'vmRunning') { return [bool]$Context.VmRunning }
    if ($When -match '^(\w+)==(.*)$') {
        $v = Get-Field $Element $Matches[1]
        return ("$v" -eq $Matches[2])
    }
    return $true
}

function Test-Rule {
    <# Applies one rule to a value. Returns a problem string or $null. #>
    param($Value, $Rule, [string] $Where)
    $t = [string]$Rule.Test
    $n = 0.0
    $isNum = ($Value -is [ValueType]) -and -not ($Value -is [bool]) -and [double]::TryParse("$Value", [ref]$n)
    switch -Regex ($t) {
        '^nonempty$' { if ("$Value".Trim() -eq '') { return "$Where is empty" } }
        '^gt0$'      { if (-not $isNum) { return "$Where is not a number: '$Value'" }; if ($n -le 0) { return "$Where is $Value, expected above 0" } }
        '^gte0$'     { if (-not $isNum) { return "$Where is not a number: '$Value'" }; if ($n -lt 0) { return "$Where is $Value, expected 0 or more" } }
        '^range:(-?[\d.]+)-(-?[\d.]+)$' {
            $lo = [double]$Matches[1]; $hi = [double]$Matches[2]
            if (-not $isNum) { return "$Where is not a number: '$Value'" }
            if ($n -lt $lo -or $n -gt $hi) { return "$Where is $Value, expected $lo to $hi" }
        }
        '^in$'   { if (@($Rule.Value) -notcontains $Value) { return "$Where is '$Value', expected one of: $(@($Rule.Value) -join ', ')" } }
        '^eq$'   { if ("$Value" -ne "$($Rule.Value)") { return "$Where is '$Value', expected '$($Rule.Value)'" } }
        '^guid$' { if ("$Value" -notmatch '^[0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$') { return "$Where is not a GUID: '$Value'" } }
        '^mac$'  { if ("$Value" -notmatch '^([0-9A-F]{2}:){5}[0-9A-F]{2}$') { return "$Where is not a MAC address: '$Value'" } }
        '^count:(\d+)$' { $c = @($Value).Count; if ($c -ne [int]$Matches[1]) { return "$Where has $c entries, expected $($Matches[1])" } }
        '^count>(\d+)$' { $c = @($Value).Count; if ($c -le [int]$Matches[1]) { return "$Where has $c entries, expected more than $($Matches[1])" } }
    }
    return $null
}

function Test-Rules {
    <# Walks Path (with [] for arrays) and applies the rule to every leaf. #>
    param($Body, $RuleSet, $Context)
    $problems = New-Object System.Collections.Generic.List[string]
    foreach ($rule in @($RuleSet)) {
        $segments = ([string]$rule.Path) -split '\.'
        $targets = @(@{ Obj = $Body; Where = '' })
        $parent = $null
        for ($i = 0; $i -lt $segments.Count; $i++) {
            $seg = $segments[$i]
            $isArray = $seg.EndsWith('[]')
            $name = $seg.TrimEnd('[', ']')
            $next = @()
            foreach ($tg in $targets) {
                if ($null -eq $tg.Obj) { continue }
                $v = Get-Field $tg.Obj $name
                $where = if ($tg.Where) { "$($tg.Where).$name" } else { $name }
                if ($isArray) {
                    if ($null -eq $v) { continue }
                    $k = 0
                    foreach ($el in @($v)) { $next += @{ Obj = $el; Where = "$where[$k]"; Parent = $el }; $k++ }
                } else {
                    $next += @{ Obj = $v; Where = $where; Parent = $tg.Obj; Present = [bool]$tg.Obj.PSObject.Properties[$name] }
                }
            }
            $targets = $next
        }
        if ($rule.Test -like 'count*') {
            # Apply to the array itself, one level up.
            foreach ($tg in $targets) { }
        }
        foreach ($tg in $targets) {
            $element = if ($tg.ContainsKey('Parent')) { $tg.Parent } else { $Body }
            if (-not (Test-When -When $rule.When -Element $element -Context $Context)) { continue }
            if ($rule.Test -like 'count*') {
                if ($null -eq $tg.Obj) { continue }
                $pr = Test-Rule -Value $tg.Obj -Rule $rule -Where $tg.Where
            } else {
                if ($tg.ContainsKey('Present') -and -not $tg.Present) { continue }   # absent: the schema decides
                if ($null -eq $tg.Obj) { continue }                                  # null: the schema decides
                $pr = Test-Rule -Value $tg.Obj -Rule $rule -Where $tg.Where
            }
            if ($pr) { $problems.Add($pr) }
        }
    }
    return $problems
}

# ---- Validation: cross-endpoint consistency ---------------------------------

$script:bodies = @{}

function Test-Consistency {
    param($Context)
    $problems = New-Object System.Collections.Generic.List[string]
    $vms = $script:bodies['/vms']; $sum = $script:bodies['/vms/summary']; $host_ = $script:bodies['/host']; $metrics = $script:bodies['/metrics']
    if ($vms -and $sum -and $vms.count -ne $sum.count) { $problems.Add("/vms reports $($vms.count) VMs but /vms/summary reports $($sum.count)") }
    if ($vms -and $host_ -and $null -ne $host_.vm_count -and $vms.count -ne $host_.vm_count) { $problems.Add("/vms reports $($vms.count) VMs but /host reports vm_count $($host_.vm_count)") }
    if ($vms -and $metrics) {
        $lines = @(($metrics -split "`n") | Where-Object { $_ -like 'hvapi_vm_state{*' }).Count
        if ($lines -ne $vms.count) { $problems.Add("/metrics has $lines hvapi_vm_state series but /vms reports $($vms.count) VMs") }
        if ($metrics -notmatch 'hvapi_cache_ready 1') { $problems.Add('/metrics does not report hvapi_cache_ready 1') }
    }
    if ($Context.VmId) {
        $entry = if ($vms) { @($vms.vms) | Where-Object { $_.id -eq $Context.VmId } | Select-Object -First 1 } else { $null }
        $detail = $script:bodies['/vms/{id}']; $s1 = $script:bodies['/vms/{id}/summary']; $mem = $script:bodies['/vms/{id}/memory']; $cpu = $script:bodies['/vms/{id}/processor']
        if ($entry -and $detail -and $entry.state -ne $detail.vm.state) { $problems.Add("state differs: /vms says $($entry.state), /vms/{id} says $($detail.vm.state)") }
        if ($entry -and $s1 -and $entry.state -ne $s1.state) { $problems.Add("state differs: /vms says $($entry.state), /vms/{id}/summary says $($s1.state)") }
        if ($s1 -and $mem -and $s1.memory_assigned_mb -ne $mem.assigned_mb) { $problems.Add("memory differs: summary says $($s1.memory_assigned_mb) MB assigned, /memory says $($mem.assigned_mb)") }
        if ($s1 -and $cpu -and $cpu.count -gt 0 -and $s1.processor_count -ne $cpu.count) { $problems.Add("processor count differs: summary says $($s1.processor_count), /processor says $($cpu.count)") }
        if ($s1 -and $detail -and $s1.snapshot_count -ne $detail.summary.snapshot_count) { $problems.Add("checkpoint count differs between /vms/{id}/summary and /vms/{id}") }
    }
    return $problems
}

function Test-Endpoint {
    param([string] $Path, [string] $Label = $Path, [string] $ExpectContentType = 'application/json', [int[]] $AcceptStatus = @(200), $Schema = $null, $Spec = $null, $Context = $null)
    $r = Invoke-Hvapi -Path $Path
    $retried = if ($r.Retries -gt 0) { " (after $($r.Retries) rate-limit retries)" } else { '' }
    if ($r.Status -eq 200) {
        if ($ExpectContentType -ne 'application/json') {
            if ($r.ContentType -like "$ExpectContentType*") {
                $script:bodies[$Label] = $r.Raw
                Add-Result $Label 'PASS' 200 $r.Ms ("{0}, {1} bytes{2}" -f $r.ContentType, $r.Raw.Length, $retried)
            } else {
                Add-Result $Label 'FAIL' 200 $r.Ms "expected $ExpectContentType, got $($r.ContentType)"
            }
            return $r
        }
        $d = Describe-Body $r.Body
        if ($null -eq $r.Body) { Add-Result $Label 'FAIL' 200 $r.Ms ($d.Text + $retried); return $r }
        $script:bodies[$Label] = $r.Body
        $problems = New-Object System.Collections.Generic.List[string]
        if ($Schema) { foreach ($x in (Test-Schema -Value $r.Body -Schema $Schema -Spec $Spec -Path 'body')) { $problems.Add($x) } }
        if ($Rules.ContainsKey($Label)) { foreach ($x in (Test-Rules -Body $r.Body -RuleSet $Rules[$Label] -Context $Context)) { $problems.Add($x) } }
        $shown = @($problems | Select-Object -First 8)
        if ($problems.Count -gt 8) { $shown += "... and $($problems.Count - 8) more" }
        if ($problems.Count -gt 0) { Add-Result $Label 'FAIL' 200 $r.Ms ("{0} data problem(s); {1}{2}" -f $problems.Count, $d.Text, $retried) $shown }
        elseif ($d.Live) { Add-Result $Label 'PASS' 200 $r.Ms ($d.Text + $retried) }
        else { Add-Result $Label 'WARN' 200 $r.Ms ($d.Text + $retried) }
        return $r
    }
    if ($AcceptStatus -contains $r.Status) {
        $msg = if ($r.Body -and $r.Body.error) { $r.Body.error } else { "status $($r.Status)" }
        Add-Result $Label 'WARN' $r.Status $r.Ms $msg
        return $r
    }
    $msg = if ($r.Body -and $r.Body.error) { $r.Body.error } elseif ($r.Error) { $r.Error } else { "status $($r.Status)" }
    Add-Result $Label 'FAIL' $r.Status $r.Ms $msg
    return $r
}


# ---- 0. Self test of the validators -----------------------------------------

if ($SelfTest) {
    $miniSpec = '{"components":{"schemas":{"VM":{"type":"object","required":["id","name","state","memory_assigned_mb","tags","created"],"properties":{"id":{"type":"string"},"name":{"type":"string"},"state":{"type":"string"},"memory_assigned_mb":{"type":"integer"},"tags":{"type":"array","items":{"type":"string"}},"created":{"type":"string","format":"date-time"},"ratio":{"type":"number"}}},"List":{"type":"object","required":["count","vms"],"properties":{"count":{"type":"integer"},"vms":{"type":"array","items":{"$ref":"#/components/schemas/VM"}}}}}}}' | ConvertFrom-Json
    $listSchema = '{"$ref":"#/components/schemas/List"}' | ConvertFrom-Json
    $rules = @(
        @{ Path = 'vms[].id'; Test = 'guid' },
        @{ Path = 'vms[].name'; Test = 'nonempty' },
        @{ Path = 'vms[].state'; Test = 'in'; Value = 'Running', 'Off' },
        @{ Path = 'vms[].memory_assigned_mb'; Test = 'gt0'; When = 'state==Running' },
        @{ Path = 'vms[].ratio'; Test = 'range:0-1' },
        @{ Path = 'vms'; Test = 'count:2' }
    )
    $good = '{"count":2,"vms":[{"id":"11111111-1111-1111-1111-111111111111","name":"Web","state":"Running","memory_assigned_mb":4096,"tags":[],"created":"2026-10-20T08:15:30Z","ratio":0.5},{"id":"22222222-2222-2222-2222-222222222222","name":"Archive","state":"Off","memory_assigned_mb":0,"tags":["a"],"created":"2026-10-20T08:15:30Z"}]}' | ConvertFrom-Json
    $bad = '{"count":"2","vms":[{"id":"not-a-guid","name":null,"state":"Running","memory_assigned_mb":0,"created":"yesterday","ratio":7},{"id":"22222222-2222-2222-2222-222222222222","name":"Archive","state":"Broken","tags":null,"created":"2026-10-20T08:15:30Z","memory_assigned_mb":"lots"}]}' | ConvertFrom-Json
    $ok = $true
    $p1 = @(Test-Schema -Value $good -Schema $listSchema -Spec $miniSpec -Path 'body') + @(Test-Rules -Body $good -RuleSet $rules -Context @{})
    if ($p1.Count -ne 0) { $ok = $false; Write-Host "Self test: the good sample should have no problems, got:" -ForegroundColor Red; $p1 | ForEach-Object { Write-Host "  $_" } }
    $p2 = @(Test-Schema -Value $bad -Schema $listSchema -Spec $miniSpec -Path 'body') + @(Test-Rules -Body $bad -RuleSet $rules -Context @{})
    $expected = 'body.count should be an integer', 'body.vms[0].name is null but the API marks it required', 'body.vms[0].tags is missing but the API marks it required', 'body.vms[0].created is not a timestamp', 'body.vms[1].tags is null but the API marks it required', 'body.vms[1].memory_assigned_mb should be an integer', 'vms[0].id is not a GUID', 'vms[0].memory_assigned_mb is 0, expected above 0', 'vms[0].ratio is 7, expected 0 to 1', "vms[1].state is 'Broken'"
    foreach ($e in $expected) {
        if (-not ($p2 | Where-Object { $_.StartsWith($e) })) { $ok = $false; Write-Host "Self test: expected a problem starting with '$e'" -ForegroundColor Red }
    }
    Write-Host ""
    Write-Host "Problems found in the bad sample:" -ForegroundColor Cyan
    $p2 | ForEach-Object { Write-Host "  $_" }
    Write-Host ""
    if ($ok) { Write-Host "Self test passed: $($p2.Count) problems detected in the bad sample, none in the good one." -ForegroundColor Green; exit 0 }
    Write-Host "Self test FAILED" -ForegroundColor Red; exit 1
}

if (-not $Uri -or -not $ApiKey) { Write-Host "Usage: Test-Hvapi.ps1 -Uri https://host:8085 -ApiKey <key> [-SkipCertificateCheck] [-VM name] [-Json]   (or -SelfTest)" -ForegroundColor Yellow; exit 2 }

# ---- 1. Reachability and the endpoint list ----------------------------------

if (-not $Json) {
    Write-Host ""
    Write-Host "hvapi endpoint test against $Uri" -ForegroundColor Cyan
    Write-Host ("=" * 60)
}

$health = Invoke-Hvapi -Path '/health' -NoKey
if ($health.Status -ne 200) {
    $why = if ($health.Error) { $health.Error } else { "status $($health.Status)" }
    Write-Host "Cannot reach $Uri/health: $why" -ForegroundColor Red
    if ($why -match 'certificate|SSL|TLS|trust') { Write-Host "If the certificate is self-signed, add -SkipCertificateCheck." -ForegroundColor Yellow }
    exit 2
}
Add-Result '/health (no key)' 'PASS' 200 $health.Ms (Describe-Body $health.Body).Text

$spec = Invoke-Hvapi -Path '/openapi.json' -NoKey
if ($spec.Status -ne 200 -or -not $spec.Body -or -not $spec.Body.paths) {
    Add-Result '/openapi.json' 'FAIL' $spec.Status $spec.Ms 'could not read the endpoint list'
    exit 2
}
$paths = @($spec.Body.paths.PSObject.Properties.Name | Sort-Object)
Add-Result '/openapi.json (no key)' 'PASS' 200 $spec.Ms ("{0} endpoints, version {1}" -f $paths.Count, $spec.Body.info.version)

# ---- 2. Security checks -----------------------------------------------------

$bad = Invoke-Hvapi -Path '/version' -Key 'not-the-key'
if ($bad.Status -eq 401) { Add-Result 'wrong key -> 401' 'PASS' 401 $bad.Ms 'rejected' } else { Add-Result 'wrong key -> 401' 'FAIL' $bad.Status $bad.Ms 'a wrong key was not rejected' }
$nokey = Invoke-Hvapi -Path '/version' -NoKey
if ($nokey.Status -eq 401) { Add-Result 'no key -> 401' 'PASS' 401 $nokey.Ms 'rejected' } else { Add-Result 'no key -> 401' 'FAIL' $nokey.Status $nokey.Ms 'a missing key was not rejected' }
$post = Invoke-Hvapi -Path '/version' -Method 'POST'
if ($post.Status -eq 405) { Add-Result 'POST -> 405' 'PASS' 405 $post.Ms 'read-only confirmed' } else { Add-Result 'POST -> 405' 'FAIL' $post.Status $post.Ms 'POST was not rejected' }
if ($Uri -like 'http://*') { Add-Result 'transport' 'WARN' 0 0 'plain HTTP: the key and all data cross the network unencrypted' } else { Add-Result 'transport' 'PASS' 0 0 'HTTPS' }

# ---- 3. Pick parameter values from live data --------------------------------

$vmId = $null; $vmName = $null; $diskId = $null; $switchId = $null; $vmRunning = $false
$vms = Invoke-Hvapi -Path '/vms'
if ($vms.Status -eq 200 -and $vms.Body.vms) {
    $list = @($vms.Body.vms)
    $pick = $null
    if ($VM) {
        $pick = $list | Where-Object { $_.id -eq $VM -or $_.name -eq $VM } | Select-Object -First 1
        if (-not $pick) { Write-Host "VM '$VM' not found; using the default choice." -ForegroundColor Yellow }
    }
    if (-not $pick) { $pick = $list | Where-Object { $_.state -eq 'Running' } | Select-Object -First 1 }
    if (-not $pick) { $pick = $list | Select-Object -First 1 }
    if ($pick) {
        $vmId = $pick.id; $vmName = $pick.name; $vmRunning = ($pick.state -eq 'Running')
        $disks = Invoke-Hvapi -Path "/vms/$vmId/disks"
        if ($disks.Status -eq 200 -and $disks.Body.controllers) {
            foreach ($c in $disks.Body.controllers) {
                foreach ($dr in $c.drives) { if (-not $diskId -and $dr.media -eq 'VHD' -and $dr.disk_id) { $diskId = $dr.disk_id } }
            }
        }
    }
}
$sw = Invoke-Hvapi -Path '/switches'
if ($sw.Status -eq 200 -and $sw.Body.switches) { $switchId = @($sw.Body.switches)[0].id }
if (-not $Json) {
    Write-Host ""
    Write-Host ("Using VM: {0}   disk: {1}   switch: {2}" -f ($(if ($vmName) { "$vmName ($vmId)" } else { 'none' }), $(if ($diskId) { $diskId } else { 'none' }), $(if ($switchId) { $switchId } else { 'none' }))) -ForegroundColor Cyan
    Write-Host ""
}

# ---- 4. Every endpoint from the OpenAPI document ----------------------------

$context = @{ VmId = $vmId; VmName = $vmName; VmRunning = $vmRunning }
$done = @('/health', '/openapi.json')
foreach ($p in $paths) {
    if ($done -contains $p) { continue }
    $op = $spec.Body.paths.$p.get
    $schema = $null
    if ($op -and $op.responses.'200'.content.'application/json') { $schema = $op.responses.'200'.content.'application/json'.schema }
    $contentType = 'application/json'
    if ($op -and $op.responses.'200'.content) {
        $contentType = @($op.responses.'200'.content.PSObject.Properties.Name)[0]
        if ($contentType -like 'text/plain*') { $contentType = 'text/plain' }
    }
    if ($p -eq '/docs') { $contentType = 'text/html' }
    $path = $p
    $accept = @(200)
    if ($path -like '*{id}*' -and $path -like '/vms/*') {
        if (-not $vmId) { Add-Result $p 'SKIP' 0 0 'no VM on this host'; continue }
        $path = $path.Replace('{id}', $vmId)
    }
    if ($path -like '*{diskId}*') {
        if (-not $diskId) { Add-Result $p 'SKIP' 0 0 'the chosen VM has no virtual hard disk'; continue }
        $path = $path.Replace('{diskId}', $diskId)
    }
    if ($path -like '/switches/{id}*') {
        if (-not $switchId) { Add-Result $p 'SKIP' 0 0 'no virtual switch on this host'; continue }
        $path = $path.Replace('{id}', $switchId)
    }
    if ($path -like '*{log}*') { $path = $path.Replace('{log}', 'vmms') }
    if ($path -like '*{class}*') { $path = $path.Replace('{class}', 'Msvm_ComputerSystem') }
    if ($p -like '*/thumbnail') { $accept = @(200, 409); $path += '?width=160&height=120' }
    Test-Endpoint -Path $path -Label $p -ExpectContentType $contentType -AcceptStatus $accept -Schema $schema -Spec $spec.Body -Context $context | Out-Null
}

# Health and the document were fetched before the loop; validate them too.
$healthSchema = $spec.Body.paths.'/health'.get.responses.'200'.content.'application/json'.schema
$hp = @(Test-Schema -Value $health.Body -Schema $healthSchema -Spec $spec.Body -Path 'body') + @(Test-Rules -Body $health.Body -RuleSet $Rules['/health'] -Context $context)
if ($hp.Count -gt 0) { Add-Result '/health (data)' 'FAIL' 200 0 "$($hp.Count) data problem(s)" $hp }

# ---- 4b. Cross-endpoint consistency ----------------------------------------

$cp = @(Test-Consistency -Context $context)
if ($cp.Count -eq 0) { Add-Result 'consistency' 'PASS' 0 0 'counts and states agree across endpoints' }
else { Add-Result 'consistency' 'FAIL' 0 0 "$($cp.Count) disagreement(s)" $cp }

# ---- 5. Summary -------------------------------------------------------------

$pass = @($script:results | Where-Object Result -eq 'PASS').Count
$warn = @($script:results | Where-Object Result -eq 'WARN').Count
$fail = @($script:results | Where-Object Result -eq 'FAIL').Count
$skip = @($script:results | Where-Object Result -eq 'SKIP').Count

if ($Json) {
    [pscustomobject]@{ uri = $Uri; version = $spec.Body.info.version; pass = $pass; warn = $warn; fail = $fail; skip = $skip; results = $script:results } | ConvertTo-Json -Depth 5
} else {
    Write-Host ""
    Write-Host ("=" * 60)
    Write-Host ("Summary: {0} passed, {1} with warnings, {2} failed, {3} skipped" -f $pass, $warn, $fail, $skip) -ForegroundColor $(if ($fail -gt 0) { 'Red' } elseif ($warn -gt 0) { 'Yellow' } else { 'Green' })
    if ($warn -gt 0) { Write-Host "WARN usually means data this host does not have (see the notes), or the chosen VM is not running. Check /status for failing sources." -ForegroundColor Yellow }
    if ($fail -gt 0) { Write-Host "FAIL means the endpoint did not answer correctly or its data failed validation; the problems are listed under each endpoint." -ForegroundColor Red }
    Write-Host ""
}
if ($fail -gt 0) { exit 1 } else { exit 0 }
