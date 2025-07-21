package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/monlor/local-pvc-backup/pkg/discovery"
	"github.com/monlor/local-pvc-backup/pkg/nodecom"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// BackupOptions holds options for the backup command
type BackupOptions struct {
	FilterOptions
	DryRun bool
	Wait   bool
}

// NewBackupCommand creates the backup command
func NewBackupCommand(k8sClient kubernetes.Interface, config *rest.Config, log *logrus.Logger) *cobra.Command {
	opts := &BackupOptions{}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Trigger immediate backup of PVCs across nodes",
		Long:  "Trigger immediate backup of specified PVCs across all relevant nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if k8sClient == nil {
				return fmt.Errorf("kubernetes client not initialized")
			}
			return runBackup(cmd.Context(), k8sClient, config, opts, log)
		},
	}

	// Add common filter flags
	AddFilterFlags(cmd, &opts.FilterOptions)
	
	// Add backup-specific flags
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be backed up without executing")
	cmd.Flags().BoolVar(&opts.Wait, "wait", true, "Wait for backup operations to complete")

	return cmd
}

func runBackup(ctx context.Context, k8sClient kubernetes.Interface, config *rest.Config, opts *BackupOptions, log *logrus.Logger) error {
	// Validate filter options
	if err := opts.FilterOptions.Validate(); err != nil {
		return fmt.Errorf("invalid filter options: %v", err)
	}

	log.Debugf("Running backup command with filter: %s", opts.FilterOptions.String())

	// Create node executor for cross-node communication
	nodeExecutor := nodecom.NewNodeExecutor(k8sClient, config, "local-pvc-backup", "kube-system", log)

	// Create discovery client to find PVCs and their nodes
	discoveryClient := discovery.NewDiscovery(k8sClient, "local-pvc-backup", "kube-system", "/data", log)

	// Get PVCs matching the filter to determine what needs to be backed up
	pvcs, err := discoveryClient.GetPVCsByFilter(ctx, discovery.FilterOptions{
		Node:      opts.FilterOptions.Node,
		Namespace: opts.FilterOptions.Namespace,
		PVC:       opts.FilterOptions.PVC,
		All:       opts.FilterOptions.All,
	})
	if err != nil {
		return fmt.Errorf("failed to discover PVCs: %v", err)
	}

	if len(pvcs) == 0 {
		fmt.Println("No backup-enabled PVCs found matching the specified filters.")
		return nil
	}

	// Group PVCs by node
	nodeGroups := make(map[string][]discovery.GlobalPVCInfo)
	for _, pvc := range pvcs {
		nodeGroups[pvc.NodeName] = append(nodeGroups[pvc.NodeName], pvc)
	}

	fmt.Printf("Found %d PVCs to backup across %d nodes\n", len(pvcs), len(nodeGroups))

	// Show backup plan
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NODE\tNAMESPACE\tPVC\tSTATUS")

	var backupPlan []BackupPlanItem
	for nodeName, nodePVCs := range nodeGroups {
		for _, pvc := range nodePVCs {
			item := BackupPlanItem{
				NodeName:  nodeName,
				Namespace: pvc.Namespace,
				PVCName:   pvc.PVCName,
				Status:    "Planned",
			}
			backupPlan = append(backupPlan, item)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.NodeName, item.Namespace, item.PVCName, item.Status)
		}
	}
	w.Flush()

	if opts.DryRun {
		fmt.Printf("\nDry run completed. %d PVCs would be backed up.\n", len(backupPlan))
		return nil
	}

	// Ask for confirmation
	fmt.Printf("\nProceed with backup? [y/N]: ")
	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "y" && confirm != "Y" && confirm != "yes" && confirm != "Yes" {
		fmt.Println("Backup cancelled.")
		return nil
	}

	fmt.Println("\nStarting backup operations...")

	// Group requests by node and execute in parallel
	var targetNodes []string
	for nodeName := range nodeGroups {
		targetNodes = append(targetNodes, nodeName)
	}

	// Execute backup command on target nodes
	responses, err := nodeExecutor.ExecuteOnNodesWithPVCs(ctx, targetNodes, nodecom.NodeCommandRequest{
		Command:   "backup",
		Namespace: opts.FilterOptions.Namespace,
		PVC:       opts.FilterOptions.PVC,
	})
	if err != nil {
		return fmt.Errorf("failed to execute cross-node backup: %v", err)
	}

	// Process results
	var results []BackupResult
	for _, resp := range responses {
		if !resp.Success {
			log.Warnf("Node %s failed: %s", resp.NodeName, resp.Error)
			results = append(results, BackupResult{
				NodeName: resp.NodeName,
				Success:  false,
				Error:    resp.Error,
			})
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

				result := BackupResult{
					NodeName:  resp.NodeName,
					Namespace: getString(itemMap, "namespace"),
					PVCName:   getString(itemMap, "pvc_name"),
					Success:   getBool(itemMap, "success"),
					Error:     getString(itemMap, "error"),
				}

				results = append(results, result)
			}
		}
	}

	// Display results
	fmt.Println("\nBackup Results:")
	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NODE\tNAMESPACE\tPVC\tSTATUS\tERROR")

	// Sort results
	sort.Slice(results, func(i, j int) bool {
		if results[i].NodeName != results[j].NodeName {
			return results[i].NodeName < results[j].NodeName
		}
		if results[i].Namespace != results[j].Namespace {
			return results[i].Namespace < results[j].Namespace
		}
		return results[i].PVCName < results[j].PVCName
	})

	successCount := 0
	for _, result := range results {
		status := "Success"
		if !result.Success {
			status = "Failed"
		} else {
			successCount++
		}

		errorMsg := result.Error
		if len(errorMsg) > 50 {
			errorMsg = errorMsg[:47] + "..."
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			result.NodeName, result.Namespace, result.PVCName, status, errorMsg)
	}
	w.Flush()

	fmt.Printf("\nBackup completed: %d successful, %d failed\n", successCount, len(results)-successCount)

	if successCount < len(results) {
		return fmt.Errorf("some backups failed")
	}

	return nil
}

// BackupPlanItem represents an item in the backup plan
type BackupPlanItem struct {
	NodeName  string
	Namespace string
	PVCName   string
	Status    string
}

// BackupResult represents the result of a backup operation
type BackupResult struct {
	NodeName  string
	Namespace string
	PVCName   string
	Success   bool
	Error     string
}