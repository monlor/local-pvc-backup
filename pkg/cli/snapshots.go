package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/monlor/local-pvc-backup/pkg/discovery"
	"github.com/monlor/local-pvc-backup/pkg/nodecom"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// SnapshotsOptions holds options for the snapshots command
type SnapshotsOptions struct {
	FilterOptions
	OutputFormat string
	Limit        int
	SortBy       string
}

// NewSnapshotsCommand creates the snapshots command
func NewSnapshotsCommand(k8sClient kubernetes.Interface, config *rest.Config, log *logrus.Logger) *cobra.Command {
	opts := &SnapshotsOptions{}

	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "List PVC backup snapshots across all nodes",
		Long:  "Display snapshots for PVCs across all nodes using cross-node communication",
		RunE: func(cmd *cobra.Command, args []string) error {
			if k8sClient == nil {
				return fmt.Errorf("kubernetes client not initialized")
			}
			return runSnapshots(cmd.Context(), k8sClient, config, opts, log)
		},
	}

	// Add common filter flags
	AddFilterFlags(cmd, &opts.FilterOptions)
	
	// Add snapshots-specific flags
	cmd.Flags().StringVarP(&opts.OutputFormat, "output", "o", "table", "Output format (table, json, yaml)")
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "Limit number of snapshots to show (0 = no limit)")
	cmd.Flags().StringVar(&opts.SortBy, "sort-by", "time", "Sort by field (time, size, pvc)")

	return cmd
}

func runSnapshots(ctx context.Context, k8sClient kubernetes.Interface, config *rest.Config, opts *SnapshotsOptions, log *logrus.Logger) error {
	// Validate filter options
	if err := opts.FilterOptions.Validate(); err != nil {
		return fmt.Errorf("invalid filter options: %v", err)
	}

	log.Debugf("Running snapshots command with filter: %s", opts.FilterOptions.String())

	// Create node executor for cross-node communication
	nodeExecutor := nodecom.NewNodeExecutor(k8sClient, config, "local-pvc-backup", "kube-system", log)

	// Create discovery client to find nodes with relevant PVCs
	discoveryClient := discovery.NewDiscovery(k8sClient, "local-pvc-backup", "kube-system", "/data", log)

	var targetNodes []string
	if opts.FilterOptions.All {
		// Get all nodes with daemon pods
		nodes, err := nodeExecutor.ExecuteOnAllNodes(ctx, nodecom.NodeCommandRequest{
			Command: "snapshots",
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

	log.Debugf("Executing snapshots command on %d nodes", len(targetNodes))

	// Execute snapshots command on target nodes
	responses, err := nodeExecutor.ExecuteOnNodesWithPVCs(ctx, targetNodes, nodecom.NodeCommandRequest{
		Command:   "snapshots",
		Namespace: opts.FilterOptions.Namespace,
		PVC:       opts.FilterOptions.PVC,
	})
	if err != nil {
		return fmt.Errorf("failed to execute cross-node snapshots: %v", err)
	}

	// Aggregate results
	var allSnapshots []SnapshotInfo
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

				snapshotInfo := SnapshotInfo{
					ID:        getString(itemMap, "id"),
					NodeName:  resp.NodeName,
					Namespace: getString(itemMap, "namespace"),
					PVCName:   getString(itemMap, "pvc_name"),
					Hostname:  getString(itemMap, "hostname"),
				}

				// Parse time
				if timeStr := getString(itemMap, "time"); timeStr != "" {
					if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
						snapshotInfo.Time = t
					}
				}

				// Parse tags
				if tagsInterface, ok := itemMap["tags"]; ok {
					if tagsList, ok := tagsInterface.([]interface{}); ok {
						for _, tag := range tagsList {
							if tagStr, ok := tag.(string); ok {
								snapshotInfo.Tags = append(snapshotInfo.Tags, tagStr)
							}
						}
					}
				}

				// Parse paths
				if pathsInterface, ok := itemMap["paths"]; ok {
					if pathsList, ok := pathsInterface.([]interface{}); ok {
						for _, path := range pathsList {
							if pathStr, ok := path.(string); ok {
								snapshotInfo.Paths = append(snapshotInfo.Paths, pathStr)
							}
						}
					}
				}

				allSnapshots = append(allSnapshots, snapshotInfo)
			}
		}
	}

	if len(allSnapshots) == 0 {
		fmt.Println("No snapshots found matching the specified filters.")
		return nil
	}

	// Sort snapshots
	sortSnapshots(allSnapshots, opts.SortBy)

	// Apply limit
	if opts.Limit > 0 && len(allSnapshots) > opts.Limit {
		allSnapshots = allSnapshots[:opts.Limit]
	}

	// Display results based on output format
	switch opts.OutputFormat {
	case "table":
		return displaySnapshotsTable(allSnapshots)
	case "json":
		return displaySnapshotsJSON(allSnapshots)
	case "yaml":
		return displaySnapshotsYAML(allSnapshots)
	default:
		return fmt.Errorf("unsupported output format: %s", opts.OutputFormat)
	}
}

// SnapshotInfo represents information about a snapshot
type SnapshotInfo struct {
	ID        string
	Time      time.Time
	NodeName  string
	Namespace string
	PVCName   string
	Hostname  string
	Tags      []string
	Paths     []string
}

func sortSnapshots(snapshots []SnapshotInfo, sortBy string) {
	switch sortBy {
	case "time":
		sort.Slice(snapshots, func(i, j int) bool {
			return snapshots[i].Time.After(snapshots[j].Time) // Most recent first
		})
	case "pvc":
		sort.Slice(snapshots, func(i, j int) bool {
			if snapshots[i].Namespace != snapshots[j].Namespace {
				return snapshots[i].Namespace < snapshots[j].Namespace
			}
			if snapshots[i].PVCName != snapshots[j].PVCName {
				return snapshots[i].PVCName < snapshots[j].PVCName
			}
			return snapshots[i].Time.After(snapshots[j].Time)
		})
	case "node":
		sort.Slice(snapshots, func(i, j int) bool {
			if snapshots[i].NodeName != snapshots[j].NodeName {
				return snapshots[i].NodeName < snapshots[j].NodeName
			}
			return snapshots[i].Time.After(snapshots[j].Time)
		})
	}
}

func displaySnapshotsTable(snapshots []SnapshotInfo) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Print header
	fmt.Fprintln(w, "SNAPSHOT ID\tDATE\tNODE\tNAMESPACE\tPVC\tHOSTNAME")

	// Print each snapshot
	for _, snapshot := range snapshots {
		// Truncate snapshot ID for display
		shortID := snapshot.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID,
			snapshot.Time.Format("2006-01-02 15:04:05"),
			snapshot.NodeName,
			snapshot.Namespace,
			snapshot.PVCName,
			snapshot.Hostname,
		)
	}

	return nil
}

func displaySnapshotsJSON(snapshots []SnapshotInfo) error {
	// TODO: Implement JSON output
	return fmt.Errorf("JSON output not implemented yet")
}

func displaySnapshotsYAML(snapshots []SnapshotInfo) error {
	// TODO: Implement YAML output
	return fmt.Errorf("YAML output not implemented yet")
}