# Two-stage: build the SPA with node, the API with Go, then ship one small
# image with the SPA baked in and served by the Go binary.

FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS api
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
WORKDIR /src
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api/ ./
# CGO_ENABLED=0 keeps the pure-Go sqlite driver and produces a static binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build \
      -ldflags="-s -w \
        -X github.com/biswas-dev/lifeai/api/internal/version.Version=${VERSION} \
        -X github.com/biswas-dev/lifeai/api/internal/version.GitCommit=${GIT_COMMIT} \
        -X github.com/biswas-dev/lifeai/api/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/lifeai ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /out/admin ./cmd/admin

FROM alpine:3.20
# poppler-utils supplies pdftotext, which reads the text out of a lab report.
RUN apk add --no-cache ca-certificates tzdata wget poppler-utils && \
    addgroup -g 1001 app && adduser -D -u 1001 -G app app
WORKDIR /app
COPY --from=api /out/lifeai /app/lifeai
COPY --from=api /out/admin /app/admin
COPY --from=web /web/dist /app/web/dist
RUN mkdir -p /data/photos && chown -R app:app /data /app
USER app

ENV PORT=8080 \
    DB_PATH=/data/lifeai.db \
    PHOTOS_DIR=/data/photos \
    FRONTEND_DIST=/app/web/dist

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["/app/lifeai"]
