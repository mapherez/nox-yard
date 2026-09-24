# Current checkpoint

**Updated:** 2026-09-25  
**Phase:** 2 — Direct Docker management

The Linux `arm64` image runs on Raspberry Pi 5. The user completed administrator setup and confirmed that the live dashboard discovers 13 projects, including NoX Yard, with CPU, memory, health, and uptime. Memory accounting was initially disabled on the Pi host and is now enabled. Read-only inventory is the completed slice of Phase 2; container inspection, logs, and lifecycle controls are next.

Before closing Phase 1, confirm that account data survives a container restart, verify local password recovery on the Pi, and validate the Linux `amd64` image.
