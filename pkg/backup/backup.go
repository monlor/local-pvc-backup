package backup

import (
	"context"
	"fmt"
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
	k8sClient, err := k8s.NewClient()
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

// performBackups performs the backup operation for all eligible PVCs
func (m *Manager) performBackups(ctx context.Context) error {
	pvcs, err := m.k8sClient.GetPVCsToBackup(ctx)
	if err != nil {
		return fmt.Errorf("failed to get PVCs to backup: %v", err)
	}

	// Prepare include and exclude patterns
	includePatterns := []string{}
	excludePatterns := []string{}

	// Add include and exclude rules for each enabled PVC
	for _, pvc := range pvcs {
		// Add include rule
		if pvc.Config.IncludePattern != "" {
			// Limit include rule to specific PVC path
			pattern := fmt.Sprintf("%s/%s", pvc.Path, pvc.Config.IncludePattern)
			includePatterns = append(includePatterns, pattern)
		} else {
			// If no include rule specified, include entire PVC directory
			includePatterns = append(includePatterns, pvc.Path)
		}

		// Add exclude rule
		if pvc.Config.ExcludePattern != "" {
			// Limit exclude rule to specific PVC path
			pattern := fmt.Sprintf("%s/%s", pvc.Path, pvc.Config.ExcludePattern)
			excludePatterns = append(excludePatterns, pattern)
		}
	}

	// Execute backup
	if err := m.resticClient.Backup(ctx, m.storagePath, includePatterns, excludePatterns); err != nil {
		return fmt.Errorf("failed to backup data directory: %v", err)
	}

	// Clean up old backups using global retention policy
	if err := m.resticClient.Forget(ctx, m.retention); err != nil {
		m.log.Errorf("Error cleaning up old backups: %v", err)
	}

	return nil
}
