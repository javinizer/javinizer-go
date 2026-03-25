# syntax=docker/dockerfile:1.7
# ==============================================================================
# Stage 1: Build Frontend
# ==============================================================================
FROM node:20-alpine AS frontend-builder

WORKDIR /frontend

# Copy package files and install dependencies
COPY web/frontend/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --production=false

# Copy frontend source and build
COPY web/frontend/ ./
RUN --mount=type=cache,target=/root/.npm \
    npm run build

# Output: /frontend/build/ contains production static files (via adapter-static)

# ==============================================================================
# Stage 2: Build Go Binary
# ==============================================================================
FROM golang:1.25-alpine AS go-builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache \
    git \
    ca-certificates \
    build-base \
    sqlite-dev

# Copy go module files and download dependencies (cached layer)
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy application source
COPY . .

# Copy built frontend from stage 1
# SvelteKit with adapter-static outputs to build/
COPY --from=frontend-builder /frontend/build ./web/dist

# Build binary with optimizations and version injection
# CGO_ENABLED=1 required for SQLite
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux \
    CGO_CFLAGS="-D_LARGEFILE64_SOURCE" \
    go build \
    -tags sqlite_omit_load_extension \
    -ldflags="-w -s \
    -X github.com/javinizer/javinizer-go/internal/version.Version=${VERSION} \
    -X github.com/javinizer/javinizer-go/internal/version.Commit=${COMMIT} \
    -X github.com/javinizer/javinizer-go/internal/version.BuildDate=${BUILD_DATE}" \
    -o javinizer \
    ./cmd/javinizer

# ==============================================================================
# Stage 3: Runtime
# ==============================================================================
FROM alpine:3.21

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

LABEL maintainer="javinizer@example.com" \
      description="JAV metadata scraper and organizer" \
      org.opencontainers.image.title="Javinizer" \
      org.opencontainers.image.description="JAV metadata scraper and organizer" \
      org.opencontainers.image.source="https://github.com/javinizer/javinizer-go" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      version="${VERSION}"

# Working directory is now /javinizer (app state location)
WORKDIR /javinizer

# Install runtime dependencies including Chromium for browser automation
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    sqlite \
    su-exec \
    wget \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ttf-freefont

# Preserve image defaults for runtime UID/GID selection. The entrypoint still
# prefers PUID/PGID, then falls back to USER_ID/GROUP_ID, and only uses these
# values when no runtime override is provided.
ARG USER_ID=1000
ARG GROUP_ID=1000

# Copy binary to /usr/local/bin for system-wide access
COPY --from=go-builder /build/javinizer /usr/local/bin/javinizer
RUN chmod +x /usr/local/bin/javinizer

# Copy frontend static files
COPY --from=go-builder /build/web/dist /app/web/dist

# Copy API documentation (Swagger/OpenAPI) to /app (avoids volume mount shadowing)
COPY --from=go-builder /build/docs/swagger /app/docs/swagger

# Copy default configuration to /app (not /javinizer, to avoid volume shadowing)
COPY configs/config.yaml.example /app/config/config.yaml.default

# Configure for Docker environment
RUN sed -i 's/^\([[:space:]]*\)host: localhost/\1host: 0.0.0.0/' /app/config/config.yaml.default && \
    sed -i 's/^\([[:space:]]*\)allowed_directories: \[\]/\1allowed_directories: ["\/media"]/' /app/config/config.yaml.default

# Copy entrypoint script
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Create image-managed state directories. Sticky world-writable permissions are
# only a fallback for containers started with an explicit Docker `user:` that
# bypasses the root bootstrap; mounted volumes still rely on host ownership.
RUN mkdir -p /javinizer/logs /javinizer/cache /media && \
    chmod 1777 /javinizer /javinizer/logs /javinizer/cache /media

# Environment variables
ENV JAVINIZER_HOME=/javinizer \
    JAVINIZER_CONFIG=/javinizer/config.yaml \
    JAVINIZER_DB=/javinizer/javinizer.db \
    JAVINIZER_LOG_DIR=/javinizer/logs \
    JAVINIZER_IMAGE_DEFAULT_UID=${USER_ID} \
    JAVINIZER_IMAGE_DEFAULT_GID=${GROUP_ID} \
    CHROME_BIN=/usr/bin/chromium-browser \
    CHROME_PATH=/usr/bin/chromium-browser \
    PATH="/usr/local/bin:${PATH}"

# Expose API/web port
EXPOSE 8080

# Health check endpoint
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --method=GET -O /dev/null http://localhost:8080/health || exit 1

# Entrypoint script to initialize config
ENTRYPOINT ["docker-entrypoint.sh"]

# Run API server (will be passed to entrypoint)
CMD ["javinizer", "api"]
