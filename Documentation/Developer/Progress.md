# Current checkpoint

**Updated:** 2026-09-28

**Phase:** 2 — Direct Docker management

The Linux `arm64` image runs on Raspberry Pi 5. The user completed administrator setup and confirmed that the live dashboard discovers 13 projects, including NoX Yard, with CPU, memory, health, and uptime. Memory accounting was initially disabled on the Pi host and is now enabled. Container inspection, lifecycle actions, logs, terminal, image pulls, and confirmed removal are implemented. The user verified start, stop, restart, and image pulls (including private images on their host), and reported that logs and terminal work in tested containers. Some containers lack `/bin/sh`; the UI now detects this and disables the terminal when known unavailable. The new availability behavior still needs verification against the Pi host.

Remaining Phase 2 validation includes removal behavior, disconnected and stale Docker targets, partial group failures, and a standalone container. The phase checkpoint requires managing an existing Compose project and a standalone container without their Compose files.

Before closing Phase 1, confirm that account data survives a container restart, verify local password recovery on the Pi, and validate the Linux `amd64` image.
