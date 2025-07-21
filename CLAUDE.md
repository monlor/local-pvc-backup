# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Kubernetes DaemonSet service written in Go that provides comprehensive PVC (Persistent Volume Claim) backup and management capabilities. The service runs as a daemon on each node and offers both automated backup services and interactive CLI commands for cross-node operations.

## Architecture

The codebase follows a modular architecture with the following key packages:

- **`main.go`**: Entry point with Cobra CLI setup and command routing
- **`pkg/backup/`**: Core backup manager implementation
- **`pkg/cli/`**: Interactive CLI commands (status, snapshots, backup, restore)  
- **`pkg/config/`**: Configuration management and environment variable parsing
- **`pkg/discovery/`**: PVC discovery and filtering logic
- **`pkg/k8s/`**: Kubernetes API client wrapper
- **`pkg/restic/`**: Restic backup client interface
- **`pkg/nodecom/`**: Cross-node communication for distributed operations

## Build and Development

### Building the Binary
```bash
go build -o local-pvc-backup main.go
```

### Building Docker Image
The project uses a multi-stage Dockerfile:
```bash
docker build -t local-pvc-backup .
```

The final image is built from scratch and includes:
- The Go binary (`local-pvc-backup`) 
- Restic binary (v0.17.3) for backup operations
- SSL certificates for HTTPS connections

### Deployment
The service is deployed using Kustomize:
```bash
kubectl apply -k deploy/
```

Configuration is managed through:
- ConfigMap for non-sensitive settings (`deploy/kustomization.yaml:14-22`)
- Secret for S3 credentials and passwords (`deploy/kustomization.yaml:24-32`)

## Key Commands and Usage

### Service Commands
- **`run`**: Start the automated backup service (used in DaemonSet)
- **`restic [args]`**: Execute restic commands with injected environment variables

### Interactive Management Commands
All support universal filtering (`--node`, `--namespace`, `--pvc`, `--all`):

- **`status`**: View PVC backup status across all nodes
- **`snapshots`**: List backup snapshots with cross-node aggregation  
- **`backup`**: Execute immediate backup operations
- **`restore`**: Intelligent time-based restore operations

### Internal Commands
- **`node-exec`**: Hidden command for cross-node communication (used internally)

## Configuration System

### PVC Configuration
PVCs are configured for backup using Kubernetes labels and annotations:

**Labels** (preferred for API efficiency):
- `backup.local-pvc.io/enabled: "true"` - Enable backup

**Annotations** (for detailed configuration):
- `backup.local-pvc.io/include: "data,conf"` - Comma-separated paths to include
- `backup.local-pvc.io/exclude: "tmp/*,*.log"` - Restic patterns to exclude

### Environment Variables
The service requires these environment variable categories:
- **S3**: `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION`, `S3_PATH`
- **Restic**: `RESTIC_PASSWORD`, `RESTIC_CACHE_DIR` 
- **Backup**: `BACKUP_STORAGE_PATH`, `BACKUP_LOG_LEVEL`, `BACKUP_INTERVAL`, `BACKUP_RETENTION`

## Cross-Node Architecture

The service implements sophisticated cross-node management:

1. **Service Discovery**: CLI commands discover target nodes using Kubernetes API
2. **Cross-Node Communication**: Uses Kubernetes exec API to communicate with daemon pods  
3. **Data Aggregation**: Collects and aggregates results from multiple nodes
4. **Smart Filtering**: Efficiently filters PVCs using labels and annotations

## Development Notes

### Go Dependencies
- Uses Go 1.21 with key dependencies:
  - `k8s.io/client-go` v0.29.3 for Kubernetes API access
  - `github.com/spf13/cobra` v1.9.1 for CLI framework
  - `github.com/sirupsen/logrus` v1.9.0 for structured logging
  - `github.com/caarlos0/env/v10` for environment variable parsing

### Testing and CI/CD
- GitHub Actions workflow builds and publishes Docker images (`docker-publish.yml`)
- Supports multi-architecture builds (linux/amd64, linux/arm64)
- Uses GitHub Container Registry (ghcr.io) for image storage
- Includes cosign for image signing and security

### Logging
Structured logging is used throughout with configurable log levels. The service uses logrus with timestamp formatting for consistent log output.

### Error Handling
The service implements comprehensive error handling with detailed error messages and appropriate HTTP status codes for API responses. Cross-node operations include error aggregation and reporting.