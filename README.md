# Local PVC Backup

A comprehensive Kubernetes DaemonSet service for automatic PVC backup and management with cross-node operations. Features interactive CLI commands for backup status viewing, snapshots management, and intelligent restore operations across all nodes.

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
- Label-based backup configuration for efficient K8s API filtering
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

🌐 **Cross-Node Management**
- Interactive CLI commands for cross-node operations
- Unified filtering parameters (--node, --namespace, --pvc, --all)
- Real-time backup status aggregation from all nodes
- Smart time-based restore across multiple nodes

## Command Structure

The service provides multiple commands for different use cases:

### Core Service Commands

1. **`run`**: Start the backup service (used in DaemonSet)
```bash
local-pvc-backup run
```

2. **`restic`**: Execute restic commands with injected environment variables
```bash
local-pvc-backup restic [restic command]
# Examples:
local-pvc-backup restic snapshots
local-pvc-backup restic -c
local-pvc-backup restic backup /path/to/backup
```

### Interactive Management Commands

3. **`status`**: View PVC backup status across all nodes
```bash
local-pvc-backup status --all
local-pvc-backup status --namespace app
local-pvc-backup status --node worker-1 --namespace db
```

4. **`snapshots`**: List backup snapshots with cross-node aggregation
```bash
local-pvc-backup snapshots --namespace app --limit 10
local-pvc-backup snapshots --pvc data --sort-by time
local-pvc-backup snapshots --all --output table
```

5. **`backup`**: Execute immediate backup operations
```bash
local-pvc-backup backup --namespace app --pvc data
local-pvc-backup backup --all --dry-run
local-pvc-backup backup --node worker-1 --wait
```

6. **`restore`**: Intelligent time-based restore operations
```bash
local-pvc-backup restore --time "2025-07-20 15:30:00" --namespace app --dry-run
local-pvc-backup restore --snapshot abc123def456 --namespace app --pvc data
local-pvc-backup restore --time "2025-07-20 15:30:00" --all
```

### Universal Filter Parameters

All interactive commands support consistent filtering:
- `--node <node-name>`: Filter by specific node
- `--namespace <namespace>`: Filter by Kubernetes namespace  
- `--pvc <pvc-name>`: Filter by PVC name
- `--all`: Include all backup-enabled PVCs across all nodes

## Configuration Format

### PVC Labels (Recommended)

Use labels for efficient K8s API filtering:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-app-data
  labels:
    backup.local-pvc.io/enabled: "true"              # Enable backup for this PVC
spec:
  # ... PVC spec
```

### PVC Annotations (Alternative)

```yaml
apiVersion: v1  
kind: PersistentVolumeClaim
metadata:
  name: my-app-data
  annotations:
    backup.local-pvc.io/enabled: "true"              # Enable backup for this PVC
    backup.local-pvc.io/include: "data,conf"         # Optional: Specify directories/files to backup
    backup.local-pvc.io/exclude: "tmp/*,logs/*.log"  # Optional: Exclude patterns
spec:
  # ... PVC spec
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

### PVC Configuration Examples

1. **MySQL backup with label-based configuration:**
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mysql-data
  namespace: database
  labels:
    backup.local-pvc.io/enabled: "true"
  annotations:
    backup.local-pvc.io/exclude: "tmp/*,*.tmp,*.log,lost+found"
spec:
  # ... PVC spec
```

2. **Redis backup with selective inclusion:**
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: redis-data
  namespace: cache
  labels:
    backup.local-pvc.io/enabled: "true"
  annotations:
    backup.local-pvc.io/include: "data,conf"
    backup.local-pvc.io/exclude: "temp/*,*.log,lost+found"
spec:
  # ... PVC spec
```

### Interactive Management Examples

1. **Check backup status across all nodes:**
```bash
# View all PVC backup status
local-pvc-backup status --all

# Check specific namespace
local-pvc-backup status --namespace database

# Check specific node and namespace
local-pvc-backup status --node worker-1 --namespace app
```

2. **List and manage snapshots:**
```bash
# List recent snapshots for a specific PVC
local-pvc-backup snapshots --namespace database --pvc mysql-data --limit 5

# List all snapshots sorted by time
local-pvc-backup snapshots --all --sort-by time --limit 20

# View snapshots for a specific node
local-pvc-backup snapshots --node worker-2 --output table
```

3. **Execute backup operations:**
```bash
# Backup specific PVC with dry-run
local-pvc-backup backup --namespace database --pvc mysql-data --dry-run

# Backup all PVCs in a namespace
local-pvc-backup backup --namespace app --wait

# Backup all PVCs across all nodes
local-pvc-backup backup --all
```

4. **Intelligent restore operations:**
```bash
# Plan restore to specific time point (dry-run)
local-pvc-backup restore --time "2025-07-20 15:30:00" --namespace database --dry-run

# Execute time-based restore for all PVCs
local-pvc-backup restore --time "2025-07-20 15:30:00" --all

# Restore specific PVC from snapshot ID
local-pvc-backup restore --snapshot abc123def456 --namespace database --pvc mysql-data
```

## How it Works

### Automated Backup Service
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

### Cross-Node Management
1. **Service Discovery**: CLI commands discover target nodes using K8s API
2. **Cross-Node Communication**: Uses K8s exec API to communicate with daemon pods
3. **Intelligent Filtering**: Efficiently filters PVCs using labels and annotations
4. **Data Aggregation**: Collects and aggregates results from multiple nodes
5. **Smart Restore Planning**: Analyzes snapshots across nodes to create optimal restore plans

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
