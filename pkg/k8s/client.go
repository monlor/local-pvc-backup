package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client represents a Kubernetes client wrapper
type Client struct {
	clientset *kubernetes.Clientset
	config    *rest.Config
	log       *logrus.Logger
}

// NewClient creates a new Kubernetes client
func NewClient(cfg *config.Config, log *logrus.Logger) (*Client, error) {
	var k8sConfig *rest.Config
	var err error

	// Try in-cluster config first
	k8sConfig, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create k8s config: %v", err)
		}
	}

	// Configure rate limiting to avoid nil pointer issues
	if k8sConfig.RateLimiter == nil {
		k8sConfig.QPS = 50
		k8sConfig.Burst = 100
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %v", err)
	}

	return &Client{
		clientset: clientset,
		config:    k8sConfig,
		log:       log,
	}, nil
}

// GetNodeName returns the current node name by reading from environment variable
// This is set by the DaemonSet via downward API
func (c *Client) GetNodeName() string {
	// Try to get from environment variable first (set by DaemonSet)
	if nodeName := os.Getenv("KUBERNETES_NODE_NAME"); nodeName != "" {
		return nodeName
	}
	
	// Fallback: try to get from HOSTNAME (less reliable)
	if hostname := os.Getenv("HOSTNAME"); hostname != "" {
		return hostname
	}
	
	// Last resort: use localhost
	c.log.Warn("Could not determine node name, using 'localhost'")
	return "localhost"
}

// GetClientset returns the Kubernetes clientset
func (c *Client) GetClientset() kubernetes.Interface {
	return c.clientset
}

// GetConfig returns the Kubernetes config
func (c *Client) GetConfig() *rest.Config {
	return c.config
}



// GetBackupConfigFromPVC extracts backup configuration from PVC labels and annotations
func GetBackupConfigFromPVC(labels, annotations map[string]string) config.PVCBackupConfig {
	cfg := config.DefaultPVCBackupConfig()

	// Check enabled flag from labels
	if enabled, ok := labels[config.LabelEnabled]; ok {
		cfg.Enabled = strings.ToLower(enabled) == "true"
	}

	// Check include/exclude patterns from annotations
	if include, ok := annotations[config.AnnotationInclude]; ok {
		cfg.Include = include
	}

	if exclude, ok := annotations[config.AnnotationExclude]; ok {
		cfg.Exclude = exclude
	}

	return cfg
}

