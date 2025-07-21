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

// RestoreOptions holds options for the restore command
type RestoreOptions struct {
	FilterOptions
	Time       string
	SnapshotID string
	TargetPath string
	DryRun     bool
	Wait       bool
}

// NewRestoreCommand creates the restore command
func NewRestoreCommand(k8sClient kubernetes.Interface, restConfig *rest.Config, appConfig *cfg.Config, log *logrus.Logger) *cobra.Command {
	opts := &RestoreOptions{}

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore PVCs from backup snapshots",
		Long:  "Restore PVCs from backup snapshots across all relevant nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if k8sClient == nil {
				return fmt.Errorf("kubernetes client not initialized")
			}
			return runRestore(cmd.Context(), k8sClient, restConfig, appConfig, opts, log)
		},
	}

	// Add common filter flags
	AddFilterFlags(cmd, &opts.FilterOptions)
	
	// Add restore-specific flags
	cmd.Flags().StringVar(&opts.Time, "time", "", "Restore to specific time (YYYY-MM-DD HH:MM:SS)")
	cmd.Flags().StringVar(&opts.SnapshotID, "snapshot", "", "Restore from specific snapshot ID")
	cmd.Flags().StringVar(&opts.TargetPath, "target-path", "", "Custom restore target path (default: original location)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show restore plan without executing")
	cmd.Flags().BoolVar(&opts.Wait, "wait", true, "Wait for restore operations to complete")

	return cmd
}

func runRestore(ctx context.Context, k8sClient kubernetes.Interface, restConfig *rest.Config, appConfig *cfg.Config, opts *RestoreOptions, log *logrus.Logger) error {
	// Validate filter options
	if err := opts.FilterOptions.Validate(); err != nil {
		return fmt.Errorf("invalid filter options: %v", err)
	}

	// Validate restore options
	if opts.Time == "" && opts.SnapshotID == "" {
		return fmt.Errorf("either --time or --snapshot must be specified")
	}

	if opts.Time != "" && opts.SnapshotID != "" {
		return fmt.Errorf("cannot specify both --time and --snapshot")
	}

	var targetTime time.Time
	var err error
	if opts.Time != "" {
		targetTime, err = time.Parse("2006-01-02 15:04:05", opts.Time)
		if err != nil {
			return fmt.Errorf("invalid time format: %v (use YYYY-MM-DD HH:MM:SS)", err)
		}
	}

	log.Debugf("Running restore command with filter: %s", opts.FilterOptions.String())

	// Get DaemonSet configuration from application config
	log.Debugf("Using DaemonSet: name=%s, namespace=%s, storage=%s", appConfig.KubernetesConfig.DaemonSetName, appConfig.KubernetesConfig.PodNamespace, appConfig.BackupConfig.StoragePath)

	// Create node executor for cross-node communication
	nodeExecutor := nodecom.NewNodeExecutor(k8sClient, restConfig, appConfig.KubernetesConfig.DaemonSetName, appConfig.KubernetesConfig.PodNamespace, log)

	// Create discovery client to find PVCs and their nodes
	discoveryClient := discovery.NewDiscovery(k8sClient, appConfig.KubernetesConfig.DaemonSetName, appConfig.KubernetesConfig.PodNamespace, appConfig.BackupConfig.StoragePath, log)

	// Get PVCs matching the filter
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

	fmt.Printf("Creating restore plan for %d PVCs...\n", len(pvcs))

	// If using time-based restore, we need to find the best snapshots for each PVC
	var restorePlan []RestorePlanItem
	if opts.Time != "" {
		restorePlan, err = createTimeBasedRestorePlan(ctx, nodeExecutor, pvcs, targetTime, opts, log)
		if err != nil {
			return fmt.Errorf("failed to create restore plan: %v", err)
		}
	} else {
		// For snapshot-based restore, use the specified snapshot
		restorePlan, err = createSnapshotBasedRestorePlan(ctx, nodeExecutor, pvcs, opts.SnapshotID, opts, log)
		if err != nil {
			return fmt.Errorf("failed to create restore plan: %v", err)
		}
	}

	if len(restorePlan) == 0 {
		fmt.Println("No suitable snapshots found for restore.")
		return nil
	}

	// Display restore plan
	fmt.Printf("\nRestore Plan:\n")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NODE\tNAMESPACE\tPVC\tSNAPSHOT ID\tSNAPSHOT TIME\tTARGET PATH")

	for _, item := range restorePlan {
		shortID := item.SnapshotID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}

		targetPath := item.TargetPath
		if targetPath == "" {
			targetPath = "original"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			item.NodeName,
			item.Namespace,
			item.PVCName,
			shortID,
			item.SnapshotTime.Format("2006-01-02 15:04:05"),
			targetPath,
		)
	}
	w.Flush()

	if opts.DryRun {
		fmt.Printf("\nDry run completed. %d PVCs would be restored.\n", len(restorePlan))
		return nil
	}

	// Ask for confirmation
	fmt.Printf("\nProceed with restore? [y/N]: ")
	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "y" && confirm != "Y" && confirm != "yes" && confirm != "Yes" {
		fmt.Println("Restore cancelled.")
		return nil
	}

	fmt.Println("\nStarting restore operations...")

	// Execute restore operations
	// TODO: Implement actual restore execution through nodecom
	fmt.Println("Restore execution not yet implemented.")
	
	return nil
}

// RestorePlanItem represents an item in the restore plan
type RestorePlanItem struct {
	NodeName     string
	Namespace    string
	PVCName      string
	SnapshotID   string
	SnapshotTime time.Time
	TargetPath   string
}

func createTimeBasedRestorePlan(ctx context.Context, nodeExecutor *nodecom.NodeExecutor, pvcs []discovery.GlobalPVCInfo, targetTime time.Time, opts *RestoreOptions, log *logrus.Logger) ([]RestorePlanItem, error) {
	// Group PVCs by node
	nodeGroups := make(map[string][]discovery.GlobalPVCInfo)
	for _, pvc := range pvcs {
		nodeGroups[pvc.NodeName] = append(nodeGroups[pvc.NodeName], pvc)
	}

	// Get snapshots from all nodes
	var targetNodes []string
	for nodeName := range nodeGroups {
		targetNodes = append(targetNodes, nodeName)
	}

	log.Debugf("Getting snapshots from %d nodes for time-based restore", len(targetNodes))

	responses, err := nodeExecutor.ExecuteOnNodesWithPVCs(ctx, targetNodes, nodecom.NodeCommandRequest{
		Command:   "snapshots",
		Namespace: opts.FilterOptions.Namespace,
		PVC:       opts.FilterOptions.PVC,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshots: %v", err)
	}

	// Build snapshot map: PVC UID -> best snapshot for target time
	snapshotMap := make(map[string]RestorePlanItem)

	for _, resp := range responses {
		if !resp.Success {
			log.Warnf("Node %s failed to get snapshots: %s", resp.NodeName, resp.Error)
			continue
		}

		if resp.Data == nil {
			continue
		}

		dataList, ok := resp.Data.([]interface{})
		if !ok {
			continue
		}

		for _, item := range dataList {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			namespace := getString(itemMap, "namespace")
			pvcName := getString(itemMap, "pvc_name")
			snapshotID := getString(itemMap, "id")
			
			// Parse snapshot time
			timeStr := getString(itemMap, "time")
			snapshotTime, err := time.Parse(time.RFC3339, timeStr)
			if err != nil {
				log.Debugf("Failed to parse snapshot time %s: %v", timeStr, err)
				continue
			}

			// Only consider snapshots that are before or at the target time
			if snapshotTime.After(targetTime) {
				continue
			}

			// Find the corresponding PVC
			var pvcUID string
			for _, pvc := range pvcs {
				if pvc.Namespace == namespace && pvc.PVCName == pvcName && pvc.NodeName == resp.NodeName {
					pvcUID = pvc.UID
					break
				}
			}

			if pvcUID == "" {
				continue
			}

			// Check if this is a better snapshot (closer to target time)
			existing, exists := snapshotMap[pvcUID]
			if !exists || snapshotTime.After(existing.SnapshotTime) {
				snapshotMap[pvcUID] = RestorePlanItem{
					NodeName:     resp.NodeName,
					Namespace:    namespace,
					PVCName:      pvcName,
					SnapshotID:   snapshotID,
					SnapshotTime: snapshotTime,
					TargetPath:   opts.TargetPath,
				}
			}
		}
	}

	// Convert map to slice
	var plan []RestorePlanItem
	for _, item := range snapshotMap {
		plan = append(plan, item)
	}

	// Sort plan
	sort.Slice(plan, func(i, j int) bool {
		if plan[i].NodeName != plan[j].NodeName {
			return plan[i].NodeName < plan[j].NodeName
		}
		if plan[i].Namespace != plan[j].Namespace {
			return plan[i].Namespace < plan[j].Namespace
		}
		return plan[i].PVCName < plan[j].PVCName
	})

	return plan, nil
}

func createSnapshotBasedRestorePlan(ctx context.Context, nodeExecutor *nodecom.NodeExecutor, pvcs []discovery.GlobalPVCInfo, snapshotID string, opts *RestoreOptions, log *logrus.Logger) ([]RestorePlanItem, error) {
	// For snapshot-based restore, we need to find which PVC the snapshot belongs to
	// This is a simplified implementation - we'd need to get all snapshots and find the matching one
	
	// TODO: Implement snapshot-based restore plan
	return nil, fmt.Errorf("snapshot-based restore not yet implemented")
}