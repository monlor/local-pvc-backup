package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/caarlos0/env/v10"
	"github.com/monlor/local-pvc-backup/pkg/backup"
	"github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/sirupsen/logrus"
)

func main() {
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Load configuration from environment variables
	cfg := &config.Config{}
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
