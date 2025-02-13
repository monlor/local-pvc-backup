package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client represents a Kubernetes client wrapper
type Client struct {
	clientset *kubernetes.Clientset
	nodeName  string
	log       *logrus.Logger
}

// NewClient creates a new Kubernetes client
func NewClient(log *logrus.Logger) (*Client, error) {
	var config *rest.Config
	var err error

	// Try in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create k8s config: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %v", err)
	}

	// Get current node name from environment
	nodeName := os.Getenv("KUBERNETES_NODE_NAME")
	if nodeName == "" {
		return nil, fmt.Errorf("KUBERNETES_NODE_NAME environment variable not set")
	}

	return &Client{
		clientset: clientset,
		nodeName:  nodeName,
		log:       log,
	}, nil
}

// GetNodeName returns the current node name
func (c *Client) GetNodeName() string {
	return c.nodeName
}

// GetPVCsToBackup returns a list of PVCs that need to be backed up on the current node
func (c *Client) GetPVCsToBackup(ctx context.Context) ([]PVCInfo, error) {
	// Get pods running on this node
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", c.nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %v", c.nodeName, err)
	}

	c.log.Debugf("Found %d pods on node %s", len(pods.Items), c.nodeName)

	// Use map to deduplicate PVCs
	pvcMap := make(map[string]PVCInfo)

	for _, pod := range pods.Items {
		c.log.Debugf("Processing pod %s/%s", pod.Namespace, pod.Name)

		// Get backup config from pod annotations
		cfg := getBackupConfig(pod.Annotations)
		if !cfg.Enabled {
			c.log.Debugf("  - Backup not enabled for pod %s/%s", pod.Namespace, pod.Name)
			continue
		}

		// Process pod volumes
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}

			pvcName := volume.PersistentVolumeClaim.ClaimName
			// Create unique key for PVC
			key := fmt.Sprintf("%s/%s", pod.Namespace, pvcName)

			// Get PVC object
			pvc, err := c.clientset.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err != nil {
				c.log.Errorf("Failed to get PVC %s/%s: %v", pod.Namespace, pvcName, err)
				continue
			}

			// Get PV name from PVC
			if pvc.Spec.VolumeName == "" {
				c.log.Errorf("PVC %s/%s has no volume name", pod.Namespace, pvcName)
				continue
			}

			// Construct the path using PV name
			pvcPath := fmt.Sprintf("%s_%s_%s", pvc.Spec.VolumeName, pod.Namespace, volume.Name)
			fullPath := filepath.Join("/data", pvcPath)

			c.log.Debugf("  - Checking PVC %s", key)
			c.log.Debugf("    - Volume name: %s", volume.Name)
			c.log.Debugf("    - PV name: %s", pvc.Spec.VolumeName)
			c.log.Debugf("    - Full path: %s", fullPath)

			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				c.log.Errorf("PVC %s/%s does not exist on node %s", pod.Namespace, pvcName, c.nodeName)
				continue
			}

			c.log.Debugf("    - Path exists, adding to backup list")

			pvcMap[key] = PVCInfo{
				Name:      pvcName,
				Namespace: pvc.Namespace,
				Path:      fullPath,
				Config:    cfg,
			}
		}
	}

	// Convert map to slice
	var pvcs []PVCInfo
	for _, pvc := range pvcMap {
		pvcs = append(pvcs, pvc)
	}

	c.log.Debugf("Found %d PVCs to backup", len(pvcs))
	return pvcs, nil
}

// PVCInfo contains information about a PVC that needs to be backed up
type PVCInfo struct {
	Name      string
	Namespace string
	Path      string
	Config    config.PVCBackupConfig
}

func getBackupConfig(annotations map[string]string) config.PVCBackupConfig {
	cfg := config.DefaultPVCBackupConfig()

	if enabled, ok := annotations[config.AnnotationEnabled]; ok {
		cfg.Enabled = strings.ToLower(enabled) == "true"
	}

	if includePattern, ok := annotations[config.AnnotationIncludePattern]; ok {
		cfg.IncludePattern = includePattern
	}

	if excludePattern, ok := annotations[config.AnnotationExcludePattern]; ok {
		cfg.ExcludePattern = excludePattern
	}

	return cfg
}
