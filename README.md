# hvapi

A lightweight, **read-only** HTTP/JSON API that runs on a Hyper-V host and exposes Hyper-V diagnostic data to other machines.

- One self-contained `hvapi.exe`, nothing to install on the host
- Read-only by design: only `GET` requests, no VM changes of any kind
- HTTPS built in, API key and IP allowlist on every request
- Documents itself: Swagger UI at `/docs`, OpenAPI at `/openapi.json`

> **Status:** 0.7.x completes the planned feature set: the VMs in depth (inventory, summary, configuration, processor, memory, disks and disk state, network, integration services, key-value pairs, checkpoints, replication, devices, security, metering, performance counters, console thumbnails), the host (hardware, OS, services, storage, network, performance, NUMA, hypervisor, migration, cluster), virtual switches, jobs, the Hyper-V event logs, a diagnostics report, Prometheus metrics and raw WMI access, all over HTTPS. Changes are listed in [CHANGELOG.md](CHANGELOG.md); the design notes are in `CLAUDE.md`.

## Secure the connection first

hvapi hands out everything about your virtual machines to anyone with an API key, and the key itself travels in a request header. **Over plain HTTP both are visible to anyone on the path.** Treat the choice below as part of the install, not an afterthought:

| Setup | When it fits | How |
| --- | --- | --- |
| **Built-in HTTPS with a certificate from your CA** (recommended) | Any network, including reaching the host from outside the management segment | Put the PEM certificate and key in `config.json` under `tls`. hvapi reloads them when they change, so renewals need no restart. |
| **Built-in HTTPS with a self-signed certificate** | A handful of clients you control | `hvapi configure` offers to generate one, or run `hvapi gen-cert`. Each client must trust the certificate (see [Trusting a self-signed certificate](#trusting-a-self-signed-certificate)). |
| **Reverse proxy that terminates TLS** (IIS with ARR, nginx, Caddy, Traefik) | You already run one, or want central certificate handling | Bind hvapi to `127.0.0.1`, put the proxy's address in `allowed_cidrs`, and let the proxy enforce client access. hvapi then logs the proxy's address as the caller, not the client's. |
| **Plain HTTP** | Only on an isolated management network that nothing untrusted can reach | Leave `tls` empty. hvapi warns in the log at every start. |

In every case, keep the Windows Firewall rule limited to the addresses that need access. The firewall and `allowed_cidrs` decide who can connect; TLS decides whether anyone in between can read it.

## Requirements

- Windows Server 2016 or later with the Hyper-V role (Windows 10/11 with Hyper-V also works). hvapi reads the `root\virtualization\v2` WMI namespace.
- An elevated prompt to run it interactively, or a service account in **Hyper-V Administrators**.
- Nothing else: no runtime, no agent, no PowerShell.

## Quick start

1. Download `hvapi-<version>-windows-amd64.zip` from the latest [release](../../releases/latest) and check it against `SHA256SUMS.txt` if you like:

   ```powershell
   Get-FileHash .\hvapi.exe -Algorithm SHA256
   ```

2. Unpack it into a folder on the Hyper-V host, for example `C:\Program Files\hvapi\`.
3. Create `config.json` with the guided setup. It explains every setting, suggests values, can generate the API key, and sets up HTTPS (your own certificate or a self-signed one):

   ```powershell
   .\hvapi.exe configure
   ```

   Give the **key** it prints to your monitoring client; only the hash is stored. (To do it by hand instead, copy `config.example.json` to `config.json`, run `.\hvapi.exe hash-key` and paste the hash in.)

4. Check the result. This prints every setting with its meaning and reports anything invalid:

   ```powershell
   .\hvapi.exe show-config
   ```

   | Field | Meaning |
   | --- | --- |
   | `listen` | IP and port to listen on. Use the management NIC's IP, not `0.0.0.0`. |
   | `api_keys` | One entry per client: an `id` (shown in logs) and the `sha256` hash. |
   | `allowed_cidrs` | Address ranges or single IPs allowed to connect. |
   | `rate_limit` | Requests per second and burst size, per key. |
   | `log_file` | Log path. Relative paths sit next to `config.json`. |
   | `log_rotate` | `max_mb` at which the log is rotated and `keep`, the number of old logs retained, so the log never grows without limit. |
   | `poll` | How often the cache is refreshed: `state_seconds` for VM state and load, `heavy_seconds` for configuration. |
   | `events` | `hours` of Hyper-V event logs to cache and `max_per_log`. |
   | `diagnostics` | Default thresholds for `/diagnostics`: `checkpoint_days` and `free_space_percent`. |
   | `tls` | `cert_file` and `key_file` (PEM, relative to `config.json`) turn on HTTPS; `min_version` is `1.2` or `1.3`. Empty means plain HTTP. |

5. Test it from an **elevated** console (Run as administrator), because reading Hyper-V data needs it:

   ```powershell
   .\hvapi.exe run
   ```

   If the prompt is not elevated, `run` stops with a message saying so instead of serving empty data. `run -force` starts anyway and lists the problem under `warnings` in `/status`.

6. Install and start the service (Administrator PowerShell):

   ```powershell
   .\hvapi.exe install
   sc.exe start hvapi
   ```

7. Open the port in Windows Firewall for your monitoring hosts only. This and `allowed_cidrs` are your access control; TLS is your confidentiality:

   ```powershell
   New-NetFirewallRule -DisplayName "hvapi" -Direction Inbound -Protocol TCP `
     -LocalPort 8085 -RemoteAddress 10.0.0.0/24 -Action Allow
   ```

## Endpoints

All endpoints are `GET` and need the API key except `/health`, `/docs` and `/openapi.json`. VM endpoints are served from a cache that a background poller refreshes (`poll.state_seconds` for state and load, `poll.heavy_seconds` for configuration), so a request never waits on WMI. `{id}` is the VM's GUID or its display name.

| Endpoint | What it returns |
| --- | --- |
| `/health` | Liveness, no key needed |
| `/version` | Build information |
| `/status` | What the poller is doing and which WMI queries failed. Check this first when something looks empty. |
| `/vms` | Every VM: state, health, uptime, generation, replication |
| `/vms/summary` | The Hyper-V Manager view: load, memory, heartbeat, guest OS, checkpoints |
| `/vms/{id}` | Everything below in one response |
| `/vms/{id}/summary` | One VM's summary |
| `/vms/{id}/config` | Generation, version, file paths, start/stop actions, boot settings |
| `/vms/{id}/processor` | vCPU count, reserve, limit, weight, compatibility, per-vCPU load |
| `/vms/{id}/memory` | Startup/min/max, dynamic memory, assigned, demand and pressure |
| `/vms/{id}/disks` | Controllers, drives and the VHD, ISO or physical disk in each |
| `/vms/{id}/network` | Adapters, MAC, switch, VLAN, guest addresses |
| `/vms/{id}/integration` | Each integration service: enabled in settings, status from the guest |
| `/vms/{id}/kvp` | Key-value pair data: guest intrinsic, guest-set, host-set |
| `/vms/{id}/checkpoints` | Checkpoint tree with age, type, current marker and differencing disk sizes |
| `/vms/{id}/disks/{diskId}/state` | **On demand:** VHD type, format, sizes, fragmentation and the full parent chain, `chain_broken` when a parent is missing |
| `/vms/{id}/replication` | Hyper-V Replica state, health, last replication, replica server and settings |
| `/vms/{id}/devices` | GPU partitions, PCI pass-through, COM ports, 3D display, Fibre Channel, DVD drives |
| `/vms/{id}/security` | Secure Boot and template, TPM, shielding, encryption settings |
| `/vms/{id}/metering` | Resource metering values when enabled |
| `/vms/{id}/thumbnail` | **On demand:** PNG of the console (`?width=&height=`), running VMs only |
| `/vms/{id}/perf` | Per-vCPU run time, virtual NIC throughput and drops, virtual disk throughput and queue, while running |
| `/jobs` | Running and recent Hyper-V jobs with `failed` and `stuck` flags |
| `/events` | Hyper-V event logs from the last `events.hours`, newest first; `?level=&hours=&limit=&id=&vm=` |
| `/events/{log}` | One log: `vmms`, `worker`, `hypervisor`, `compute`, `vmswitch` or `high-availability` |
| `/diagnostics` | Findings from every rule (below), most severe first, with `status` ok, warning or critical |
| `/metrics` | Prometheus text format: VM, host, volume, service, job, event and diagnostics gauges |
| `/wmi/classes` | The classes `/wmi/{class}` may read |
| `/wmi/{class}` | **On demand:** `SELECT * FROM` an allowlisted class, raw rows (`?limit=`) |
| `/host` | Hardware, OS, memory, processors, Hyper-V defaults, VM counts |
| `/host/services` | vmms, vmcompute and hvhost state; `all_running` to alert on |
| `/host/storage` | Volumes and cluster shared volumes with free space, health and which VMs live there |
| `/host/network` | Physical adapters, link state and speed, switch binding |
| `/host/perf` | Hypervisor CPU usage, partitions, memory, balancer pressure, host NICs, disks and switch throughput |
| `/host/numa` | NUMA nodes with processors and memory |
| `/host/hypervisor` | Hypervisor presence, build, scheduler type, VBS status |
| `/host/migration` | Live migration settings and networks |
| `/host/cluster` | Cluster name, nodes, roles and resources, or `clustered: false` |
| `/switches` | Virtual switches: type, bound adapters, teaming, extensions |
| `/switches/{id}` | One switch with its ports |
| `/switches/{id}/ports` | Host-side and VM ports with VLAN settings |

Data that a host does not have (for example dynamic memory counters, or guest details when integration services are off) is left out and explained in a `notes` array rather than failing the request. A `503` with `Retry-After` means the poller has not completed its first run yet.

The two **on demand** endpoints are the only ones that touch the host when called. They use the read-only WMI methods `GetVirtualHardDiskSettingData`, `GetVirtualHardDiskState` and `GetVirtualSystemThumbnailImage`, with a 20 second timeout (`504` when exceeded), and disk results are cached for 30 seconds. Everything else is served from the cache.

## Diagnostics and monitoring

`/diagnostics` applies these rules to the cache and returns findings, each with a stable `rule` name for alert routing:

| Rule | Severity | Means |
| --- | --- | --- |
| `services_stopped` | critical | vmms, vmcompute or hvhost is not running |
| `vm_health` | warning or critical | Hyper-V reports the VM as degraded or failed |
| `heartbeat_missing` | warning | A running VM's guest heartbeat is No Contact, Lost Communication or Error |
| `integration_disabled` | info | Heartbeat or data exchange is disabled on a running VM |
| `adapter_disconnected` | info | A running VM has an adapter not connected to a switch |
| `replication_health` | warning or critical | Hyper-V Replica reports Warning or Critical |
| `checkpoint_old` | warning | A checkpoint is older than `diagnostics.checkpoint_days` (default 7) |
| `checkpoint_disk_missing` | critical | A checkpoint's differencing disk cannot be read: the chain may be broken |
| `vm_disk_missing` | warning | A VM's virtual hard disk cannot be read by the service account |
| `volume_low_space` | warning or critical | A volume holding VM files is below `diagnostics.free_space_percent` (default 10; critical below half) |
| `volume_health` | warning | A VM volume is not Healthy |
| `job_failed`, `job_stuck` | warning | A Hyper-V job failed, or has run for over an hour |
| `host_memory_pressure` | warning | The dynamic memory balancer is at or above 100% |
| `host_cpu_high` | warning | Hypervisor logical processors are 90% busy or more |
| `cluster_node_down`, `cluster_resource_failed` | warning, critical | A cluster node is not Up, or a resource has failed |
| `event_errors` | warning or critical | Error (or critical) events in a Hyper-V log within `events.hours` |

Override the two thresholds per request with `?checkpoint_days=` and `?free_space_percent=`.

**Prometheus** scrapes `/metrics`. The API key can be sent as a bearer token, which is the standard way:

```yaml
scrape_configs:
  - job_name: hvapi
    scheme: https
    authorization:
      credentials: <your key>
    static_configs:
      - targets: ["10.0.0.10:8085"]
    # With a self-signed certificate: tls_config: { ca_file: hvapi-cert.pem }
```

`hvapi_diagnostics_status` (0 ok, 1 warning, 2 critical) is the single number to alert on; the rest let you graph the rest.

**Raw WMI.** `/wmi/{class}` returns a class's instances exactly as WMI reports them, for anything the shaped endpoints do not cover. Only classes in `/wmi/classes` are allowed and only `SELECT *` is ever run.

## API documentation

The running service documents itself:

| URL | What it is |
| --- | --- |
| `http://<listen>/docs` | Interactive Swagger UI. Click **Authorize**, paste your API key, and try any request. |
| `http://<listen>/openapi.json` | The OpenAPI 3.0 document, for client generators and other tools. |

Both need no API key, but the IP allowlist applies. The document is generated from the same route table that serves the API, so it always matches the build. `hvapi openapi -o openapi.json` writes the same document from the command line, and every CI build attaches it to the artifacts and releases. The `/docs` page loads Swagger UI from a CDN, so the browser (not the host) needs internet access; `openapi.json` works without it.

## Calling the API

```powershell
Invoke-RestMethod https://10.0.0.10:8085/health
Invoke-RestMethod https://10.0.0.10:8085/version -Headers @{ "X-API-Key" = "<your key>" }
```

```bash
curl -H "X-API-Key: <your key>" https://10.0.0.10:8085/version
```

`/health` reports `tls` and `tls_expires`, so a monitor can warn before the certificate runs out.

| Code | Meaning |
| --- | --- |
| `401` | Missing or wrong API key (`X-API-Key` header, or `Authorization: Bearer <key>`) |
| `403` | Caller's IP is not in `allowed_cidrs` |
| `405` | Anything other than `GET` |
| `429` | Rate limit exceeded |

## Service account

`hvapi install` registers the service as LocalSystem. A dedicated account is better:

```powershell
sc.exe config hvapi obj= "DOMAIN\svc-hvapi" password= "..."
```

The account needs to read Hyper-V data: membership of **Hyper-V Administrators**, plus **Event Log Readers** for `/events`. If `/status` shows access denied errors, or `/events` lists a `read failed` error for every log, this is the first thing to check.

## Testing an installation

`Test-Hvapi.ps1` ships in the release zip. It reads the endpoint list from the running service, calls every endpoint with real parameters taken from the host (a VM, one of its disks, a switch), checks that a wrong key, a missing key and a POST are rejected, and prints a table and a summary. It tests the data, not only the connection: every JSON response is checked three ways.

- **Schema.** Against the service's own OpenAPI document: every field the API marks required is present and not null, every value has the documented type, timestamps parse. The service guarantees that a required field is never null (a test in the Go code enforces it), so a null here is a defect.
- **Plausibility.** Rules for values that cannot be right on a working host: a running VM with 0 processors or 0 MB assigned, a volume with more free space than size, a host with no logical processors, an unknown state name, a MAC address that is not one, a poller source that is required but returned no rows.
- **Consistency.** Figures that must agree across endpoints: VM counts between `/vms`, `/vms/summary`, `/host` and `/metrics`; the chosen VM's state, memory and processor count between its endpoints.

Each problem is printed under its endpoint, and any problem makes the endpoint FAIL. `-SelfTest` runs the validators against built-in samples, one of them a VM with a null name and 0 MB of memory, and shows what gets caught.

```powershell
.\Test-Hvapi.ps1 -Uri https://10.0.0.10:8085 -ApiKey <your key>
.\Test-Hvapi.ps1 -Uri https://10.0.0.10:8085 -ApiKey <your key> -SkipCertificateCheck   # self-signed certificate
.\Test-Hvapi.ps1 -Uri https://10.0.0.10:8085 -ApiKey <your key> -VM "Web Server" -Json  # pick the VM, machine-readable output
```

| Result | Meaning |
| --- | --- |
| `PASS` | 200 with data |
| `WARN` | 200 but something was missing: a `notes` entry, an empty list, or an expected condition such as a thumbnail of a VM that is off |
| `FAIL` | Error status, timeout, unreadable response, or any data problem (listed under the endpoint) |
| `SKIP` | Nothing on the host to test it with (no VM, no VHD, no switch) |

The exit code is 1 when anything failed, 2 when the service could not be reached. It waits and retries when it hits the per-key rate limit, so it is safe with the default limits. Works in Windows PowerShell 5.1 and PowerShell 7.

## Troubleshooting

**Empty `/vms` or notes everywhere.** Open `/status`. Its `warnings` say when the start-up check failed, and each source shows its row count and error. "Access denied" means the account cannot read Hyper-V WMI: use an elevated prompt for `run`, or for the service put its account in Hyper-V Administrators. "Namespace not found" means the Hyper-V role is not installed.

**Trusting a self-signed certificate.** <a id="trusting-a-self-signed-certificate"></a> Copy `hvapi-cert.pem` from the host to each client. On Windows, import it into the machine's trusted roots from an elevated prompt:

   ```powershell
   Import-Certificate -FilePath .\hvapi-cert.pem -CertStoreLocation Cert:\LocalMachine\Root
   ```

   With curl use `--cacert hvapi-cert.pem`; with PowerShell 7 you can also pass `-SkipCertificateCheck` for a quick test, but not for monitoring, since that disables the protection. `hvapi show-config` prints the certificate's SHA-256 fingerprint for comparison. The certificate must list the name or IP you connect with; regenerate with `hvapi gen-cert -san <names>` if it does not.

**Using a certificate from your CA.** hvapi reads PEM files. If you have a `.pfx`, split it with OpenSSL (included with Git for Windows):

   ```powershell
   openssl pkcs12 -in hvapi.pfx -clcerts -nokeys -out hvapi-cert.pem
   openssl pkcs12 -in hvapi.pfx -nocerts -nodes -out hvapi-key.pem
   ```

   Include intermediates in `hvapi-cert.pem` after the server certificate. Protect the key file: only the service account needs to read it. When you renew, overwrite the two files and hvapi picks them up within 30 seconds.

**Checking what a host exposes.** `/status` should show `rows` greater than zero and no `error` for every source; `dynamic_memory_counters` may legitimately be missing on some hosts. For anything that looks wrong, query the class directly and share the output:

   ```powershell
   .\hvapi.exe wmi "SELECT * FROM Msvm_ComputerSystem WHERE Caption = 'Virtual Machine'"
   .\hvapi.exe wmi -ns root\cimv2 "SELECT * FROM Win32_PerfFormattedData_BalancerStats_HyperVDynamicMemoryVM"
   ```

   The `wmi` command uses the same read-only client as the service and accepts only `SELECT`, `ASSOCIATORS OF` and `REFERENCES OF`.

## Commands

```
hvapi run [-config path]          Run in the console (Ctrl+C to stop)
hvapi configure [-config path]    Build or edit config.json with a guided walk-through
hvapi show-config [-config path]  Print the config with every setting explained
hvapi gen-cert [options]          Generate a self-signed certificate for HTTPS
hvapi hash-key                    Generate a new API key and its hash
hvapi install [-config path]      Install as a Windows service
hvapi uninstall                   Stop and remove the service
hvapi wmi [-ns namespace] <WQL>   Run a read-only WMI query and print the rows (Windows)
hvapi openapi [-o file]           Print the OpenAPI document for this build
hvapi version                     Print build information
hvapi help [command]              Show help, or details of one command
```

`configure`, `show-config`, `gen-cert`, `hash-key` and `openapi` also run on Linux and macOS, so a config can be prepared on a workstation and copied to the host.

## Building and releasing

Two GitHub Actions workflows:

- **ci** runs on every push and pull request: module tidiness, `gofmt`, `go vet` for Linux and Windows, tests, and a development build of `hvapi.exe` attached to the run as an artifact.
- **release** publishes a GitHub release with `hvapi.exe`, a zip, `config.example.json`, `openapi.json` and `SHA256SUMS.txt`. Start it from the **Actions** tab (release, Run workflow, enter a version such as `v0.3.0`; the tag is created for you), or push a tag. A version with a suffix such as `v0.3.0-beta.1` becomes a pre-release. The release notes are taken from the matching section of `CHANGELOG.md`, so add that section first.

To build locally (Go 1.22 or later, on any OS):

```bash
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o hvapi.exe ./cmd/hvapi
```

## Licence

[MIT](LICENSE).
