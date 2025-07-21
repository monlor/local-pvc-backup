package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/monlor/local-pvc-backup/pkg/k8s"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// FilterOptions represents filtering criteria for PVC discovery
type FilterOptions struct {
	Node      string
	Namespace string
	PVC       string
	All       bool
}

// Matches checks if a PVC matches the filter criteria
func (f *FilterOptions) Matches(nodeName, namespace, pvcName string) bool {
	if f.All {
		return true
	}

	// Check node filter
	if f.Node != "" && f.Node != nodeName {
		return false
	}

	// Check namespace filter
	if f.Namespace != "" && f.Namespace != namespace {
		return false
	}

	// Check PVC filter
	if f.PVC != "" && f.PVC != pvcName {
		return false
	}

	return true
}

// String returns a human-readable representation of the filter
func (f *FilterOptions) String() string {
	if f.All {
		return "all backup-enabled PVCs"
	}

	var parts []string
	if f.Node != "" {
		parts = append(parts, fmt.Sprintf("node=%s", f.Node))
	}
	if f.Namespace != "" {
		parts = append(parts, fmt.Sprintf("namespace=%s", f.Namespace))
	}
	if f.PVC != "" {
		parts = append(parts, fmt.Sprintf("pvc=%s", f.PVC))
	}

	if len(parts) == 0 {
		return "no filters"
	}

	return strings.Join(parts, ", ")
}

// GlobalPVCInfo represents PVC information across all nodes
type GlobalPVCInfo struct {
	PVCName       string
	Namespace     string
	NodeName      string
	Path          string
	UID           string
	LastBackup    *time.Time
	SnapshotCount int
	TotalSize     string
	BackupEnabled bool
	Config        config.PVCBackupConfig
}

// Discovery handles PVC discovery across the cluster
type Discovery struct {
	k8sClient         kubernetes.Interface
	daemonSetName     string
	daemonSetNamespace string
	storagePath       string
	log               *logrus.Logger
}

// NewDiscovery creates a new discovery instance
func NewDiscovery(k8sClient kubernetes.Interface, daemonSetName, daemonSetNamespace, storagePath string, log *logrus.Logger) *Discovery {
	return &Discovery{
		k8sClient:         k8sClient,
		daemonSetName:     daemonSetName,
		daemonSetNamespace: daemonSetNamespace,
		storagePath:       storagePath,
		log:               log,
	}
}

// GetPVCsByFilter returns PVCs that match the filter criteria
func (d *Discovery) GetPVCsByFilter(ctx context.Context, filter FilterOptions) ([]GlobalPVCInfo, error) {
	d.log.Debugf("Discovering PVCs with filter: %s", filter.String())

	// Build label selector to only get backup-enabled PVCs
	labelSelector := fmt.Sprintf("%s=true", config.LabelEnabled)
	
	var pvcs []corev1.PersistentVolumeClaim
	
	if filter.Namespace != "" {
		// Get PVCs from specific namespace
		pvcList, err := d.k8sClient.CoreV1().PersistentVolumeClaims(filter.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list PVCs in namespace %s: %v", filter.Namespace, err)
		}
		pvcs = pvcList.Items
	} else {
		// Get PVCs from all namespaces
		pvcList, err := d.k8sClient.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list PVCs: %v", err)
		}
		pvcs = pvcList.Items
	}

	d.log.Debugf("Found %d backup-enabled PVCs", len(pvcs))

	var result []GlobalPVCInfo

	// Process each PVC
	for _, pvc := range pvcs {
		// Apply PVC name filter
		if filter.PVC != "" && filter.PVC != pvc.Name {
			continue
		}

		// Check if PVC has a bound volume
		if pvc.Spec.VolumeName == "" {
			d.log.Debugf("PVC %s/%s has no bound volume, skipping", pvc.Namespace, pvc.Name)
			continue
		}

		// Find which node(s) this PVC is mounted on
		nodeName, err := d.findPVCNode(ctx, &pvc)
		if err != nil {
			d.log.Debugf("Failed to find node for PVC %s/%s: %v", pvc.Namespace, pvc.Name, err)
			continue
		}

		// Apply node filter
		if filter.Node != "" && filter.Node != nodeName {
			continue
		}

		// Apply final filter check
		if !filter.Matches(nodeName, pvc.Namespace, pvc.Name) {
			continue
		}

		// Construct the storage path (same logic as backup)
		pvcPath := fmt.Sprintf("%s_%s_%s", pvc.Spec.VolumeName, pvc.Namespace, pvc.Name)
		fullPath := filepath.Join(d.storagePath, pvcPath)

		// Get backup configuration
		backupConfig := k8s.GetBackupConfigFromPVC(pvc.Labels, pvc.Annotations)

		d.log.Debugf("Found matching PVC %s/%s on node %s", pvc.Namespace, pvc.Name, nodeName)

		result = append(result, GlobalPVCInfo{
			PVCName:       pvc.Name,
			Namespace:     pvc.Namespace,
			NodeName:      nodeName,
			Path:          fullPath,
			UID:           string(pvc.UID),
			BackupEnabled: backupConfig.Enabled,
			Config:        backupConfig,
			// TODO: Get backup status from restic
			LastBackup:    nil,
			SnapshotCount: 0,
			TotalSize:     "Unknown",
		})
	}

	d.log.Debugf("Found %d PVCs matching all filters", len(result))
	return result, nil
}

// findPVCNode finds which node a PVC is mounted on by looking at pods
func (d *Discovery) findPVCNode(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (string, error) {
	// List all pods in the PVC's namespace
	pods, err := d.k8sClient.CoreV1().Pods(pvc.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list pods in namespace %s: %v", pvc.Namespace, err)
	}

	// Find pods that use this PVC
	for _, pod := range pods.Items {
		// Skip pods that are not running or don't have a node assigned
		if pod.Spec.NodeName == "" || pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Check if this pod uses the PVC
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvc.Name {
				// Check if the PVC path exists on this node (if we can check)
				pvcPath := fmt.Sprintf("%s_%s_%s", pvc.Spec.VolumeName, pvc.Namespace, pvc.Name)
				fullPath := filepath.Join(d.storagePath, pvcPath)
				
				// Try to check if path exists (this will work if we're on the same node)
				if _, err := os.Stat(fullPath); err == nil {
					return pod.Spec.NodeName, nil
				}
				
				// If we can't check the file system, return the node name anyway
				// (this is expected when running from a different node)
				return pod.Spec.NodeName, nil
			}
		}
	}

	return "", fmt.Errorf("no running pod found using PVC %s/%s", pvc.Namespace, pvc.Name)
}

// GetNodeNames returns all nodes that have backup-enabled PVCs
func (d *Discovery) GetNodeNames(ctx context.Context) ([]string, error) {
	pvcs, err := d.GetPVCsByFilter(ctx, FilterOptions{All: true})
	if err != nil {
		return nil, err
	}

	nodeSet := make(map[string]bool)
	for _, pvc := range pvcs {
		nodeSet[pvc.NodeName] = true
	}

	var nodes []string
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	return nodes, nil
}