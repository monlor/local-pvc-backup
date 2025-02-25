package config

import (
	"time"
)

// Config represents the main configuration for the backup service
type Config struct {
	S3Config     S3Config     `envPrefix:"S3_"`
	BackupConfig BackupConfig `envPrefix:"BACKUP_"`
	ResticConfig ResticConfig `envPrefix:"RESTIC_"`
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

// Annotations for backup configuration
const (
	// Base annotation prefix
	AnnotationPrefix = "backup.local-pvc.io"

	// Specific annotations
	AnnotationEnabled = AnnotationPrefix + "/enabled"
	AnnotationInclude = AnnotationPrefix + "/include"
	AnnotationExclude = AnnotationPrefix + "/exclude"
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
