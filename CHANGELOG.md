# Changelog

Notable changes to hvapi. The format follows [Keep a Changelog](https://keepachangelog.com/) and versions follow [Semantic Versioning](https://semver.org/). Each `## x.y.z` section becomes the release notes for that version.

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
