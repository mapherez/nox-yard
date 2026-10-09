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

FROM alpine:3.23 AS runtime-base
LABEL org.opencontainers.image.source="https://github.com/mapherez/nox-yard"
RUN apk add --no-cache ca-certificates tzdata docker-cli-compose
ENV NOX_DATA_DIR=/data \
    NOX_LISTEN_ADDR=:8080
EXPOSE 8080
CMD ["nox-yard"]

FROM runtime-base AS dev-backend
COPY --from=backend /out/nox-yard /usr/local/bin/nox-yard

# CI already compiled/tested these artefacts. BuildKit prunes source build stages.
FROM runtime-base AS runtime-ci
ARG TARGETARCH
COPY .ci-build/bin/${TARGETARCH}/nox-yard /usr/local/bin/nox-yard
COPY .ci-build/web/ /srv/nox-yard/web/
ENV NOX_WEB_DIR=/srv/nox-yard/web

# Keep the default source-build target available for explicit developer builds.
FROM runtime-base AS production
COPY --from=backend /out/nox-yard /usr/local/bin/nox-yard
COPY --from=frontend /src/web/dist /srv/nox-yard/web
ENV NOX_WEB_DIR=/srv/nox-yard/web
