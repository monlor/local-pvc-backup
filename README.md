# Local PVC Backup

A Kubernetes DaemonSet service that automatically backs up Local-Path PVCs to S3 storage using restic.

## Features

- Runs as a DaemonSet on each node
- Uses restic for efficient and secure backups
- Supports incremental backups
- Encryption support
- Configurable backup retention policies
- Flexible include/exclude patterns
- Supports various workload types:
  - Deployments
  - DaemonSets
  - StatefulSets

## Annotation Format

```yaml
backup.local-pvc.io/enabled: "true"                    # Enable backup for this PVC
backup.local-pvc.io/include-pattern: "data/.*|conf/.*" # Optional: Only backup specific paths
backup.local-pvc.io/exclude-pattern: "*.tmp|*.log"     # Optional: Exclude specific files
```

## Configuration

The service requires the following environment variables:

### S3 Configuration
- `S3_ENDPOINT`: S3 endpoint URL
- `S3_BUCKET`: S3 bucket name
- `S3_ACCESS_KEY`: S3 access key
- `S3_SECRET_KEY`: S3 secret key
- `S3_REGION`: S3 region
- `S3_PATH`: S3 storage path prefix (default: "backups")

### Restic Configuration
- `RESTIC_PASSWORD`: Password for encrypting backups
- `RESTIC_CACHE_PATH`: Cache directory path (default: "/var/cache/restic")

### Backup Configuration
- `BACKUP_STORAGE_PATH`: Local storage path (default: "/data")
- `BACKUP_LOG_LEVEL`: Logging level (default: "info")
- `BACKUP_INTERVAL`: Backup interval (default: "1h")
- `BACKUP_RETENTION`: Retention policy (default: "7d,4w,12m")

## Installation

1. Modify the `deploy/kustomization.yaml` file to set the correct S3 endpoint, bucket, access key, secret key, region, and path.

2. Deploy using kustomize:
```bash
kubectl apply -k deploy/
```

## Usage Example

1. Enable backup for a PVC:
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: example-pvc
  annotations:
    backup.local-pvc.io/enabled: "true"
    backup.local-pvc.io/include-pattern: "data/.*|conf/.*"
    backup.local-pvc.io/exclude-pattern: "*.tmp|*.log"
spec:
  # ... PVC spec
```

## How it Works

1. The service runs as a DaemonSet on each node
2. It monitors PVCs mounted on the node
3. For each PVC with backup enabled:
   - Creates a restic repository in S3 if not exists
   - Performs incremental backups based on include/exclude patterns
   - Maintains backups according to retention policy
4. Each node has its own restic repository to avoid conflicts

## License

MIT
