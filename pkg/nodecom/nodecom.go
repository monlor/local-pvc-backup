package nodecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/rest"
)

// NodeExecutor handles cross-node command execution
type NodeExecutor struct {
	k8sClient    kubernetes.Interface
	config       *rest.Config
	k8sConfig    *config.KubernetesConfig
	log          *logrus.Logger
}

// NewNodeExecutor creates a new node executor
func NewNodeExecutor(k8sClient kubernetes.Interface, config *rest.Config, k8sConfig *config.KubernetesConfig, log *logrus.Logger) *NodeExecutor {
	return &NodeExecutor{
		k8sClient: k8sClient,
		config:    config,
		k8sConfig: k8sConfig,
		log:       log,
	}
}

// NodeCommandRequest represents a command to execute on a node
type NodeCommandRequest struct {
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Namespace string   `json:"namespace,omitempty"`
	PVC       string   `json:"pvc,omitempty"`
}

// NodeCommandResponse represents the response from a node command
type NodeCommandResponse struct {
	NodeName string      `json:"node_name"`
	Success  bool        `json:"success"`
	Output   string      `json:"output,omitempty"`
	Error    string      `json:"error,omitempty"`
	Data     interface{} `json:"data,omitempty"`
}

// ExecuteOnNode executes a command on a specific node
func (ne *NodeExecutor) ExecuteOnNode(ctx context.Context, nodeName string, request NodeCommandRequest) (*NodeCommandResponse, error) {
	ne.log.Debugf("Executing command on node %s: command=%s, args=%v, namespace=%s, pvc=%s", 
		nodeName, request.Command, request.Args, request.Namespace, request.PVC)
	
	// Find the daemon pod on the target node
	pod, err := ne.findDaemonPodOnNode(ctx, nodeName)
	if err != nil {
		ne.log.Errorf("Failed to find daemon pod on node %s: %v", nodeName, err)
		return &NodeCommandResponse{
			NodeName: nodeName,
			Success:  false,
			Error:    fmt.Sprintf("failed to find daemon pod on node %s: %v", nodeName, err),
		}, nil
	}

	ne.log.Debugf("Found daemon pod %s/%s on node %s", pod.Namespace, pod.Name, nodeName)

	// Build the command to execute
	cmdArgs := []string{"/local-pvc-backup", "node-exec"}
	cmdArgs = append(cmdArgs, request.Command)
	if request.Namespace != "" {
		cmdArgs = append(cmdArgs, "--namespace", request.Namespace)
	}
	if request.PVC != "" {
		cmdArgs = append(cmdArgs, "--pvc", request.PVC)
	}
	cmdArgs = append(cmdArgs, request.Args...)

	ne.log.Debugf("Executing command in pod %s/%s: %v", pod.Namespace, pod.Name, cmdArgs)

	// Execute the command
	output, err := ne.execInPod(ctx, pod, cmdArgs)
	if err != nil {
		ne.log.Errorf("Failed to execute command on node %s: %v", nodeName, err)
		return &NodeCommandResponse{
			NodeName: nodeName,
			Success:  false,
			Error:    fmt.Sprintf("failed to execute command on node %s: %v", nodeName, err),
		}, nil
	}

	ne.log.Debugf("Command executed successfully on node %s, output length: %d", nodeName, len(output))

	// Try to parse the output as JSON response
	var response NodeCommandResponse
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		ne.log.Debugf("Output is not JSON, treating as plain text: %s", output)
		// If not JSON, treat as plain text output
		return &NodeCommandResponse{
			NodeName: nodeName,
			Success:  true,
			Output:   output,
		}, nil
	}

	response.NodeName = nodeName
	ne.log.Debugf("Parsed JSON response from node %s: success=%t", nodeName, response.Success)
	return &response, nil
}

// ExecuteOnAllNodes executes a command on all nodes that have daemon pods
func (ne *NodeExecutor) ExecuteOnAllNodes(ctx context.Context, request NodeCommandRequest) ([]*NodeCommandResponse, error) {
	nodes, err := ne.getNodesWithDaemonPods(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes with daemon pods: %v", err)
	}

	ne.log.Debugf("Executing command on %d nodes", len(nodes))

	// Execute commands in parallel
	results := make(chan *NodeCommandResponse, len(nodes))
	
	for _, nodeName := range nodes {
		go func(node string) {
			resp, err := ne.ExecuteOnNode(ctx, node, request)
			if err != nil {
				results <- &NodeCommandResponse{
					NodeName: node,
					Success:  false,
					Error:    err.Error(),
				}
			} else {
				results <- resp
			}
		}(nodeName)
	}

	// Collect results
	var responses []*NodeCommandResponse
	for i := 0; i < len(nodes); i++ {
		responses = append(responses, <-results)
	}

	return responses, nil
}

// ExecuteOnNodesWithPVCs executes a command on nodes that have specific PVCs
func (ne *NodeExecutor) ExecuteOnNodesWithPVCs(ctx context.Context, nodeNames []string, request NodeCommandRequest) ([]*NodeCommandResponse, error) {
	if len(nodeNames) == 0 {
		return []*NodeCommandResponse{}, nil
	}

	ne.log.Debugf("Executing command on %d specified nodes", len(nodeNames))

	// Execute commands in parallel
	results := make(chan *NodeCommandResponse, len(nodeNames))
	
	for _, nodeName := range nodeNames {
		go func(node string) {
			resp, err := ne.ExecuteOnNode(ctx, node, request)
			if err != nil {
				results <- &NodeCommandResponse{
					NodeName: node,
					Success:  false,
					Error:    err.Error(),
				}
			} else {
				results <- resp
			}
		}(nodeName)
	}

	// Collect results
	var responses []*NodeCommandResponse
	for i := 0; i < len(nodeNames); i++ {
		responses = append(responses, <-results)
	}

	return responses, nil
}

// findDaemonPodOnNode finds the daemon pod running on a specific node
func (ne *NodeExecutor) findDaemonPodOnNode(ctx context.Context, nodeName string) (*corev1.Pod, error) {
	// Build the label selector using the configured label name and daemon set name
	labelSelector := fmt.Sprintf("%s=%s", ne.k8sConfig.DaemonSetLabel, ne.k8sConfig.DaemonSetName)
	fieldSelector := fmt.Sprintf("spec.nodeName=%s", nodeName)
	
	ne.log.Debugf("Searching for daemon pod on node %s with labelSelector=%s, fieldSelector=%s, namespace=%s", 
		nodeName, labelSelector, fieldSelector, ne.k8sConfig.PodNamespace)
	
	pods, err := ne.k8sClient.CoreV1().Pods(ne.k8sConfig.PodNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: fieldSelector,
	})
	if err != nil {
		ne.log.Errorf("Failed to list pods: %v", err)
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}

	ne.log.Debugf("Found %d pods on node %s", len(pods.Items), nodeName)
	
	if len(pods.Items) == 0 {
		// Try to get more information about why no pods were found
		allPods, err := ne.k8sClient.CoreV1().Pods(ne.k8sConfig.PodNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			ne.log.Errorf("Failed to list all daemon pods: %v", err)
		} else {
			ne.log.Debugf("Total daemon pods in namespace %s: %d", ne.k8sConfig.PodNamespace, len(allPods.Items))
			for _, pod := range allPods.Items {
				ne.log.Debugf("Pod %s: node=%s, phase=%s, labels=%v", 
					pod.Name, pod.Spec.NodeName, pod.Status.Phase, pod.Labels)
			}
		}
		return nil, fmt.Errorf("no daemon pod found on node %s", nodeName)
	}

	// Find a running pod
	for _, pod := range pods.Items {
		ne.log.Debugf("Checking pod %s: phase=%s, nodeName=%s", 
			pod.Name, pod.Status.Phase, pod.Spec.NodeName)
		if pod.Status.Phase == corev1.PodRunning {
			ne.log.Debugf("Found running daemon pod %s on node %s", pod.Name, nodeName)
			return &pod, nil
		}
	}

	ne.log.Warnf("Found %d pods on node %s but none are running", len(pods.Items), nodeName)
	for _, pod := range pods.Items {
		ne.log.Debugf("Pod %s status: phase=%s, reason=%s, message=%s", 
			pod.Name, pod.Status.Phase, pod.Status.Reason, pod.Status.Message)
	}
	
	return nil, fmt.Errorf("no running daemon pod found on node %s", nodeName)
}

// getNodesWithDaemonPods returns all nodes that have daemon pods
func (ne *NodeExecutor) getNodesWithDaemonPods(ctx context.Context) ([]string, error) {
	// Build the label selector using the configured label name and daemon set name
	labelSelector := fmt.Sprintf("%s=%s", ne.k8sConfig.DaemonSetLabel, ne.k8sConfig.DaemonSetName)
	
	ne.log.Debugf("Searching for daemon pods with labelSelector=%s in namespace %s", 
		labelSelector, ne.k8sConfig.PodNamespace)
	
	pods, err := ne.k8sClient.CoreV1().Pods(ne.k8sConfig.PodNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		ne.log.Errorf("Failed to list daemon pods: %v", err)
		return nil, fmt.Errorf("failed to list daemon pods: %v", err)
	}

	ne.log.Debugf("Found %d total daemon pods", len(pods.Items))
	
	nodeSet := make(map[string]bool)
	for _, pod := range pods.Items {
		ne.log.Debugf("Pod %s: node=%s, phase=%s", pod.Name, pod.Spec.NodeName, pod.Status.Phase)
		if pod.Status.Phase == corev1.PodRunning && pod.Spec.NodeName != "" {
			nodeSet[pod.Spec.NodeName] = true
			ne.log.Debugf("Added node %s to daemon nodes list", pod.Spec.NodeName)
		}
	}

	var nodes []string
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	ne.log.Debugf("Found %d nodes with running daemon pods: %v", len(nodes), nodes)
	return nodes, nil
}

// execInPod executes a command in a pod
func (ne *NodeExecutor) execInPod(ctx context.Context, pod *corev1.Pod, command []string) (string, error) {
	req := ne.k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Command: command,
		Stdout:  true,
		Stderr:  true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(ne.config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	output := stdout.String()
	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("command failed: %v, stderr: %s", err, stderr.String())
		}
		return "", fmt.Errorf("command failed: %v", err)
	}

	if stderr.Len() > 0 {
		ne.log.Debugf("Command stderr: %s", stderr.String())
	}

	return strings.TrimSpace(output), nil
}