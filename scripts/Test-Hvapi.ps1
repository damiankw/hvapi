<#
.SYNOPSIS
    Calls every hvapi endpoint and reports which returned live data.

.DESCRIPTION
    Reads the endpoint list from the running service's /openapi.json, so it
    covers whatever this build serves, fills in the path parameters from the
    host's own data (a VM, one of its disks, a switch, an event log, a WMI
    class), calls each endpoint with GET and prints a table and a summary.

    Results:
      PASS  200 with data
      WARN  200 but something was missing (notes present, empty list, or an
            expected condition such as a thumbnail of a VM that is not running)
      FAIL  any other outcome: error status, timeout, bad JSON
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

.EXAMPLE
    .\Test-Hvapi.ps1 -Uri https://10.0.0.10:8085 -ApiKey 0123abcd... -SkipCertificateCheck

.EXAMPLE
    .\Test-Hvapi.ps1 -Uri http://127.0.0.1:8085 -ApiKey (Get-Content key.txt) -VM "Web Server"
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)] [string] $Uri,
    [Parameter(Mandatory = $true)] [string] $ApiKey,
    [switch] $SkipCertificateCheck,
    [string] $VM,
    [int] $TimeoutSec = 60,
    [switch] $Json
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
    param([string] $Endpoint, [string] $Result, [int] $Status, [int] $Ms, [string] $Detail)
    $script:results.Add([pscustomobject]@{ Endpoint = $Endpoint; Result = $Result; Status = $Status; Ms = $Ms; Detail = $Detail })
    if (-not $Json) {
        $color = switch ($Result) { 'PASS' { 'Green' } 'WARN' { 'Yellow' } 'FAIL' { 'Red' } default { 'DarkGray' } }
        Write-Host ("{0,-5} {1,3} {2,6}ms  {3,-44} {4}" -f $Result, $Status, $Ms, $Endpoint, $Detail) -ForegroundColor $color
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

function Test-Endpoint {
    param([string] $Path, [string] $Label = $Path, [string] $ExpectContentType = 'application/json', [int[]] $AcceptStatus = @(200))
    $r = Invoke-Hvapi -Path $Path
    $retried = if ($r.Retries -gt 0) { " (after $($r.Retries) rate-limit retries)" } else { '' }
    if ($r.Status -eq 200) {
        if ($ExpectContentType -ne 'application/json') {
            if ($r.ContentType -like "$ExpectContentType*") {
                Add-Result $Label 'PASS' 200 $r.Ms ("{0}, {1} bytes{2}" -f $r.ContentType, $r.Raw.Length, $retried)
            } else {
                Add-Result $Label 'FAIL' 200 $r.Ms "expected $ExpectContentType, got $($r.ContentType)"
            }
            return $r
        }
        $d = Describe-Body $r.Body
        if ($null -eq $r.Body) { Add-Result $Label 'FAIL' 200 $r.Ms ($d.Text + $retried) }
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

$vmId = $null; $vmName = $null; $diskId = $null; $switchId = $null
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
        $vmId = $pick.id; $vmName = $pick.name
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

$done = @('/health', '/openapi.json')
foreach ($p in $paths) {
    if ($done -contains $p) { continue }
    $op = $spec.Body.paths.$p.get
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
    Test-Endpoint -Path $path -Label $p -ExpectContentType $contentType -AcceptStatus $accept | Out-Null
}

# ---- 5. Summary -------------------------------------------------------------

$pass = @($script:results | Where-Object Result -eq 'PASS').Count
$warn = @($script:results | Where-Object Result -eq 'WARN').Count
$fail = @($script:results | Where-Object Result -eq 'FAIL').Count
$skip = @($script:results | Where-Object Result -eq 'SKIP').Count

if ($Json) {
    [pscustomobject]@{ uri = $Uri; version = $spec.Body.info.version; pass = $pass; warn = $warn; fail = $fail; skip = $skip; results = $script:results } | ConvertTo-Json -Depth 4
} else {
    Write-Host ""
    Write-Host ("=" * 60)
    Write-Host ("Summary: {0} passed, {1} with warnings, {2} failed, {3} skipped" -f $pass, $warn, $fail, $skip) -ForegroundColor $(if ($fail -gt 0) { 'Red' } elseif ($warn -gt 0) { 'Yellow' } else { 'Green' })
    if ($warn -gt 0) { Write-Host "WARN usually means data this host does not have (see the notes), or the chosen VM is not running. Check /status for failing sources." -ForegroundColor Yellow }
    if ($fail -gt 0) { Write-Host "FAIL means the endpoint did not answer correctly. Run with -Verbose or look at the service log." -ForegroundColor Red }
    Write-Host ""
}
if ($fail -gt 0) { exit 1 } else { exit 0 }
