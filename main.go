package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/monlor/local-pvc-backup/pkg/backup"
	"github.com/monlor/local-pvc-backup/pkg/cli"
	"github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/monlor/local-pvc-backup/pkg/discovery"
	"github.com/monlor/local-pvc-backup/pkg/k8s"
	"github.com/monlor/local-pvc-backup/pkg/restic"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	cfg          *config.Config
	log          *logrus.Logger
	k8sClient    *k8s.Client
	resticClient *restic.Client
	root         = &cobra.Command{
		Use:   "local-pvc-backup",
		Short: "Local PVC backup tool",
		Long:  `A tool for backing up local PVCs using restic`,
	}
)

func init() {
	// Initialize logger only
	log = logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	log.SetLevel(logrus.InfoLevel) // default level
}

// initializeClients initializes k8s and restic clients with full configuration
func initializeClients() error {
	// Load configuration from environment variables
	cfg = &config.Config{}
	if err := env.Parse(cfg); err != nil {
		return fmt.Errorf("failed to parse environment variables: %v", err)
	}

	// Set log level
	level, err := logrus.ParseLevel(cfg.BackupConfig.LogLevel)
	if err != nil {
		log.Warnf("Invalid log level %s, using info", cfg.BackupConfig.LogLevel)
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	// Initialize k8s client
	k8sClient, err = k8s.NewClient(log)
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %v", err)
	}

	// Initialize restic client
	resticClient = restic.NewClient(
		cfg.S3Config.Endpoint,
		cfg.S3Config.Bucket,
		cfg.S3Config.Path,
		cfg.S3Config.AccessKey,
		cfg.S3Config.SecretKey,
		cfg.S3Config.Region,
		cfg.ResticConfig.Password,
		cfg.ResticConfig.CachePath,
		k8sClient.GetNodeName(),
		log,
	)
	
	return nil
}

func main() {
	// Add run command
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the backup service",
		Run: func(cmd *cobra.Command, args []string) {
			if err := initializeClients(); err != nil {
				log.Fatal(err)
			}
			runBackupService()
		},
	}

	// Add restic command
	resticCmd := &cobra.Command{
		Use:                "restic [restic command]",
		Short:              "Execute restic command with injected environment variables",
		Long:               "Execute restic command with all environment variables from configuration",
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				log.Fatal("Please provide a restic command")
			}
			if err := initializeClients(); err != nil {
				log.Fatal(err)
			}
			runResticCommand(args)
		},
	}

	// Add node-exec command (for internal cross-node communication)
	nodeExecCmd := &cobra.Command{
		Use:    "node-exec [command]",
		Short:  "Internal command for cross-node execution",
		Hidden: true, // Hide from help
		Run: func(cmd *cobra.Command, args []string) {
			if err := initializeClients(); err != nil {
				log.Fatal(err)
			}
			runNodeExec(cmd, args)
		},
	}

	// Add enhanced status command - create template and copy flags
	statusTemplate := cli.NewStatusEnhancedCommand(nil, nil, log)
	statusCmd := &cobra.Command{
		Use:   statusTemplate.Use,
		Short: statusTemplate.Short,
		Long:  statusTemplate.Long,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initializeClients(); err != nil {
				return err
			}
			// Ensure we have a proper context
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			actualCmd := cli.NewStatusEnhancedCommand(k8sClient.GetClientset(), k8sClient.GetConfig(), log)
			// Set context on the actual command
			actualCmd.SetContext(ctx)
			// Copy flag values
			actualCmd.Flags().Set("all", cmd.Flag("all").Value.String())
			actualCmd.Flags().Set("namespace", cmd.Flag("namespace").Value.String())
			actualCmd.Flags().Set("node", cmd.Flag("node").Value.String())
			actualCmd.Flags().Set("pvc", cmd.Flag("pvc").Value.String())
			actualCmd.Flags().Set("output", cmd.Flag("output").Value.String())
			return actualCmd.RunE(actualCmd, args)
		},
	}
	statusCmd.Flags().AddFlagSet(statusTemplate.Flags())

	// Add snapshots command - create template and copy flags
	snapshotsTemplate := cli.NewSnapshotsCommand(nil, nil, log)
	snapshotsCmd := &cobra.Command{
		Use:   snapshotsTemplate.Use,
		Short: snapshotsTemplate.Short,
		Long:  snapshotsTemplate.Long,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initializeClients(); err != nil {
				return err
			}
			// Ensure we have a proper context
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			actualCmd := cli.NewSnapshotsCommand(k8sClient.GetClientset(), k8sClient.GetConfig(), log)
			// Set context on the actual command
			actualCmd.SetContext(ctx)
			// Copy flag values
			actualCmd.Flags().Set("all", cmd.Flag("all").Value.String())
			actualCmd.Flags().Set("namespace", cmd.Flag("namespace").Value.String())
			actualCmd.Flags().Set("node", cmd.Flag("node").Value.String())
			actualCmd.Flags().Set("pvc", cmd.Flag("pvc").Value.String())
			actualCmd.Flags().Set("output", cmd.Flag("output").Value.String())
			actualCmd.Flags().Set("limit", cmd.Flag("limit").Value.String())
			actualCmd.Flags().Set("sort-by", cmd.Flag("sort-by").Value.String())
			return actualCmd.RunE(actualCmd, args)
		},
	}
	snapshotsCmd.Flags().AddFlagSet(snapshotsTemplate.Flags())

	// Add backup command - create template and copy flags
	backupTemplate := cli.NewBackupCommand(nil, nil, log)
	backupCmd := &cobra.Command{
		Use:   backupTemplate.Use,
		Short: backupTemplate.Short,
		Long:  backupTemplate.Long,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initializeClients(); err != nil {
				return err
			}
			// Ensure we have a proper context
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			actualCmd := cli.NewBackupCommand(k8sClient.GetClientset(), k8sClient.GetConfig(), log)
			// Set context on the actual command
			actualCmd.SetContext(ctx)
			// Copy flag values
			actualCmd.Flags().Set("all", cmd.Flag("all").Value.String())
			actualCmd.Flags().Set("namespace", cmd.Flag("namespace").Value.String())
			actualCmd.Flags().Set("node", cmd.Flag("node").Value.String())
			actualCmd.Flags().Set("pvc", cmd.Flag("pvc").Value.String())
			actualCmd.Flags().Set("dry-run", cmd.Flag("dry-run").Value.String())
			actualCmd.Flags().Set("wait", cmd.Flag("wait").Value.String())
			return actualCmd.RunE(actualCmd, args)
		},
	}
	backupCmd.Flags().AddFlagSet(backupTemplate.Flags())

	// Add restore command - create template and copy flags
	restoreTemplate := cli.NewRestoreCommand(nil, nil, log)
	restoreCmd := &cobra.Command{
		Use:   restoreTemplate.Use,
		Short: restoreTemplate.Short,
		Long:  restoreTemplate.Long,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initializeClients(); err != nil {
				return err
			}
			// Ensure we have a proper context
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			actualCmd := cli.NewRestoreCommand(k8sClient.GetClientset(), k8sClient.GetConfig(), log)
			// Set context on the actual command
			actualCmd.SetContext(ctx)
			// Copy flag values
			actualCmd.Flags().Set("all", cmd.Flag("all").Value.String())
			actualCmd.Flags().Set("namespace", cmd.Flag("namespace").Value.String())
			actualCmd.Flags().Set("node", cmd.Flag("node").Value.String())
			actualCmd.Flags().Set("pvc", cmd.Flag("pvc").Value.String())
			actualCmd.Flags().Set("time", cmd.Flag("time").Value.String())
			actualCmd.Flags().Set("snapshot", cmd.Flag("snapshot").Value.String())
			actualCmd.Flags().Set("target-path", cmd.Flag("target-path").Value.String())
			actualCmd.Flags().Set("dry-run", cmd.Flag("dry-run").Value.String())
			actualCmd.Flags().Set("wait", cmd.Flag("wait").Value.String())
			return actualCmd.RunE(actualCmd, args)
		},
	}
	restoreCmd.Flags().AddFlagSet(restoreTemplate.Flags())

	root.AddCommand(runCmd)
	root.AddCommand(resticCmd)
	root.AddCommand(nodeExecCmd)
	root.AddCommand(statusCmd)
	root.AddCommand(snapshotsCmd)
	root.AddCommand(backupCmd)
	root.AddCommand(restoreCmd)

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

func runBackupService() {
	// Create backup manager
	manager, err := backup.NewManager(cfg, k8sClient, resticClient, log)
	if err != nil {
		log.Fatalf("Failed to create backup manager: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Infof("Received shutdown signal: %v", sig)
		cancel()
	}()

	// Start backup loop
	log.Info("Starting backup service...")
	if err := manager.StartBackupLoop(ctx); err != nil {
		log.Fatalf("Backup service error: %v", err)
	}
}

func runNodeExec(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		log.Fatal("node-exec requires a command")
	}

	command := args[0]
	cmdArgs := args[1:]

	// Parse additional flags
	var namespace, pvc string
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace filter")
	cmd.Flags().StringVar(&pvc, "pvc", "", "PVC filter")
	cmd.ParseFlags(cmdArgs)

	// Remove parsed flags from args
	filteredArgs := []string{}
	skipNext := false
	for _, arg := range cmdArgs {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--namespace" || arg == "--pvc" {
			skipNext = true
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	response := handleNodeExecCommand(command, filteredArgs, namespace, pvc)
	
	// Output JSON response
	output, _ := json.Marshal(response)
	fmt.Println(string(output))
}

func handleNodeExecCommand(command string, args []string, namespace, pvc string) map[string]interface{} {
	ctx := context.Background()
	
	switch command {
	case "status":
		return handleNodeExecStatus(ctx, namespace, pvc)
	case "snapshots":
		return handleNodeExecSnapshots(ctx, namespace, pvc)
	case "backup":
		return handleNodeExecBackup(ctx, namespace, pvc)
	case "restore":
		return handleNodeExecRestore(ctx, args, namespace, pvc)
	default:
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("unknown command: %s", command),
		}
	}
}

func handleNodeExecStatus(ctx context.Context, namespace, pvc string) map[string]interface{} {
	// Get local PVCs that match the filter
	filter := discovery.FilterOptions{
		Namespace: namespace,
		PVC:       pvc,
		All:       namespace == "" && pvc == "",
	}
	
	discoveryClient := discovery.NewDiscovery(k8sClient.GetClientset(), "local-pvc-backup", "default", "/data", log)
	pvcs, err := discoveryClient.GetPVCsByFilter(ctx, filter)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("failed to get PVCs: %v", err),
		}
	}

	// Get backup status for each PVC
	var pvcStatuses []map[string]interface{}
	for _, pvcInfo := range pvcs {
		// Get snapshots for this PVC
		snapshots, err := resticClient.ListSnapshotsByPVC(ctx, pvcInfo.UID)
		if err != nil {
			log.Debugf("Failed to get snapshots for PVC %s/%s: %v", pvcInfo.Namespace, pvcInfo.PVCName, err)
			snapshots = []restic.SnapshotInfo{}
		}

		var lastBackup *time.Time
		if len(snapshots) > 0 {
			// Find the most recent snapshot
			latest := snapshots[0]
			for _, snapshot := range snapshots {
				if snapshot.Time.After(latest.Time) {
					latest = snapshot
				}
			}
			lastBackup = &latest.Time
		}

		pvcStatuses = append(pvcStatuses, map[string]interface{}{
			"pvc_name":       pvcInfo.PVCName,
			"namespace":      pvcInfo.Namespace,
			"path":           pvcInfo.Path,
			"uid":            pvcInfo.UID,
			"last_backup":    lastBackup,
			"snapshot_count": len(snapshots),
			"backup_enabled": pvcInfo.BackupEnabled,
			"node_name":      k8sClient.GetNodeName(),
		})
	}

	return map[string]interface{}{
		"success": true,
		"data":    pvcStatuses,
	}
}

func handleNodeExecSnapshots(ctx context.Context, namespace, pvc string) map[string]interface{} {
	// Get local PVCs that match the filter
	filter := discovery.FilterOptions{
		Namespace: namespace,
		PVC:       pvc,
		All:       namespace == "" && pvc == "",
	}
	
	discoveryClient := discovery.NewDiscovery(k8sClient.GetClientset(), "local-pvc-backup", "default", "/data", log)
	pvcs, err := discoveryClient.GetPVCsByFilter(ctx, filter)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("failed to get PVCs: %v", err),
		}
	}

	var allSnapshots []map[string]interface{}
	for _, pvcInfo := range pvcs {
		snapshots, err := resticClient.ListSnapshotsByPVC(ctx, pvcInfo.UID)
		if err != nil {
			log.Debugf("Failed to get snapshots for PVC %s/%s: %v", pvcInfo.Namespace, pvcInfo.PVCName, err)
			continue
		}

		for _, snapshot := range snapshots {
			allSnapshots = append(allSnapshots, map[string]interface{}{
				"id":         snapshot.ID,
				"time":       snapshot.Time,
				"pvc_name":   pvcInfo.PVCName,
				"namespace":  pvcInfo.Namespace,
				"node_name":  k8sClient.GetNodeName(),
				"hostname":   snapshot.Hostname,
				"tags":       snapshot.Tags,
				"paths":      snapshot.Paths,
			})
		}
	}

	return map[string]interface{}{
		"success": true,
		"data":    allSnapshots,
	}
}

func handleNodeExecBackup(ctx context.Context, namespace, pvc string) map[string]interface{} {
	// Get local PVCs that match the filter
	filter := discovery.FilterOptions{
		Namespace: namespace,
		PVC:       pvc,
		All:       namespace == "" && pvc == "",
	}
	
	discoveryClient := discovery.NewDiscovery(k8sClient.GetClientset(), "local-pvc-backup", "default", "/data", log)
	pvcs, err := discoveryClient.GetPVCsByFilter(ctx, filter)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("failed to get PVCs: %v", err),
		}
	}

	var results []map[string]interface{}
	for _, pvcInfo := range pvcs {
		// Perform backup for this PVC
		backupPaths := []string{pvcInfo.Path}
		excludePatterns := []string{}

		// Process include/exclude patterns if configured
		if pvcInfo.Config.Include != "" {
			// TODO: Process include patterns properly
		}
		if pvcInfo.Config.Exclude != "" {
			// TODO: Process exclude patterns properly
		}

		err := resticClient.Backup(ctx, backupPaths, excludePatterns, pvcInfo.UID, pvcInfo.PVCName, pvcInfo.Namespace)
		
		results = append(results, map[string]interface{}{
			"pvc_name":  pvcInfo.PVCName,
			"namespace": pvcInfo.Namespace,
			"node_name": k8sClient.GetNodeName(),
			"success":   err == nil,
			"error":     func() string { if err != nil { return err.Error() }; return "" }(),
		})
	}

	return map[string]interface{}{
		"success": true,
		"data":    results,
	}
}

func handleNodeExecRestore(ctx context.Context, args []string, namespace, pvc string) map[string]interface{} {
	// Parse restore arguments
	if len(args) == 0 {
		return map[string]interface{}{
			"success": false,
			"error":   "restore requires snapshot ID or time",
		}
	}

	// TODO: Implement restore logic
	return map[string]interface{}{
		"success": false,
		"error":   "restore not implemented yet",
	}
}

func runResticCommand(args []string) {
	// Create restic command
	cmd := exec.Command("restic", args...)

	// Set environment variables from config
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("RESTIC_REPOSITORY=%s", resticClient.GetRepository()))
	cmd.Env = append(cmd.Env, fmt.Sprintf("RESTIC_PASSWORD=%s", cfg.ResticConfig.Password))
	cmd.Env = append(cmd.Env, fmt.Sprintf("RESTIC_CACHE_DIR=%s", cfg.ResticConfig.CachePath))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", cfg.S3Config.AccessKey))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", cfg.S3Config.SecretKey))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AWS_DEFAULT_REGION=%s", cfg.S3Config.Region))

	// Set command output to current process output
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run the command
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to execute restic command: %v", err)
	}
}
