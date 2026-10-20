# hvapi: context for Claude Code

This project was planned in a claude.ai chat. This file carries the decisions over.

## What it is

A lightweight, **read-only** HTTP/JSON API that runs on a Hyper-V host and exposes as much Hyper-V diagnostic data as possible to other machines on the internal network.

## Decisions already made

- **Language:** Go, compiled to a single static Windows exe (`GOOS=windows GOARCH=amd64`, `CGO_ENABLED=0`). Nothing is installed on the host.
- **Read-only, strictly:** GET only. No start, stop, create or change of any kind, ever. The WMI layer may call only `ExecQuery` plus an explicit allowlist of read-only methods (`GetSummaryInformation`, `GetVirtualHardDiskSettingData`, `GetVirtualHardDiskState`, `GetVirtualSystemThumbnailImage`).
- **Data sources:** WMI (`root\virtualization\v2`, `root\cimv2`, `root\Microsoft\Windows\Storage`, `root\StandardCimv2`, `root\MSCluster`), performance counters (via `Win32_PerfFormattedData_*`), and Windows event logs via `wevtapi` (`EvtQuery`). Never spawn PowerShell per request. WMIC is removed from Windows, but WMI itself is fully supported.
- **Architecture:** a background poller fills an in-memory cache; handlers serve from the cache. Only the thumbnail and VHD state calls run on demand, with short timeouts.
- **Security:** HTTPS built in (phase 3): `tls.cert_file` and `tls.key_file` (PEM, relative to the config) enable it; `internal/tlscert` reloads the files when their mtime changes (checked at most every 30 s, bad replacements ignored) and generates self-signed ECDSA P-256 certificates (`hvapi gen-cert`, and the `configure` wizard). Minimum TLS 1.2, HSTS header when serving TLS, `/health` reports `tls` and `tls_expires`. Plain HTTP still allowed but warned about at start; the README tells users to use TLS or a reverse proxy. No PKCS#12 support (would need a dependency); the README shows the OpenSSL conversion. API key in the `X-API-Key` header, stored as SHA-256 hashes, constant-time compare. IP allowlist in the app and Windows Firewall. Per-key rate limit. Requests logged with key ID, never the key. No `X-Forwarded-For` handling: behind a proxy the proxy's address is the caller.
- **Resource bounds:** the log file rotates by size (`internal/logfile`, `log_rotate.max_mb` and `keep`); the cache is replaced each poll, never appended; WMI calls in flight are capped at `wmi.MaxInFlight` (8) so a hung WMI cannot strand unbounded threads, further calls fail fast with `wmi.ErrBusy`.
- **Missing data:** if a class doesn't exist on a host (older OS, no cluster), return an empty result with a note; never fail the whole request.
- **API documentation:** generated, not hand-written. `internal/server/routes.go` is a route table; each entry both registers the handler and describes the operation. `internal/openapi` turns the table and the Go response types (with `json` and `doc` struct tags) into an OpenAPI 3.0 document. It is served at `/openapi.json` (no key), rendered by Swagger UI at `/docs` (CDN-loaded, pinned version), printed by `hvapi openapi`, and attached to CI artifacts and releases. A test fails if a route is missing from the document. Every new endpoint must be added to the table with a summary, description and a typed response.
- **CLI:** `cmd/hvapi/main.go` builds on every OS; the service package has non-Windows stubs. Commands live in one table (`commands()`), which also drives `hvapi help`. `configure` (guided setup) and `show-config` (annotated dump) are in `internal/setup`; their explanations come from `config.Fields`, the single list of setting descriptions.
- **Dependencies:** keep them minimal. Standard library first; `golang.org/x/sys` for Windows APIs; `github.com/go-ole/go-ole` for COM. No WMI wrapper library: `internal/wmi` is a thin layer over the WMI scripting API (`WbemScripting.SWbemLocator`) so the read-only allowlist sits at one choke point. Rows are `map[string]any` (`wmi.Row`) with typed accessors; WMI returns uint64 and datetime as strings, which `Row.Int` and `hyperv.CIMTime` handle.
- **WMI layer rules:** every call runs on its own OS-thread-locked goroutine with its own COM apartment and a context timeout. Arrays of embedded objects need the custom reader in `safearray_windows.go` because go-ole cannot read them. Outside Windows the client returns `wmi.ErrUnavailable`, so the service runs (empty data, notes) on Linux for development.
- **Collector design:** `internal/hyperv` runs a fixed list of whole-class queries per poll (one query per class, not one per VM) and groups rows by VM in Go. Resource settings are linked to their VM through `Msvm_VirtualSystemSettingDataComponent`; if that returns nothing, by InstanceID prefix with snapshot configurations excluded. `/vms/summary` is built from queryable classes (`Msvm_Processor.LoadPercentage`, `Msvm_Memory`, `Msvm_HeartbeatComponent`, KVP intrinsic items, dynamic memory counters) rather than the `GetSummaryInformation` method, so phase 2 needs no method calls; `wmi.Client.Call` exists with the allowlist for phase 4.
- **Response shape:** every VM response carries `collected_at` and `notes`. A missing optional source is a note, never an error. `503` with `Retry-After` only before the first poll completes. `/status` exposes every source's row count and error for debugging on the host.
- **Build and release:** `.github/workflows/ci.yml` runs on every push and PR (tidy check, gofmt, vet for both targets, tests, dev build artifact). `.github/workflows/release.yml` runs on `v*` tags or manually from the Actions tab with a version input (it then creates the tag); it builds with version ldflags, packages a zip plus `SHA256SUMS.txt`, takes release notes from the matching `## x.y.z` section of `CHANGELOG.md`, and marks suffixed versions (`-beta.1`) as pre-releases. **Every release needs a CHANGELOG.md section first.** CI uses the latest stable Go; `go.mod` states the minimum (1.22). `go.sum` is committed.
- **Licence:** MIT (`LICENSE`).

## Status

**Phase 1 (foundation) is done:** config loading and validation, API key, IP allowlist, rate limit, JSON logging, Windows service install/uninstall/run, `hash-key`, `configure`, `show-config`, `help` and `openapi` commands, `/health` (no key), `/version`, `/openapi.json` and `/docs`. Tests sit next to each package.

**Phase 4 (host and networking) is done (v0.4.0):** `/host`, `/host/services`, `/host/storage` (volumes plus CSVs, with VM file placement by longest mount-point prefix), `/host/network`, `/host/perf`, `/host/numa`, `/host/hypervisor` (scheduler type left for the events phase), `/host/migration`, `/host/cluster`, `/switches`, `/switches/{id}`, `/switches/{id}/ports`. Switch topology comes from the EPASD rows: host-side ports have `HostResource` of class `Msvm_ExternalEthernetPort` or `Msvm_InternalEthernetPort` and an InstanceID starting with the switch GUID; VM ports reference the switch. A missing `root\MSCluster` namespace means "not clustered", not an error. Fixtures in `internal/hyperv/host_test.go`.

**Phase 3 (security) is done (v0.3.0):** built-in HTTPS with certificate reload, `gen-cert`, HTTPS step in `configure`, certificate details in `show-config`, `tls`/`tls_expires` in `/health`, HSTS, log rotation, WMI in-flight cap.

**Phase 2 (core VM data) is done and confirmed on a real Hyper-V host** (`/status` and `/vms/summary` verified by the owner): read-only WMI client with query and method allowlists, cache and two-interval poller, start-up preflight (refuses an unelevated console when WMI denies access; `run -force` overrides; warnings in `/status`), `/status`, `/vms`, `/vms/summary`, `/vms/{id}` and its `summary`, `config`, `processor`, `memory`, `disks`, `network`, plus the `hvapi wmi` command. Tests use recorded-style fixtures (`internal/hyperv/fixtures_test.go`). Released as **v0.2.0**. Items still worth confirming from a host's `/status` output when convenient: the dynamic memory counter class name, the InstanceID layout of snapshot settings (only matters for the fallback linking path), and whether `Msvm_EthernetSwitchPortVlanSettingData` InstanceIDs start with the connection's InstanceID.

## Build phases and versions

Each phase ships as a minor version: phase N is released as v0.N.0, with patch releases (v0.N.1 ...) for fixes in between. Every release needs its `CHANGELOG.md` section.

1. Foundation: done (part of v0.2.0).
2. Core VM data: done (v0.2.0).
3. Security: done (v0.3.0). Built-in TLS, certificate generation and reload, log rotation, WMI call cap.
4. Host and networking: done (v0.4.0).
5. Deep VM detail (v0.5.0): `integration`, `kvp`, `checkpoints`, `disks/{diskId}/state`, `replication`, `devices`, `security`, `metering`, `thumbnail`.
6. Activity and performance (v0.6.0): `/jobs`, `/events`, `/events/{log}`, host and VM `/perf`.
7. Analysis (v0.7.0): `/diagnostics`, `/metrics` (Prometheus), raw `/wmi/classes` and `/wmi/{class}`. (API reference is generated; see the API documentation decision above.)

## Endpoint plan and data sources

Service: `/health`, `/version`, `/metrics`, `/diagnostics`.

Host: `/host` (`Win32_ComputerSystem`, `Win32_OperatingSystem`, `Msvm_VirtualSystemManagementServiceSettingData`), `/host/services` (`Win32_Service`: vmms, vmcompute, hvhost), `/host/storage` (`MSFT_Volume`, `MSCluster_ClusterSharedVolume`), `/host/network` (`MSFT_NetAdapter`, `Msvm_ExternalEthernetPort`), `/host/perf` (Hyper-V hypervisor logical processor counters, `PerfOS_Memory`), `/host/numa` (`Msvm_NumaNode`), `/host/hypervisor` (OS build; scheduler type from event ID 2 in the Hyper-V-Hypervisor log), `/host/migration` (`Msvm_VirtualSystemMigrationServiceSettingData`, `Msvm_VirtualSystemMigrationNetworkSettingData`), `/host/cluster` (`MSCluster_Cluster`, `MSCluster_Node`, `MSCluster_Resource`).

VMs: `/vms` (`Msvm_ComputerSystem` where `Caption = 'Virtual Machine'`), `/vms/summary` and `/vms/{id}/summary` (`GetSummaryInformation`), `/vms/{id}`, `/config` (`Msvm_VirtualSystemSettingData`, realized), `/processor` (`Msvm_ProcessorSettingData`), `/memory` (`Msvm_MemorySettingData`, dynamic memory counters), `/disks` (`Msvm_StorageAllocationSettingData`, `Msvm_ResourceAllocationSettingData`), `/disks/{diskId}/state` (`Msvm_ImageManagementService` read methods), `/network` (`Msvm_SyntheticEthernetPortSettingData`, `Msvm_EthernetPortAllocationSettingData`, `Msvm_GuestNetworkAdapterConfiguration`), `/integration` (heartbeat, shutdown, time sync, VSS, KVP, guest service components), `/kvp` (`Msvm_KvpExchangeComponent` guest items), `/checkpoints` (snapshot `Msvm_VirtualSystemSettingData` via `Msvm_SnapshotOfVirtualSystem`, `Msvm_ParentChild`), `/replication` (`Msvm_ReplicationRelationship`, `Msvm_ReplicationSettingData`), `/perf` (virtual processor, virtual NIC, virtual storage counters), `/thumbnail` (`GetVirtualSystemThumbnailImage`, RGB565 to PNG), `/devices` (`Msvm_GpuPartitionSettingData`, `Msvm_PciExpressSettingData`, DVD drives), `/security` (`Msvm_SecuritySettingData`, Secure Boot), `/metering` (`Msvm_AggregationMetricValue`).

Networking, jobs, events: `/switches`, `/switches/{id}`, `/switches/{id}/ports`, `/jobs` (`Msvm_ConcreteJob`, `Msvm_StorageJob`), `/events` and `/events/{log}` (VMMS-Admin, Worker-Admin, Hypervisor-Admin, Compute-Admin, VmSwitch-Operational, High-Availability-Admin).

Raw: `/wmi/classes`, `/wmi/{class}` (allowlisted classes only, `ExecQuery` only).

Diagnostics rules: missing heartbeat on a running VM, broken checkpoint chain, old checkpoints (default 7 days), low free space on VM volumes (default 10%), replication Warning/Critical, failed or stuck jobs, integration services disabled or outdated, core Hyper-V services stopped, error events in the last 24 hours.

Perf counter class names are unconfirmed; verify on a real host before relying on them.

## Open questions

- Can a non-admin account read everything, or does the service need Hyper-V Administrators? (Confirmed so far: an unelevated prompt gets access denied; elevated works. A plain Hyper-V Administrators member has not been tested.)
- Confirm perf counter class names on Server 2019, 2022 and 2025.
