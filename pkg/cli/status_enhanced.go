package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	cfg "github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/monlor/local-pvc-backup/pkg/discovery"
	"github.com/monlor/local-pvc-backup/pkg/nodecom"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)


// StatusEnhancedOptions holds options for the enhanced status command
type StatusEnhancedOptions struct {
	FilterOptions
	OutputFormat string
}

// NewStatusEnhancedCommand creates the enhanced status command with cross-node communication
func NewStatusEnhancedCommand(k8sClient kubernetes.Interface, restConfig *rest.Config, appConfig *cfg.Config, log *logrus.Logger) *cobra.Command {
	opts := &StatusEnhancedOptions{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show PVC backup status across all nodes",
		Long:  "Display the backup status of PVCs across all nodes using cross-node communication",
		RunE: func(cmd *cobra.Command, args []string) error {
			if k8sClient == nil {
				return fmt.Errorf("kubernetes client not initialized")
			}
			return runStatusEnhanced(cmd.Context(), k8sClient, restConfig, appConfig, opts, log)
		},
	}

	// Add common filter flags
	AddFilterFlags(cmd, &opts.FilterOptions)
	
	// Add status-specific flags
	cmd.Flags().StringVarP(&opts.OutputFormat, "output", "o", "table", "Output format (table, json, yaml)")

	return cmd
}

func runStatusEnhanced(ctx context.Context, k8sClient kubernetes.Interface, restConfig *rest.Config, appConfig *cfg.Config, opts *StatusEnhancedOptions, log *logrus.Logger) error {
	// Ensure we have a valid context
	if ctx == nil {
		ctx = context.Background()
	}

	// Validate filter options
	if err := opts.FilterOptions.Validate(); err != nil {
		return fmt.Errorf("invalid filter options: %v", err)
	}

	log.Debugf("Running enhanced status command with filter: %s", opts.FilterOptions.String())

	// Get DaemonSet configuration from application config
	log.Debugf("Using DaemonSet: name=%s, namespace=%s", appConfig.KubernetesConfig.DaemonSetName, appConfig.KubernetesConfig.PodNamespace)

	// Create node executor for cross-node communication
	nodeExecutor := nodecom.NewNodeExecutor(k8sClient, restConfig, appConfig.KubernetesConfig.DaemonSetName, appConfig.KubernetesConfig.PodNamespace, log)

	// Create discovery client to find nodes with relevant PVCs
	discoveryClient := discovery.NewDiscovery(k8sClient, appConfig.KubernetesConfig.DaemonSetName, appConfig.KubernetesConfig.PodNamespace, appConfig.BackupConfig.StoragePath, log)

	var targetNodes []string
	if opts.FilterOptions.All {
		// Get all nodes with daemon pods
		nodes, err := nodeExecutor.ExecuteOnAllNodes(ctx, nodecom.NodeCommandRequest{
			Command: "status",
		})
		if err != nil {
			return fmt.Errorf("failed to get nodes: %v", err)
		}
		for _, resp := range nodes {
			if resp.Success {
				targetNodes = append(targetNodes, resp.NodeName)
			}
		}
	} else {
		// Get PVCs matching the filter to determine target nodes
		pvcs, err := discoveryClient.GetPVCsByFilter(ctx, discovery.FilterOptions{
			Node:      opts.FilterOptions.Node,
			Namespace: opts.FilterOptions.Namespace,
			PVC:       opts.FilterOptions.PVC,
			All:       opts.FilterOptions.All,
		})
		if err != nil {
			return fmt.Errorf("failed to discover PVCs: %v", err)
		}

		nodeSet := make(map[string]bool)
		for _, pvc := range pvcs {
			nodeSet[pvc.NodeName] = true
		}

		for node := range nodeSet {
			targetNodes = append(targetNodes, node)
		}
	}

	if len(targetNodes) == 0 {
		fmt.Println("No nodes found matching the specified filters.")
		return nil
	}

	log.Debugf("Executing status command on %d nodes", len(targetNodes))

	// Execute status command on target nodes
	responses, err := nodeExecutor.ExecuteOnNodesWithPVCs(ctx, targetNodes, nodecom.NodeCommandRequest{
		Command:   "status",
		Namespace: opts.FilterOptions.Namespace,
		PVC:       opts.FilterOptions.PVC,
	})
	if err != nil {
		return fmt.Errorf("failed to execute cross-node status: %v", err)
	}

	// Aggregate results
	var allPVCs []PVCStatusInfo
	for _, resp := range responses {
		if !resp.Success {
			log.Warnf("Node %s failed: %s", resp.NodeName, resp.Error)
			continue
		}

		// Parse the data
		if resp.Data != nil {
			dataList, ok := resp.Data.([]interface{})
			if !ok {
				log.Warnf("Node %s returned invalid data format", resp.NodeName)
				continue
			}

			for _, item := range dataList {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				pvcInfo := PVCStatusInfo{
					NodeName:      resp.NodeName,
					Namespace:     getString(itemMap, "namespace"),
					PVCName:       getString(itemMap, "pvc_name"),
					UID:           getString(itemMap, "uid"),
					Path:          getString(itemMap, "path"),
					BackupEnabled: getBool(itemMap, "backup_enabled"),
					SnapshotCount: getInt(itemMap, "snapshot_count"),
				}

				// Parse last backup time
				if lastBackupStr := getString(itemMap, "last_backup"); lastBackupStr != "" {
					if t, err := time.Parse(time.RFC3339, lastBackupStr); err == nil {
						pvcInfo.LastBackup = &t
					}
				}

				allPVCs = append(allPVCs, pvcInfo)
			}
		}
	}

	if len(allPVCs) == 0 {
		fmt.Println("No backup-enabled PVCs found matching the specified filters.")
		return nil
	}

	// Sort by node, namespace, PVC name
	sort.Slice(allPVCs, func(i, j int) bool {
		if allPVCs[i].NodeName != allPVCs[j].NodeName {
			return allPVCs[i].NodeName < allPVCs[j].NodeName
		}
		if allPVCs[i].Namespace != allPVCs[j].Namespace {
			return allPVCs[i].Namespace < allPVCs[j].Namespace
		}
		return allPVCs[i].PVCName < allPVCs[j].PVCName
	})

	// Display results based on output format
	switch opts.OutputFormat {
	case "table":
		return displayStatusEnhancedTable(allPVCs)
	case "json":
		return displayStatusEnhancedJSON(allPVCs)
	case "yaml":
		return displayStatusEnhancedYAML(allPVCs)
	default:
		return fmt.Errorf("unsupported output format: %s", opts.OutputFormat)
	}
}

// PVCStatusInfo represents status information for a PVC
type PVCStatusInfo struct {
	NodeName      string
	Namespace     string
	PVCName       string
	UID           string
	Path          string
	LastBackup    *time.Time
	SnapshotCount int
	BackupEnabled bool
}

func displayStatusEnhancedTable(pvcs []PVCStatusInfo) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Print header
	fmt.Fprintln(w, "NODE\tNAMESPACE\tPVC\tLAST BACKUP\tSNAPSHOTS\tSTATUS")

	// Print each PVC
	for _, pvc := range pvcs {
		lastBackup := "Never"
		if pvc.LastBackup != nil {
			lastBackup = pvc.LastBackup.Format("2006-01-02 15:04:05")
		}

		status := "Enabled"
		if !pvc.BackupEnabled {
			status = "Disabled"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
			pvc.NodeName,
			pvc.Namespace,
			pvc.PVCName,
			lastBackup,
			pvc.SnapshotCount,
			status,
		)
	}

	return nil
}

func displayStatusEnhancedJSON(pvcs []PVCStatusInfo) error {
	// TODO: Implement JSON output
	return fmt.Errorf("JSON output not implemented yet")
}

func displayStatusEnhancedYAML(pvcs []PVCStatusInfo) error {
	// TODO: Implement YAML output
	return fmt.Errorf("YAML output not implemented yet")
}

// Helper functions to safely extract values from maps
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}