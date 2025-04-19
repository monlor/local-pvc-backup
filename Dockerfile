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
FROM scratch

# Copy SSL certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /local-pvc-backup /local-pvc-backup
COPY --from=restic/restic:0.17.3 /usr/bin/restic /usr/bin/restic

ENTRYPOINT ["/local-pvc-backup"]

CMD ["run"]