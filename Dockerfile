# Build stage
FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS builder

ARG TARGETARCH

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN GOARCH=${TARGETARCH} CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /local-pvc-backup main.go

# Final stage
FROM --platform=$TARGETPLATFORM alpine:3.19

# Install restic and dependencies
RUN apk add --no-cache restic ca-certificates tzdata

WORKDIR /

# Copy binary from builder
COPY --from=builder /local-pvc-backup /local-pvc-backup

# Create directories
RUN mkdir -p /data /var/cache/restic && \
    adduser -D -u 1000 backup && \
    chown -R backup:backup /data /var/cache/restic

# Set timezone to UTC
ENV TZ=UTC \
    RESTIC_CACHE_DIR=/var/cache/restic

USER backup

ENTRYPOINT ["/local-pvc-backup"] 