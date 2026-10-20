# Changelog

Notable changes to hvapi. The format follows [Keep a Changelog](https://keepachangelog.com/) and versions follow [Semantic Versioning](https://semver.org/). Each `## x.y.z` section becomes the release notes for that version.

## 0.7.0 - 2026-10-20

Analysis. This completes the planned feature set.

### Added

- `/diagnostics`: every rule in the README's table run over the cache, findings most severe first, `status` ok/warning/critical, thresholds from the new `diagnostics` config block (`checkpoint_days`, `free_space_percent`) with per-request overrides.
- `/metrics`: Prometheus text format with VM state, CPU, memory, heartbeat, checkpoints and replication health; host CPU, memory, uptime and VM counts; services, volumes, jobs, events, cluster nodes; and the diagnostics status and finding counts.
- `/wmi/classes` and `/wmi/{class}`: raw, read-only access to an allowlist of WMI classes.
- The API key is also accepted as `Authorization: Bearer <key>`, so Prometheus can scrape with its standard configuration.
- `Test-Hvapi.ps1`, included in the release zip: calls every endpoint the running service documents, with parameters taken from the host, runs three security checks, and reports PASS, WARN, FAIL or SKIP per endpoint with a summary and exit code. It validates the data: every response against the OpenAPI schema (required fields never null, correct types), plausibility rules (a running VM with no memory, a volume with free space above its size ...), and consistency across endpoints. `-SelfTest` demonstrates the validators.
- JSON responses never contain null for a field the API marks required: nil lists and maps are encoded as empty, and a test checks every documented route.

## 0.6.0 - 2026-10-20

Activity and performance.

### Added

- `/jobs`: running and recent Hyper-V jobs, including storage jobs, with `failed` and `stuck` flags and counts.
- `/events` and `/events/{log}`: the Hyper-V VMMS, Worker, Hypervisor, Compute, VmSwitch and High Availability logs, read through the Windows Event Log API with rendered messages, cached for the last `events.hours` (default 24) up to `events.max_per_log` (default 500) each. Filter by level, hours, limit, event ID or VM. Needs the Event Log Readers right for the service account.
- `/vms/{id}/perf`: per virtual processor run time, virtual network adapter throughput and drops, virtual disk throughput, queue length, latency and errors.
- `/host/perf` now includes host network interfaces, logical disks (latency, queue, free space) and virtual switch throughput.
- `/host/hypervisor` reports the scheduler type (Classic, Core or Root) from the hypervisor's event log.
- New config block `events` with `hours` and `max_per_log`.

## 0.5.0 - 2026-10-20

Deep VM detail.

### Added

- `/vms/{id}/integration`: every integration service with whether it is enabled and its live status.
- `/vms/{id}/kvp`: guest intrinsic, guest-set and host-set key-value pairs.
- `/vms/{id}/checkpoints`: the checkpoint tree with age, type, the current checkpoint, and each one's differencing disks with sizes and missing-file detection.
- `/vms/{id}/disks/{diskId}/state` (on demand): VHD type, format, maximum and file size, fragmentation, and the full parent chain with `chain_broken` when a parent cannot be opened. Cached for 30 seconds.
- `/vms/{id}/replication`: Hyper-V Replica state, health, last replication times, replica server, interval, recovery points and authentication.
- `/vms/{id}/devices`: GPU partitions, PCI Express devices, COM ports, 3D display controller, virtual Fibre Channel ports, DVD drives.
- `/vms/{id}/security`: Secure Boot and template, virtual TPM, shielding, state and migration traffic encryption.
- `/vms/{id}/metering`: resource metering values when enabled.
- `/vms/{id}/thumbnail` (on demand): PNG screenshot of the console, `?width=&height=`.
- `/vms/{id}` now includes integration, checkpoints, replication, security and devices.
- The OpenAPI document describes binary responses and query parameters.

### Fixed

- Numeric values WMI delivers as strings with a fractional part (real64) were truncated to integers.

## 0.4.0 - 2026-10-20

Host and networking.

### Added

- `/host`: hardware, OS, memory, processors, Hyper-V defaults (paths, MAC range, NUMA spanning) and VM counts.
- `/host/services`: state of vmms, vmcompute and hvhost with an `all_running` flag.
- `/host/storage`: every volume and cluster shared volume with size, free space, health, CSV owner, and which VMs keep files on it.
- `/host/network`: physical and host-side adapters with link state, speed, MTU and the virtual switch they are bound to.
- `/host/perf`: hypervisor logical processor usage, root partition time, partition counts, OS memory counters and dynamic memory balancer pressure.
- `/host/numa`, `/host/hypervisor` (including virtualization-based security status), `/host/migration`, `/host/cluster` (nodes, roles, resources with their VM IDs; `clustered: false` on a standalone host).
- `/switches`, `/switches/{id}`, `/switches/{id}/ports`: switch type, bound adapters, management OS access, embedded teaming, SR-IOV, bandwidth mode, extensions, and every host-side and VM port with VLAN settings.

## 0.3.0 - 2026-10-20

Security release: hvapi can now serve HTTPS itself, for installations that cannot put a reverse proxy in front of it.

### Added

- Built-in HTTPS. Set `tls.cert_file` and `tls.key_file` (PEM) in `config.json`; `tls.min_version` chooses 1.2 (default) or 1.3. Certificate and key are reloaded when the files change, so renewals need no restart. A `Strict-Transport-Security` header is sent over HTTPS.
- `hvapi gen-cert` generates a self-signed certificate for the host's name and addresses. `hvapi configure` has an HTTPS step that uses an existing certificate, generates one, or (with a warning) keeps plain HTTP.
- `hvapi show-config` shows the certificate's subject, names, expiry and fingerprint. `/health` reports `tls` and `tls_expires`.
- Log rotation: `log_rotate.max_mb` (default 10) and `log_rotate.keep` (default 5) bound the log file's size.
- A cap on WMI calls in flight, so a hung WMI service cannot accumulate threads poll after poll.

### Changed

- Starting without TLS logs a warning. The README now leads with how to secure the connection.

## 0.2.0 - 2026-10-20

First release. Covers phases 1 (foundation) and 2 (core VM data) of the plan.

### Added

- Read-only HTTP/JSON API as a single `hvapi.exe` for the Hyper-V host, runnable in a console or as a Windows service.
- API key authentication (`X-API-Key`, stored as SHA-256 hashes), IP allowlist, per-key rate limit, JSON request log that never contains the key.
- Commands: `run`, `configure` (guided setup), `show-config` (annotated dump and validation), `hash-key`, `install`, `uninstall`, `wmi` (read-only query tool), `openapi`, `version`, `help`.
- Start-up check that refuses to serve from an unelevated console when Hyper-V WMI denies access, with advice on what to do; `run -force` to override.
- Endpoints: `/health`, `/version`, `/status`, `/vms`, `/vms/summary`, `/vms/{id}` and its `summary`, `config`, `processor`, `memory`, `disks` and `network`.
- Built-in API documentation: OpenAPI 3.0 at `/openapi.json`, Swagger UI at `/docs`, generated from the route table so it always matches the build.
- Background poller with a fast state loop and a slow configuration loop; handlers serve from the cache. Data missing on a host is reported in `notes`, never as a failure.
- GitHub Actions: CI on every push and pull request; release workflow that tags, builds, packages with checksums and publishes.
