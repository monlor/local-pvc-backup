package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/caarlos0/env/v10"
	"github.com/monlor/local-pvc-backup/pkg/backup"
	"github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	cfg  *config.Config
	log  *logrus.Logger
	root = &cobra.Command{
		Use:   "local-pvc-backup",
		Short: "Local PVC backup tool",
		Long:  `A tool for backing up local PVCs using restic`,
	}
)

func init() {
	// Initialize logger
	log = logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Load configuration from environment variables
	cfg = &config.Config{}
	if err := env.Parse(cfg); err != nil {
		log.Fatalf("Failed to parse environment variables: %v", err)
	}

	// Set log level
	level, err := logrus.ParseLevel(cfg.BackupConfig.LogLevel)
	if err != nil {
		log.Warnf("Invalid log level %s, using info", cfg.BackupConfig.LogLevel)
		level = logrus.InfoLevel
	}
	log.SetLevel(level)
}

func main() {
	// Add run command
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the backup service",
		Run: func(cmd *cobra.Command, args []string) {
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
			runResticCommand(args)
		},
	}

	root.AddCommand(runCmd)
	root.AddCommand(resticCmd)

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

func runBackupService() {
	// Create backup manager
	manager, err := backup.NewManager(cfg, log)
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

func runResticCommand(args []string) {
	// Create restic command
	cmd := exec.Command("restic", args...)

	// Set environment variables from config
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("RESTIC_REPOSITORY=s3:%s/%s/%s", cfg.S3Config.Endpoint, cfg.S3Config.Bucket, cfg.S3Config.Path))
	cmd.Env = append(cmd.Env, fmt.Sprintf("RESTIC_PASSWORD=%s", cfg.ResticConfig.Password))
	cmd.Env = append(cmd.Env, fmt.Sprintf("RESTIC_CACHE_PATH=%s", cfg.ResticConfig.CachePath))
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
