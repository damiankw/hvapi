# hvapi: context for Claude Code

This project was planned in a claude.ai chat. This file carries the decisions over.

## What it is

A lightweight, **read-only** HTTP/JSON API that runs on a Hyper-V host and exposes as much Hyper-V diagnostic data as possible to other machines on the internal network.

## Decisions already made

- **Language:** Go, compiled to a single static Windows exe (`GOOS=windows GOARCH=amd64`, `CGO_ENABLED=0`). Nothing is installed on the host.
- **Read-only, strictly:** GET only. No start, stop, create or change of any kind, ever. The WMI layer may call only `ExecQuery` plus an explicit allowlist of read-only methods (`GetSummaryInformation`, `GetVirtualHardDiskSettingData`, `GetVirtualHardDiskState`, `GetVirtualSystemThumbnailImage`).
- **Data sources:** WMI (`root\virtualization\v2`, `root\cimv2`, `root\Microsoft\Windows\Storage`, `root\StandardCimv2`, `root\MSCluster`), performance counters (via `Win32_PerfFormattedData_*`), and Windows event logs via `wevtapi` (`EvtQuery`). Never spawn PowerShell per request. WMIC is removed from Windows, but WMI itself is fully supported.
- **Architecture:** a background poller fills an in-memory cache; handlers serve from the cache. Only the thumbnail and VHD state calls run on demand, with short timeouts.
- **Security:** plain HTTP for now (internal only, HTTPS deliberately deferred; config reserves a `tls` block). API key in the `X-API-Key` header, stored as SHA-256 hashes, constant-time compare. IP allowlist in the app and Windows Firewall. Per-key rate limit. Requests logged with key ID, never the key.
- **Missing data:** if a class doesn't exist on a host (older OS, no cluster), return an empty result with a note; never fail the whole request.
- **Dependencies:** keep them minimal. Standard library first; `golang.org/x/sys` for Windows APIs; a WMI library (go-ole based: `yusufpapurcu/wmi` or `microsoft/wmi`) to be chosen in phase 2.
- **Build:** GitHub Actions builds `hvapi.exe` on push to `main` and publishes a release on `v*` tags. `go.sum` is committed; CI still runs `go mod tidy` as a safety net.

## Status

**Phase 1 (foundation) is done:** config loading and validation, API key, IP allowlist, rate limit, JSON logging, Windows service install/uninstall/run, `hash-key` command, `/health` (no key) and `/version`. Tests in `internal/server/server_test.go`.

## Build phases

1. Foundation: done.
2. Core VM data: read-only WMI client and class/method allowlist, cache and poller, `/vms`, `/vms/summary`, `/vms/{id}`, and its `config`, `processor`, `memory`, `disks`, `network` endpoints.
3. Host and networking: all `/host/*` endpoints and `/switches/*`.
4. Deep VM detail: `integration`, `kvp`, `checkpoints`, `disks/{diskId}/state`, `replication`, `devices`, `security`, `metering`, `thumbnail`.
5. Activity and performance: `/jobs`, `/events`, `/events/{log}`, host and VM `/perf`.
6. Analysis: `/diagnostics`, `/metrics` (Prometheus), raw `/wmi/classes` and `/wmi/{class}`, `docs/API.md`.

## Endpoint plan and data sources

Service: `/health`, `/version`, `/metrics`, `/diagnostics`.

Host: `/host` (`Win32_ComputerSystem`, `Win32_OperatingSystem`, `Msvm_VirtualSystemManagementServiceSettingData`), `/host/services` (`Win32_Service`: vmms, vmcompute, hvhost), `/host/storage` (`MSFT_Volume`, `MSCluster_ClusterSharedVolume`), `/host/network` (`MSFT_NetAdapter`, `Msvm_ExternalEthernetPort`), `/host/perf` (Hyper-V hypervisor logical processor counters, `PerfOS_Memory`), `/host/numa` (`Msvm_NumaNode`), `/host/hypervisor` (OS build; scheduler type from event ID 2 in the Hyper-V-Hypervisor log), `/host/migration` (`Msvm_VirtualSystemMigrationServiceSettingData`, `Msvm_VirtualSystemMigrationNetworkSettingData`), `/host/cluster` (`MSCluster_Cluster`, `MSCluster_Node`, `MSCluster_Resource`).

VMs: `/vms` (`Msvm_ComputerSystem` where `Caption = 'Virtual Machine'`), `/vms/summary` and `/vms/{id}/summary` (`GetSummaryInformation`), `/vms/{id}`, `/config` (`Msvm_VirtualSystemSettingData`, realized), `/processor` (`Msvm_ProcessorSettingData`), `/memory` (`Msvm_MemorySettingData`, dynamic memory counters), `/disks` (`Msvm_StorageAllocationSettingData`, `Msvm_ResourceAllocationSettingData`), `/disks/{diskId}/state` (`Msvm_ImageManagementService` read methods), `/network` (`Msvm_SyntheticEthernetPortSettingData`, `Msvm_EthernetPortAllocationSettingData`, `Msvm_GuestNetworkAdapterConfiguration`), `/integration` (heartbeat, shutdown, time sync, VSS, KVP, guest service components), `/kvp` (`Msvm_KvpExchangeComponent` guest items), `/checkpoints` (snapshot `Msvm_VirtualSystemSettingData` via `Msvm_SnapshotOfVirtualSystem`, `Msvm_ParentChild`), `/replication` (`Msvm_ReplicationRelationship`, `Msvm_ReplicationSettingData`), `/perf` (virtual processor, virtual NIC, virtual storage counters), `/thumbnail` (`GetVirtualSystemThumbnailImage`, RGB565 to PNG), `/devices` (`Msvm_GpuPartitionSettingData`, `Msvm_PciExpressSettingData`, DVD drives), `/security` (`Msvm_SecuritySettingData`, Secure Boot), `/metering` (`Msvm_AggregationMetricValue`).

Networking, jobs, events: `/switches`, `/switches/{id}`, `/switches/{id}/ports`, `/jobs` (`Msvm_ConcreteJob`, `Msvm_StorageJob`), `/events` and `/events/{log}` (VMMS-Admin, Worker-Admin, Hypervisor-Admin, Compute-Admin, VmSwitch-Operational, High-Availability-Admin).

Raw: `/wmi/classes`, `/wmi/{class}` (allowlisted classes only, `ExecQuery` only).

Diagnostics rules: missing heartbeat on a running VM, broken checkpoint chain, old checkpoints (default 7 days), low free space on VM volumes (default 10%), replication Warning/Critical, failed or stuck jobs, integration services disabled or outdated, core Hyper-V services stopped, error events in the last 24 hours.

Perf counter class names are unconfirmed; verify on a real host before relying on them.

## Open questions

- Licence (MIT suggested).
- Can a non-admin account read everything, or does the service need Hyper-V Administrators?
- Confirm perf counter class names on Server 2019, 2022 and 2025.
