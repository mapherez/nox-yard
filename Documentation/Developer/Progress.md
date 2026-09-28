# Current checkpoint

**Updated:** 2026-09-28

**Phase:** 3 — Managed Compose projects (started)

The Linux `arm64` image runs on Raspberry Pi 5. The user completed administrator setup and confirmed that the live dashboard discovers 13 projects, including NoX Yard, with CPU, memory, health, and uptime. Memory accounting was initially disabled on the Pi host and is now enabled. Container inspection, lifecycle actions, logs, terminal, image pulls, and confirmed removal are implemented. The user verified start, stop, restart, image pulls (including private images on their host), and removal of containers and projects. Logs and terminal work in tested containers. Some containers lack `/bin/sh`; the UI now detects this and disables the terminal when known unavailable. The new availability behavior still needs verification against the Pi host.

The remaining Phase 2 checkpoint is to confirm inspection and management of a standalone container created outside Compose. Docker disconnection, stale targets, and partial group failures remain deferred until they can be tested on a suitable host.

Phase 3 has started with the New Project dashboard button and a right-side source drawer for pasted YAML, uploaded YAML, and public HTTPS URLs. The drawer calls a source-intake API that enforces a 1 MiB limit and public HTTPS URL restrictions. The local development Compose stack now runs Vite alongside the API for live UI testing. Source intake does not yet validate Compose syntax or deploy projects. Next, add Compose validation and interpolation-variable collection, then the confirmation preview and deployment path. No Phase 3 project has been deployed yet.

Before closing Phase 1, confirm that account data survives a container restart, verify local password recovery on the Pi, and validate the Linux `amd64` image.
