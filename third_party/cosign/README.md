# Cosign

The Yard runtime includes an unmodified Cosign v3.1.4 binary from the official
`ghcr.io/sigstore/cosign/cosign` multi-architecture image. `Dockerfile` fixes the
image by digest; amd64 and arm64 each copy the matching platform binary.

Cosign is a Sigstore project, copyright The Sigstore Authors, licensed under
the [Apache License 2.0](LICENSE). Its license remains separate from Yard's
project license. The runtime includes this license at
`/usr/share/licenses/cosign/LICENSE`.

Upstream source: https://github.com/sigstore/cosign/tree/v3.1.4
