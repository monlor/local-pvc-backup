package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monlor/local-pvc-backup/pkg/config"
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
}

// NewClient creates a new Kubernetes client
func NewClient() (*Client, error) {
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

	// Use map to deduplicate PVCs
	pvcMap := make(map[string]PVCInfo)

	for _, pod := range pods.Items {
		// Get backup config from pod annotations
		cfg := getBackupConfig(pod.Annotations)
		if !cfg.Enabled {
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

			// Check if PVC path exists in storage
			pvcPath := fmt.Sprintf("pvc-%s_%s_%s", pvcName, pod.Namespace, volume.Name)
			fullPath := filepath.Join("/data", pvcPath)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				continue
			}

			pvcMap[key] = PVCInfo{
				Name:      pvcName,
				Namespace: pod.Namespace,
				Path:      pvcPath,
				Config:    cfg,
			}
		}
	}

	// Convert map to slice
	var pvcs []PVCInfo
	for _, pvc := range pvcMap {
		pvcs = append(pvcs, pvc)
	}

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
