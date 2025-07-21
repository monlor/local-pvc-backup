package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	cfg "github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/monlor/local-pvc-backup/pkg/discovery"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

// StatusOptions holds options for the status command
type StatusOptions struct {
	FilterOptions
	OutputFormat string
}

// convertToDiscoveryFilter converts CLI FilterOptions to discovery FilterOptions
func (opts *StatusOptions) convertToDiscoveryFilter() discovery.FilterOptions {
	return discovery.FilterOptions{
		Node:      opts.FilterOptions.Node,
		Namespace: opts.FilterOptions.Namespace,
		PVC:       opts.FilterOptions.PVC,
		All:       opts.FilterOptions.All,
	}
}

// RunStatusCommand runs the status command with the given parameters
func RunStatusCommand(ctx context.Context, k8sClient kubernetes.Interface, appConfig *cfg.Config, cmd *cobra.Command, log *logrus.Logger) error {
	opts := &StatusOptions{}

	// Add filter flags to the command if not already added
	AddFilterFlags(cmd, &opts.FilterOptions)
	cmd.Flags().StringVarP(&opts.OutputFormat, "output", "o", "table", "Output format (table, json, yaml)")

	// Parse flags
	if err := cmd.ParseFlags([]string{}); err != nil {
		return err
	}

	return runStatus(ctx, k8sClient, appConfig, opts, log)
}

// NewStatusCommand creates the status command
func NewStatusCommand(k8sClient kubernetes.Interface, appConfig *cfg.Config, log *logrus.Logger) *cobra.Command {
	opts := &StatusOptions{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show PVC backup status",
		Long:  "Display the backup status of PVCs across all nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if k8sClient == nil {
				return fmt.Errorf("kubernetes client not initialized")
			}
			return runStatus(cmd.Context(), k8sClient, appConfig, opts, log)
		},
	}

	// Add common filter flags
	AddFilterFlags(cmd, &opts.FilterOptions)
	
	// Add status-specific flags
	cmd.Flags().StringVarP(&opts.OutputFormat, "output", "o", "table", "Output format (table, json, yaml)")

	return cmd
}

func runStatus(ctx context.Context, k8sClient kubernetes.Interface, appConfig *cfg.Config, opts *StatusOptions, log *logrus.Logger) error {
	// Validate filter options
	if err := opts.FilterOptions.Validate(); err != nil {
		return fmt.Errorf("invalid filter options: %v", err)
	}

	log.Debugf("Running status command with filter: %s", opts.FilterOptions.String())

	// Get DaemonSet configuration from application config
	log.Debugf("Using DaemonSet: name=%s, namespace=%s, storage=%s", appConfig.KubernetesConfig.DaemonSetName, appConfig.KubernetesConfig.PodNamespace, appConfig.BackupConfig.StoragePath)
	
	discoveryClient := discovery.NewDiscovery(k8sClient, appConfig.KubernetesConfig.DaemonSetName, appConfig.KubernetesConfig.PodNamespace, appConfig.BackupConfig.StoragePath, log)

	// Get PVCs matching the filter
	pvcs, err := discoveryClient.GetPVCsByFilter(ctx, opts.convertToDiscoveryFilter())
	if err != nil {
		return fmt.Errorf("failed to discover PVCs: %v", err)
	}

	if len(pvcs) == 0 {
		fmt.Println("No backup-enabled PVCs found matching the specified filters.")
		return nil
	}

	// Display results based on output format
	switch opts.OutputFormat {
	case "table":
		return displayStatusTable(pvcs)
	case "json":
		return displayStatusJSON(pvcs)
	case "yaml":
		return displayStatusYAML(pvcs)
	default:
		return fmt.Errorf("unsupported output format: %s", opts.OutputFormat)
	}
}

func displayStatusTable(pvcs []discovery.GlobalPVCInfo) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Print header
	fmt.Fprintln(w, "NODE\tNAMESPACE\tPVC\tLAST BACKUP\tSNAPSHOTS\tSIZE\tSTATUS")

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

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			pvc.NodeName,
			pvc.Namespace,
			pvc.PVCName,
			lastBackup,
			pvc.SnapshotCount,
			pvc.TotalSize,
			status,
		)
	}

	return nil
}

func displayStatusJSON(pvcs []discovery.GlobalPVCInfo) error {
	// TODO: Implement JSON output
	return fmt.Errorf("JSON output not implemented yet")
}

func displayStatusYAML(pvcs []discovery.GlobalPVCInfo) error {
	// TODO: Implement YAML output
	return fmt.Errorf("YAML output not implemented yet")
}