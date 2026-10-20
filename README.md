# hvapi

A lightweight, **read-only** HTTP/JSON API that runs on a Hyper-V host and exposes Hyper-V diagnostic data to other machines.

- One self-contained `hvapi.exe`, nothing to install on the host
- Read-only by design: only `GET` requests, no VM changes of any kind
- HTTPS built in, API key and IP allowlist on every request
- Documents itself: Swagger UI at `/docs`, OpenAPI at `/openapi.json`

> **Status:** 0.3.x covers VM inventory, summary, configuration, processor, memory, disks and network, served over HTTPS. Host, switch, checkpoint, event and performance endpoints are planned; the roadmap is in `CLAUDE.md`. Changes are listed in [CHANGELOG.md](CHANGELOG.md).

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
Invoke-RestMethod https://10.0.0.10:8085/health
Invoke-RestMethod https://10.0.0.10:8085/version -Headers @{ "X-API-Key" = "<your key>" }
```

```bash
curl -H "X-API-Key: <your key>" https://10.0.0.10:8085/version
```

`/health` reports `tls` and `tls_expires`, so a monitor can warn before the certificate runs out.

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

The account needs to read Hyper-V data: membership of **Hyper-V Administrators** (event log endpoints, when they arrive, will also need **Event Log Readers**). If `/status` shows access denied errors, this is the first thing to check.

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
