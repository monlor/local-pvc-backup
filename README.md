# Local PVC Backup

A Kubernetes DaemonSet service that automatically backs up Local-Path PVCs to S3 storage using restic.

## Features

- Runs as a DaemonSet on each node
- Uses restic for efficient and secure backups
- Supports incremental backups
- Encryption support
- Configurable backup retention policies
- Flexible include/exclude patterns using restic's pattern format
- Supports various workload types:
  - Deployments
  - DaemonSets
  - StatefulSets

## Annotation Format

```yaml
backup.local-pvc.io/enabled: "true"                      # Enable backup for this PVC
backup.local-pvc.io/include-pattern: "*.sql,conf/*.cnf"  # Optional: Only backup specific files/paths
backup.local-pvc.io/exclude-pattern: "tmp/*,logs/*.log"  # Optional: Exclude specific files/paths
```

## Configuration

The service requires the following environment variables:

### S3 Configuration
- `S3_ENDPOINT`: S3 endpoint URL
- `S3_BUCKET`: S3 bucket name
- `S3_ACCESS_KEY`: S3 access key
- `S3_SECRET_KEY`: S3 secret key
- `S3_REGION`: S3 region
- `S3_PATH`: S3 storage path prefix (default: "")

### Restic Configuration
- `RESTIC_PASSWORD`: Password for encrypting backups
- `RESTIC_CACHE_PATH`: Cache directory path (default: "/var/cache/restic")

### Backup Configuration
- `BACKUP_STORAGE_PATH`: Local storage path (default: "/data")
- `BACKUP_LOG_LEVEL`: Logging level (default: "info")
- `BACKUP_INTERVAL`: Backup interval (default: "1h")
- `BACKUP_RETENTION`: Retention policy (default: "14d")

## Installation

1. Modify the `deploy/kustomization.yaml` file to set the correct S3 endpoint, bucket, access key, secret key, region, and path.

2. Deploy using kustomize:
```bash
kubectl apply -k deploy/
```

## Usage Examples

1. MySQL backup example:
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mysql-data
  annotations:
    backup.local-pvc.io/enabled: "true"
    backup.local-pvc.io/include-pattern: "*.sql,*.cnf,data/*.ibd"
    backup.local-pvc.io/exclude-pattern: "tmp/*,*.tmp,*.log"
spec:
  # ... PVC spec
```

2. Redis backup example:
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: redis-data
  annotations:
    backup.local-pvc.io/enabled: "true"
    backup.local-pvc.io/include-pattern: "*.rdb,*.aof,*.conf"
    backup.local-pvc.io/exclude-pattern: "temp/*,*.log"
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
