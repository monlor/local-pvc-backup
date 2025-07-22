# Deployment Configuration

This directory contains Kubernetes deployment configurations for the local-pvc-backup service.

## Files

- `rbac.yaml`: ServiceAccount, ClusterRole, and ClusterRoleBinding for required permissions
- `daemonset.yaml`: DaemonSet configuration that runs the backup service on each node
- `kustomization.yaml`: Kustomize configuration with ConfigMap and Secret generators
- `example-pvc.yaml`: Example PVC configurations with backup enabled

## Required Permissions

The service requires the following Kubernetes permissions:

- `pods`: get, list, watch - To discover PVCs and their locations
- `persistentvolumeclaims`: get, list, watch - To manage backup configurations
- `pods/exec`: create - For cross-node communication via kubectl exec
- `pods/log`: get, list - For debugging and log access

## Environment Variables

### Required Configuration (via Secret)

```bash
# S3 storage configuration
S3_ENDPOINT=https://your-s3-endpoint.com
S3_BUCKET=your-backup-bucket
S3_ACCESS_KEY=your-access-key
S3_SECRET_KEY=your-secret-key
S3_REGION=us-east-1
RESTIC_PASSWORD=your-encryption-password
```

### Optional Configuration (via ConfigMap)

```bash
# Backup behavior settings
BACKUP_STORAGE_PATH=/data
BACKUP_LOG_LEVEL=info
BACKUP_INTERVAL=1h
BACKUP_RETENTION=14d
S3_PATH=backups/k8s-cluster
RESTIC_CACHE_DIR=/var/cache/restic

# Cross-node communication (auto-configured)
DISCOVERY_MODE=k8s-api
DAEMONSET_NAME=local-pvc-backup
DAEMONSET_NAMESPACE=default
DAEMONSET_LABEL=app.kubernetes.io/name
```

### Auto-Configured Variables

The following environment variables are automatically set by the DaemonSet:

- `KUBERNETES_NODE_NAME`: Automatically set to the current node name via downward API
- `KUBERNETES_NAMESPACE`: Automatically set to the pod's namespace via downward API

## Quick Start

1. **Update the configuration values in `kustomization.yaml`:**
   
   Edit the `secretGenerator` section with your S3 credentials:
   ```yaml
   secretGenerator:
     - name: local-pvc-backup
       literals:
         - S3_ENDPOINT=https://your-s3-endpoint.com
         - S3_BUCKET=your-backup-bucket
         - S3_ACCESS_KEY=your-access-key
         - S3_SECRET_KEY=your-secret-key
         - S3_REGION=us-east-1
         - RESTIC_PASSWORD=your-encryption-password
   ```

2. **Optionally customize backup settings in `kustomization.yaml`:**
   
   Edit the `configMapGenerator` section:
   ```yaml
   configMapGenerator:
     - name: local-pvc-backup
       literals:
         - BACKUP_STORAGE_PATH=/data
         - BACKUP_LOG_LEVEL=info
         - BACKUP_INTERVAL=6h          # Changed from 1h
         - BACKUP_RETENTION=30d        # Changed from 14d
         - S3_PATH=prod-backups        # Added path prefix
         - RESTIC_CACHE_DIR=/var/cache/restic
   ```

3. **Deploy the service:**
   ```bash
   kubectl apply -k .
   ```

4. **Verify deployment:**
   ```bash
   # Check DaemonSet status
   kubectl get daemonset local-pvc-backup
   
   # Check pod logs
   kubectl logs -l app=local-pvc-backup -f
   
   # Test cross-node commands
   kubectl exec -it $(kubectl get pods -l app=local-pvc-backup -o name | head -1) -- local-pvc-backup status --all
   ```

## PVC Configuration

Enable backup for your PVCs using labels (recommended) or annotations:

### Label-based Configuration (Recommended)
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-app-data
  labels:
    backup.local-pvc.io/enabled: "true"
  annotations:
    backup.local-pvc.io/exclude: "tmp/*,*.log"
spec:
  # ... PVC specification
```

### Annotation-based Configuration (Alternative)
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-app-data
  annotations:
    backup.local-pvc.io/enabled: "true"
    backup.local-pvc.io/include: "data,conf"
    backup.local-pvc.io/exclude: "tmp/*,*.log"
spec:
  # ... PVC specification
```

## Storage Requirements

- **Host Path**: The DaemonSet mounts `/var/lib/rancher/k3s/storage` (K3s default) as `/data`
- **Cache Directory**: Creates `/var/lib/local-pvc-backup/cache` for restic cache
- **Resource Limits**: 500m CPU, 512Mi memory per pod
- **Resource Requests**: 100m CPU, 128Mi memory per pod

## Troubleshooting

1. **Check pod status and logs:**
   ```bash
   kubectl get pods -l app=local-pvc-backup
   kubectl logs -l app=local-pvc-backup --tail=100
   ```

2. **Test cross-node communication:**
   ```bash
   kubectl exec -it <pod-name> -- local-pvc-backup status --all
   ```

3. **Verify RBAC permissions:**
   ```bash
   kubectl auth can-i create pods/exec --as=system:serviceaccount:default:local-pvc-backup
   ```

4. **Check restic repository:**
   ```bash
   kubectl exec -it <pod-name> -- local-pvc-backup restic snapshots
   ```