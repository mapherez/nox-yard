FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
ARG TARGETOS
ARG TARGETARCH
ARG BUILD_SHA
ARG BUILD_VERSION
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.buildSHA=${BUILD_SHA} -X main.buildVersion=${BUILD_VERSION}" -o /out/nox-yard ./cmd/nox-yard \
    && go version -m /out/nox-yard | grep -Eq "GOARCH=${TARGETARCH}$"

# Official Cosign v3.1.4 multi-architecture image, fixed by index digest.
FROM ghcr.io/sigstore/cosign/cosign@sha256:0f06154836d2de34f887a5c273506d52ca43edaa1d9e53e6510fa78a48f69c2d AS cosign

FROM alpine:3.23 AS dev-backend
RUN apk add --no-cache ca-certificates tzdata docker-cli-compose
COPY --from=backend /out/nox-yard /usr/local/bin/nox-yard
COPY --from=cosign /ko-app/cosign /usr/local/bin/cosign
COPY third_party/cosign/LICENSE /usr/share/licenses/cosign/LICENSE
ENV NOX_DATA_DIR=/data \
    NOX_LISTEN_ADDR=:8080 \
    NOX_MCP_LISTEN_ADDR=:8081
EXPOSE 8080 8081
CMD ["nox-yard"]

FROM alpine:3.23
LABEL org.opencontainers.image.source="https://github.com/mapherez/nox-yard"
RUN apk add --no-cache ca-certificates tzdata docker-cli-compose
COPY --from=backend /out/nox-yard /usr/local/bin/nox-yard
COPY --from=frontend /src/web/dist /srv/nox-yard/web
COPY --from=cosign /ko-app/cosign /usr/local/bin/cosign
COPY third_party/cosign/LICENSE /usr/share/licenses/cosign/LICENSE
ENV NOX_DATA_DIR=/data \
    NOX_WEB_DIR=/srv/nox-yard/web \
    NOX_LISTEN_ADDR=:8080 \
    NOX_MCP_LISTEN_ADDR=:8081
EXPOSE 8080 8081
CMD ["nox-yard"]
