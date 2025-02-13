package backup

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	cfg "github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/monlor/local-pvc-backup/pkg/k8s"
	"github.com/monlor/local-pvc-backup/pkg/restic"
	"github.com/sirupsen/logrus"
)

// Manager handles the backup operations
type Manager struct {
	resticClient *restic.Client
	k8sClient    *k8s.Client
	storagePath  string
	interval     time.Duration
	retention    string
	log          *logrus.Logger
}

// NewManager creates a new backup manager
func NewManager(config *cfg.Config, log *logrus.Logger) (*Manager, error) {
	k8sClient, err := k8s.NewClient(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %v", err)
	}

	resticClient := restic.NewClient(
		config.S3Config.Endpoint,
		config.S3Config.Bucket,
		config.S3Config.Path,
		config.S3Config.AccessKey,
		config.S3Config.SecretKey,
		config.S3Config.Region,
		config.ResticConfig.Password,
		config.ResticConfig.CachePath,
		k8sClient.GetNodeName(),
		log,
	)

	// Ensure restic repository is initialized
	if err := resticClient.EnsureRepository(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ensure restic repository: %v", err)
	}

	return &Manager{
		resticClient: resticClient,
		k8sClient:    k8sClient,
		storagePath:  config.BackupConfig.StoragePath,
		interval:     config.BackupConfig.BackupInterval,
		retention:    config.BackupConfig.Retention,
		log:          log,
	}, nil
}

// StartBackupLoop starts the backup loop
func (m *Manager) StartBackupLoop(ctx context.Context) error {
	// 立即执行一次备份
	if err := m.performBackups(ctx); err != nil {
		m.log.Errorf("Initial backup failed: %v", err)
	}

	// 创建定时器
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	m.log.Infof("Starting backup loop with interval: %v", m.interval)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.performBackups(ctx); err != nil {
				m.log.Errorf("Error performing backups: %v", err)
			}
		}
	}
}

// processPatterns processes comma-separated pattern string and returns a list of patterns with base path
func (m *Manager) processPatterns(basePath, patternStr string) []string {
	if patternStr == "" {
		return nil
	}

	var result []string
	patterns := strings.Split(patternStr, ",")
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		// Join base path with pattern
		result = append(result, filepath.Join(basePath, pattern))
	}
	return result
}

// performBackups performs the backup operation for all eligible PVCs
func (m *Manager) performBackups(ctx context.Context) error {
	pvcs, err := m.k8sClient.GetPVCsToBackup(ctx)
	if err != nil {
		return fmt.Errorf("failed to get PVCs to backup: %v", err)
	}

	if len(pvcs) == 0 {
		m.log.Info("No PVCs to backup")
		return nil
	}

	// Prepare backup paths and exclude patterns
	backupPaths := []string{}
	excludePatterns := []string{}

	// Add backup paths and exclude rules for each enabled PVC
	for _, pvc := range pvcs {
		m.log.Infof("Configuring backup for PVC %s/%s:", pvc.Namespace, pvc.Name)

		// Add base PVC path if no include paths specified
		if pvc.Config.Include == "" {
			backupPaths = append(backupPaths, pvc.Path)
		} else {
			// Process include paths
			if paths := m.processPatterns(pvc.Path, pvc.Config.Include); len(paths) > 0 {
				backupPaths = append(backupPaths, paths...)
			}
		}

		// Process exclude patterns
		if patterns := m.processPatterns(pvc.Path, pvc.Config.Exclude); len(patterns) > 0 {
			excludePatterns = append(excludePatterns, patterns...)
		}
	}

	// Execute backup with all PVC paths
	if err := m.resticClient.Backup(ctx, backupPaths, excludePatterns); err != nil {
		return fmt.Errorf("failed to backup data directory: %v", err)
	}

	// Clean up old backups using global retention policy
	if err := m.resticClient.Forget(ctx, m.retention); err != nil {
		m.log.Errorf("Error cleaning up old backups: %v", err)
	}

	return nil
}
