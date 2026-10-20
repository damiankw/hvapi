# hvapi

A lightweight, **read-only** HTTP/JSON API that runs on a Hyper-V host and exposes Hyper-V diagnostic data to other machines.

- One self-contained `hvapi.exe`, nothing to install on the host
- Read-only by design: only `GET` requests, no VM changes of any kind
- API key and IP allowlist on every request

> **Status:** phase 2 (core VM data). VM inventory, summary, configuration, processor, memory, disks and network are in. Host, switch, checkpoint, event and performance endpoints are still to come; the full plan is in `CLAUDE.md`. Phase 2 has not yet been verified against a real Hyper-V host; see [Verifying on a host](#verifying-on-a-host).

## Quick start

1. Download `hvapi.exe` and `config.example.json` from the latest [release](../../releases), or from the build artifacts under [Actions](../../actions).
2. Put both in a folder on the Hyper-V host, for example `C:\Program Files\hvapi\`.
3. Create `config.json` with the guided setup. It explains every setting, suggests values, and can generate the API key for you:

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
   | `poll` | Poll intervals in seconds (used from phase 2). |
   | `tls` | Reserved for HTTPS later. Leave empty. |

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

7. Open the port in Windows Firewall for your monitoring hosts only:

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

Data that a host does not have (for example dynamic memory counters, or guest details when integration services are off) is left out and explained in a `notes` array rather than failing the request. A `503` with `Retry-After` means the poller has not completed its first run yet.

## API documentation

The running service documents itself:

| URL | What it is |
| --- | --- |
| `http://<listen>/docs` | Interactive Swagger UI. Click **Authorize**, paste your API key, and try any request. |
| `http://<listen>/openapi.json` | The OpenAPI 3.0 document, for client generators and other tools. |

Both need no API key, but the IP allowlist applies. The document is generated from the same route table that serves the API, so it always matches the build. `hvapi openapi -o openapi.json` writes the same document from the command line, and every CI build attaches it to the artifacts and releases. The `/docs` page loads Swagger UI from a CDN, so the browser (not the host) needs internet access; `openapi.json` works without it.

## Calling the API

```powershell
Invoke-RestMethod http://10.0.0.10:8085/health
Invoke-RestMethod http://10.0.0.10:8085/version -Headers @{ "X-API-Key" = "<your key>" }
```

```bash
curl -H "X-API-Key: <your key>" http://10.0.0.10:8085/version
```

| Code | Meaning |
| --- | --- |
| `401` | Missing or wrong API key |
| `403` | Caller's IP is not in `allowed_cidrs` |
| `405` | Anything other than `GET` |
| `429` | Rate limit exceeded |

## Service account

`hvapi install` registers the service as LocalSystem. A dedicated account is better:

```powershell
sc.exe config hvapi obj= "DOMAIN\svc-hvapi" password= "..."
```

The account needs to read Hyper-V data: membership of **Hyper-V Administrators** (and, from phase 5, **Event Log Readers** for event endpoints). If `/status` shows access denied errors, this is the first thing to check.

## Troubleshooting

**Empty `/vms` or notes everywhere.** Open `/status`. Its `warnings` say when the start-up check failed, and each source shows its row count and error. "Access denied" means the account cannot read Hyper-V WMI: use an elevated prompt for `run`, or for the service put its account in Hyper-V Administrators. "Namespace not found" means the Hyper-V role is not installed.

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
hvapi hash-key                    Generate a new API key and its hash
hvapi install [-config path]      Install as a Windows service
hvapi uninstall                   Stop and remove the service
hvapi wmi [-ns namespace] <WQL>   Run a read-only WMI query and print the rows (Windows)
hvapi openapi [-o file]           Print the OpenAPI document for this build
hvapi version                     Print build information
hvapi help [command]              Show help, or details of one command
```

`configure`, `show-config`, `hash-key` and `openapi` also run on Linux and macOS, so a config can be prepared on a workstation and copied to the host.

## Building

Every push to `main` builds `hvapi.exe` with GitHub Actions; download it from the run's artifacts. Pushing a tag such as `v0.1.0` also publishes a release.

To build locally (Go 1.22 or later, on any OS):

```bash
go test ./...
GOOS=windows GOARCH=amd64 go build -o hvapi.exe ./cmd/hvapi
```
