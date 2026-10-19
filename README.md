# hvapi

A lightweight, **read-only** HTTP/JSON API that runs on a Hyper-V host and exposes Hyper-V diagnostic data to other machines.

- One self-contained `hvapi.exe`, nothing to install on the host
- Read-only by design: only `GET` requests, no VM changes of any kind
- API key and IP allowlist on every request

> **Status:** phase 1 (foundation). Only `/health`, `/version` and the built-in API documentation exist so far. The full endpoint plan is in `CLAUDE.md`.

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

From phase 2 that account needs to read Hyper-V data: membership of **Hyper-V Administrators** (and **Event Log Readers** for event endpoints).

## Commands

```
hvapi run [-config path]          Run in the console (Ctrl+C to stop)
hvapi configure [-config path]    Build or edit config.json with a guided walk-through
hvapi show-config [-config path]  Print the config with every setting explained
hvapi hash-key                    Generate a new API key and its hash
hvapi install [-config path]      Install as a Windows service
hvapi uninstall                   Stop and remove the service
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
