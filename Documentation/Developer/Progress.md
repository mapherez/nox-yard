# Current checkpoint

**Updated:** 2026-09-24  
**Phase:** 2 — Docker inventory (in progress)

The corrected Linux `arm64` image starts on Raspberry Pi 5. The user completed administrator setup and reached the authenticated dashboard. Read-only Docker inventory, its authenticated API, and project cards are implemented in the working tree. `go test ./...` and `npm run build` pass locally. The image has not been republished; live inventory on the Pi remains to be checked after publication.

Before closing Phase 1, confirm that account data survives a container restart, verify local password recovery on the Pi, and validate the Linux `amd64` image.
