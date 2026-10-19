# hvapi

A lightweight, **read-only** HTTP/JSON API that runs on a Hyper-V host and exposes Hyper-V diagnostic data to other machines.

- One self-contained `hvapi.exe`, nothing to install on the host
- Read-only by design: only `GET` requests, no VM changes of any kind
- API key and IP allowlist on every request

> **Status:** phase 1 (foundation). Only `/health` and `/version` exist so far. The full endpoint plan is in `docs/DESIGN.md`.

## Quick start

1. Download `hvapi.exe` and `config.example.json` from the latest [release](../../releases), or from the build artifacts under [Actions](../../actions).
2. Put both in a folder on the Hyper-V host, for example `C:\Program Files\hvapi\`.
3. Generate an API key:

   ```powershell
   .\hvapi.exe hash-key
   ```

   Give the **key** to your monitoring client. Put the **hash** in the config.

4. Copy `config.example.json` to `config.json` and edit it:

   | Field | Meaning |
   | --- | --- |
   | `listen` | IP and port to listen on. Use the management NIC's IP, not `0.0.0.0`. |
   | `api_keys` | One entry per client: an `id` (shown in logs) and the `sha256` hash. |
   | `allowed_cidrs` | Address ranges or single IPs allowed to connect. |
   | `rate_limit` | Requests per second and burst size, per key. |
   | `log_file` | Log path. Relative paths sit next to `config.json`. |
   | `poll` | Poll intervals in seconds (used from phase 2). |
   | `tls` | Reserved for HTTPS later. Leave empty. |

5. Test it in a console:

   ```powershell
   .\hvapi.exe run
   ```

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

From phase 2 that account needs to read Hyper-V data: membership of **Hyper-V Administrators** (and **Event Log Readers** for event endpoints).

## Commands

```
hvapi run [-config path]       Run in the console (Ctrl+C to stop)
hvapi install [-config path]   Install as a Windows service
hvapi uninstall                Stop and remove the service
hvapi hash-key                 Generate a new API key and its hash
hvapi version                  Print build information
```

## Building

Every push to `main` builds `hvapi.exe` with GitHub Actions; download it from the run's artifacts. Pushing a tag such as `v0.1.0` also publishes a release.

To build locally (Go 1.22 or later, on any OS):

```bash
go mod tidy
GOOS=windows GOARCH=amd64 go build -o hvapi.exe ./cmd/hvapi
```
