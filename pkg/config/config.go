package config

import (
	"time"
)

// Config represents the main configuration for the backup service
type Config struct {
	S3Config         S3Config         `envPrefix:"S3_"`
	BackupConfig     BackupConfig     `envPrefix:"BACKUP_"`
	ResticConfig     ResticConfig     `envPrefix:"RESTIC_"`
	KubernetesConfig KubernetesConfig `envPrefix:"KUBERNETES_"`
}

// S3Config holds the S3 storage configuration
type S3Config struct {
	Endpoint  string `env:"ENDPOINT,required"`
	Bucket    string `env:"BUCKET,required"`
	AccessKey string `env:"ACCESS_KEY,required"`
	SecretKey string `env:"SECRET_KEY,required"`
	Region    string `env:"REGION,required"`
	Path      string `env:"PATH" envDefault:""` // S3 存储路径前缀
}

// ResticConfig holds the restic configuration
type ResticConfig struct {
	Password  string `env:"PASSWORD,required"` // 用于加密的密码
	CachePath string `env:"CACHE_PATH" envDefault:"/var/cache/restic"`
}

// BackupConfig holds the backup configuration
type BackupConfig struct {
	StoragePath    string        `env:"STORAGE_PATH" envDefault:"/data"`
	LogLevel       string        `env:"LOG_LEVEL" envDefault:"info"`
	BackupInterval time.Duration `env:"INTERVAL" envDefault:"1h"`   // Backup interval
	Retention      string        `env:"RETENTION" envDefault:"14d"` // Retention policy: keep backups within 7 days, 30 days, and 365 days
}

// KubernetesConfig holds the Kubernetes-related configuration
type KubernetesConfig struct {
	PodNamespace    string `env:"KUBERNETES_NAMESPACE" envDefault:"default"`          // Current pod's namespace (from KUBERNETES_POD_NAMESPACE)
	DaemonSetName   string `env:"DAEMONSET_NAME" envDefault:"local-pvc-backup"`       // DaemonSet name for cross-node communication
	DaemonSetLabel  string `env:"DAEMONSET_LABEL" envDefault:"app.kubernetes.io/name"` // Label name for DaemonSet pods
}

// Labels and Annotations for backup configuration
const (
	// Base prefix
	Prefix = "backup.local-pvc.io"

	// PVC Labels (for enabled flag - allows efficient K8s API filtering)
	LabelEnabled = Prefix + "/enabled"

	// PVC Annotations (for detailed configuration)
	AnnotationInclude = Prefix + "/include"
	AnnotationExclude = Prefix + "/exclude"
)

// PVCBackupConfig represents the backup configuration for a specific PVC
type PVCBackupConfig struct {
	Enabled bool
	Include string
	Exclude string
}

// DefaultPVCBackupConfig returns the default backup configuration
func DefaultPVCBackupConfig() PVCBackupConfig {
	return PVCBackupConfig{
		Enabled: false,
		Include: "",
		Exclude: "",
	}
}
