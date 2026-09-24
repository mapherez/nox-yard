FROM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/nox-yard ./cmd/nox-yard

FROM alpine:3.23
LABEL org.opencontainers.image.source="https://github.com/mapherez/nox-yard"
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend /out/nox-yard /usr/local/bin/nox-yard
COPY --from=frontend /src/web/dist /srv/nox-yard/web
ENV NOX_DATA_DIR=/data \
    NOX_WEB_DIR=/srv/nox-yard/web \
    NOX_LISTEN_ADDR=:8080
EXPOSE 8080
CMD ["nox-yard"]
