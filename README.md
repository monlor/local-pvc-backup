# Local PVC Backup

A lightweight and efficient Kubernetes DaemonSet service designed to automatically backup Local-Path Persistent Volume Claims (PVCs) to S3-compatible storage using restic. Perfect for backing up your stateful applications in Kubernetes with minimal configuration.

## ✨ Features

🔄 **Automated Backup**
- Runs as a DaemonSet on each node
- Automatically discovers and backs up PVCs with backup enabled
- Configurable backup intervals

🔒 **Secure & Efficient**
- Powered by restic for efficient incremental backups
- End-to-end encryption for data security
- Deduplication and compression support

⚙️ **Flexible Configuration**
- Simple annotation-based backup configuration
- Supports excluding files/directories using restic patterns
- Configurable backup paths for selective backup

🗑️ **Smart Retention**
- Configurable retention policies
- Automatic cleanup of old backups
- Space-efficient backup storage

💾 **Storage Support**
- Works with any S3-compatible storage
- Supports custom S3 endpoints and regions
- Optional path prefix for better organization

## Command Structure

The service provides two main commands:

1. `run`: Start the backup service (used in DaemonSet)
```bash
local-pvc-backup run
```

2. `restic`: Execute restic commands with injected environment variables
```bash
local-pvc-backup restic [restic command]
# Examples:
local-pvc-backup restic snapshots
local-pvc-backup restic -c
local-pvc-backup restic backup /path/to/backup
```

The `restic` command automatically injects all necessary environment variables from the configuration.

## Annotation Format

```yaml
backup.local-pvc.io/enabled: "true"                  # Enable backup for this PVC
backup.local-pvc.io/include: "data,conf"             # Optional: Specify directories/files to backup (comma-separated paths)
backup.local-pvc.io/exclude: "tmp/*,logs/*.log"      # Optional: Exclude patterns (supports restic's pattern format)
```

## Pattern Format

Only the `exclude` annotation supports restic's pattern format. The `include` annotation is a simple comma-separated list of paths relative to the PVC root.

### Include Format
- Simple comma-separated list of paths
- Each path is relative to the PVC root
- Does not support wildcards or patterns
- Examples:
  - `"data,conf"`: Backs up only the `data` and `conf` directories
  - `"data/mysql,conf/my.cnf"`: Backs up specific paths

### Exclude Format
- Supports restic's pattern format
- Supports wildcards and patterns
- Examples:
  - `"tmp/*"`: Excludes all files in tmp directory
  - `"*.log"`: Excludes all log files
  - `"data/*.tmp"`: Excludes tmp files in data directory
  - `"logs/*.log,temp/*"`: Excludes multiple patterns

If no `include` is specified, the entire PVC will be backed up (subject to exclude patterns).

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
- `RESTIC_CACHE_DIR`: Cache directory path (default: "/var/cache/restic")

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
    backup.local-pvc.io/exclude: "tmp/*,*.tmp,*.log,lost+found"
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
    backup.local-pvc.io/exclude: "temp/*,*.log,lost+found"
spec:
  # ... PVC spec
```

## How it Works

1. The service runs as a DaemonSet on each node
2. It monitors PVCs mounted on the node
3. For each PVC with backup enabled:
   - Creates a restic repository in S3 if not exists
   - Backs up all enabled PVCs in a single restic backup command
   - Applies user-defined exclude patterns for each PVC
   - Performs incremental backups
   - Maintains backups according to retention policy
4. Each node has its own restic repository to avoid conflicts
5. Uses PV name to locate the correct backup directory

## Backup Command Format

The service uses restic's backup command in the following format:
```bash
restic backup \
  --repo s3:endpoint/bucket/path/node-xxx \
  --host node-xxx \
  --exclude "pvc1/tmp/*" \
  --exclude "pvc1/*.log" \
  --exclude "pvc2/temp/*" \
  /data/pvc1 /data/pvc2
```

This approach:
- Backs up multiple PVCs in a single command
- Uses exclude patterns to skip unwanted files
- Performs efficient incremental backups
- Maintains backup history per node

## License

MIT
