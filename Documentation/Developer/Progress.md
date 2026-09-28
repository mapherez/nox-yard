# Current checkpoint

**Updated:** 2026-09-28

**Phase:** 3 — Managed Compose projects (implementation, host checkpoint pending)

The Linux `arm64` image runs on Raspberry Pi 5. The user completed administrator setup and confirmed that the live dashboard discovers 13 projects, including NoX Yard, with CPU, memory, health, and uptime. Memory accounting was initially disabled on the Pi host and is now enabled. Container inspection, lifecycle actions, logs, terminal, image pulls, and confirmed removal are implemented. The user verified start, stop, restart, image pulls (including private images on their host), and removal of containers and projects. Logs and terminal work in tested containers. Some containers lack `/bin/sh`; the UI now detects this and disables the terminal when known unavailable. The new availability behavior still needs verification against the Pi host.

The remaining Phase 2 checkpoint is to confirm inspection and management of a standalone container created outside Compose. Docker disconnection, stale targets, and partial group failures remain deferred until they can be tested on a suitable host.

Phase 3 now has the New Project button, right-side drawer, source intake for paste/upload/public HTTPS URL, interpolation-variable fields, Compose validation, a confirmation preview, and asynchronous pull/deployment with persisted job status. Managed source and variables are stored in SQLite and managed projects remain visible without containers. The existing project drawer exposes managed start/stop/restart, pull, update/recreate, logs, inspect, terminal, and removal with an explicit volume choice. URL duplicates support copy or sync; external Compose projects can be adopted after comparison. The local development Compose stack runs Vite alongside the API for live UI testing. The Phase 3 host checkpoint remains pending: deploy the same sample stack by all three inputs on the Pi, check invalid input before host mutation, and verify persistence across restart.

Before closing Phase 1, confirm that account data survives a container restart, verify local password recovery on the Pi, and validate the Linux `amd64` image.
