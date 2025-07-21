package backup

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	cfg "github.com/monlor/local-pvc-backup/pkg/config"
	"github.com/monlor/local-pvc-backup/pkg/discovery"
	"github.com/monlor/local-pvc-backup/pkg/k8s"
	"github.com/monlor/local-pvc-backup/pkg/restic"
	"github.com/sirupsen/logrus"
)

// Manager handles the backup operations
type Manager struct {
	resticClient *restic.Client
	k8sClient    *k8s.Client
	discovery    *discovery.Discovery
	storagePath  string
	interval     time.Duration
	retention    string
	log          *logrus.Logger
}

// NewManager creates a new backup manager
func NewManager(config *cfg.Config, k8sClient *k8s.Client, resticClient *restic.Client, log *logrus.Logger) (*Manager, error) {
	// Ensure restic repository is initialized
	if err := resticClient.EnsureRepository(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ensure restic repository: %v", err)
	}

	// Create discovery client
	discoveryClient := discovery.NewDiscovery(k8sClient.GetClientset(), "local-pvc-backup", "default", config.BackupConfig.StoragePath, log)

	return &Manager{
		resticClient: resticClient,
		k8sClient:    k8sClient,
		discovery:    discoveryClient,
		storagePath:  config.BackupConfig.StoragePath,
		interval:     config.BackupConfig.BackupInterval,
		retention:    config.BackupConfig.Retention,
		log:          log,
	}, nil
}

// NewManagerWithClients creates a new backup manager with existing clients
func NewManagerWithClients(config *cfg.Config, k8sClient *k8s.Client, resticClient *restic.Client, log *logrus.Logger) (*Manager, error) {
	// Ensure restic repository is initialized
	if err := resticClient.EnsureRepository(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ensure restic repository: %v", err)
	}

	// Create discovery client
	discoveryClient := discovery.NewDiscovery(k8sClient.GetClientset(), "local-pvc-backup", "default", config.BackupConfig.StoragePath, log)

	return &Manager{
		resticClient: resticClient,
		k8sClient:    k8sClient,
		discovery:    discoveryClient,
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
	// Get backup-enabled PVCs on this node using discovery
	filter := discovery.FilterOptions{
		Node: m.k8sClient.GetNodeName(), // Only PVCs on this node
		All:  false,
	}
	
	pvcs, err := m.discovery.GetPVCsByFilter(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to get PVCs to backup: %v", err)
	}

	if len(pvcs) == 0 {
		m.log.Info("No PVCs to backup on this node")
		return nil
	}

	// Prepare backup paths and exclude patterns
	backupPaths := []string{}
	excludePatterns := []string{}

	// Add backup paths and exclude rules for each enabled PVC
	for _, pvc := range pvcs {
		// Skip PVCs that are not backup-enabled
		if !pvc.BackupEnabled {
			continue
		}
		
		m.log.Infof("Configuring backup for PVC %s/%s, include: %s, exclude: %s", pvc.Namespace, pvc.PVCName, pvc.Config.Include, pvc.Config.Exclude)

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

		// Execute backup for this PVC
		if err := m.resticClient.Backup(ctx, backupPaths, excludePatterns, pvc.UID, pvc.PVCName, pvc.Namespace); err != nil {
			return fmt.Errorf("failed to backup PVC %s/%s: %v", pvc.Namespace, pvc.PVCName, err)
		}

		// Reset paths and patterns for next PVC
		backupPaths = backupPaths[:0]
		excludePatterns = excludePatterns[:0]
	}

	// Clean up old backups using global retention policy
	if err := m.resticClient.Forget(ctx, m.retention); err != nil {
		m.log.Errorf("Error cleaning up old backups: %v", err)
	}

	return nil
}
