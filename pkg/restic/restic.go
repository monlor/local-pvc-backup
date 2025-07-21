package restic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Client represents a restic client
type Client struct {
	s3Endpoint  string
	s3Bucket    string
	s3Path      string
	s3AccessKey string
	s3SecretKey string
	s3Region    string
	password    string
	cachePath   string
	nodeName    string
	log         *logrus.Logger
}

// NewClient creates a new restic client
func NewClient(s3Endpoint, s3Bucket, s3Path, s3AccessKey, s3SecretKey, s3Region, password, cachePath, nodeName string, log *logrus.Logger) *Client {
	return &Client{
		s3Endpoint:  s3Endpoint,
		s3Bucket:    s3Bucket,
		s3Path:      s3Path,
		s3AccessKey: s3AccessKey,
		s3SecretKey: s3SecretKey,
		s3Region:    s3Region,
		password:    password,
		cachePath:   cachePath,
		nodeName:    nodeName,
		log:         log,
	}
}

// GetRepository returns the S3 repository URL
func (c *Client) GetRepository() string {
	if c.s3Path == "" {
		return fmt.Sprintf("s3:%s/%s/node-%s", c.s3Endpoint, c.s3Bucket, c.nodeName)
	}
	return fmt.Sprintf("s3:%s/%s/%s/node-%s", c.s3Endpoint, c.s3Bucket, c.s3Path, c.nodeName)
}

// getEnv returns the environment variables for restic
func (c *Client) getEnv() []string {
	return []string{
		fmt.Sprintf("RESTIC_PASSWORD=%s", c.password),
		fmt.Sprintf("RESTIC_CACHE_DIR=%s", c.cachePath),
		fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", c.s3AccessKey),
		fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", c.s3SecretKey),
		fmt.Sprintf("AWS_DEFAULT_REGION=%s", c.s3Region),
		fmt.Sprintf("TMPDIR=%s", c.cachePath),
	}
}

// InitRepository initializes a new restic repository
func (c *Client) InitRepository(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "restic", "init", "--repo", c.GetRepository())
	cmd.Env = append(os.Environ(), c.getEnv()...)
	c.log.Debugf("Executing command: restic init --repo %s", c.GetRepository())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to initialize repository: %v, output: %s", err, string(output))
	}
	return nil
}

// Backup performs a backup of the specified paths
func (c *Client) Backup(ctx context.Context, sourcePaths []string, excludePatterns []string, pvcID, pvcName, namespace string) error {
	args := []string{
		"backup",
		"--repo", c.GetRepository(),
		"--host", c.nodeName,
		"--tag", fmt.Sprintf("node=%s", c.nodeName),
		"--tag", fmt.Sprintf("pvc-id=%s", pvcID),
		"--tag", fmt.Sprintf("pvc-name=%s", pvcName),
		"--tag", fmt.Sprintf("namespace=%s", namespace),
	}

	// Add exclude patterns
	for _, pattern := range excludePatterns {
		if pattern != "" {
			args = append(args, "--exclude", pattern)
		}
	}

	// Add all source paths
	args = append(args, sourcePaths...)

	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = append(os.Environ(), c.getEnv()...)

	// Log the full command with all arguments
	c.log.Debugf("Executing command: restic %s", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to backup: %v, output: %s", err, string(output))
	}
	return nil
}

// Forget removes old snapshots according to the retention policy
func (c *Client) Forget(ctx context.Context, retention string) error {
	// Parse retention policy
	keepFlags := []string{}
	for _, policy := range strings.Split(retention, ",") {
		policy = strings.TrimSpace(policy)
		if policy == "" {
			continue
		}
		keepFlags = append(keepFlags, "--keep-within", policy)
	}

	if len(keepFlags) == 0 {
		return nil
	}

	args := []string{
		"forget",
		"--repo", c.GetRepository(),
		"--prune",
	}
	args = append(args, keepFlags...)

	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = append(os.Environ(), c.getEnv()...)

	// Log the full command with all arguments
	c.log.Debugf("Executing command: restic %s", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to forget old snapshots: %v, output: %s", err, string(output))
	}
	return nil
}

// Check verifies the repository
func (c *Client) Check(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "restic", "check", "--repo", c.GetRepository())
	cmd.Env = append(os.Environ(), c.getEnv()...)

	// Log the full command
	c.log.Debugf("Executing command: restic check --repo %s", c.GetRepository())

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("repository check failed: %v, output: %s", err, string(output))
	}
	return nil
}

// EnsureRepository ensures the repository exists and is accessible
func (c *Client) EnsureRepository(ctx context.Context) error {
	// Try to check the repository
	err := c.Check(ctx)
	if err != nil {
		c.log.Infof("Repository check failed, trying to initialize...")
		// If check fails, try to initialize
		return c.InitRepository(ctx)
	}
	return nil
}

// SnapshotInfo represents information about a restic snapshot
type SnapshotInfo struct {
	ID       string            `json:"id"`
	Time     time.Time         `json:"time"`
	Tree     string            `json:"tree"`
	Paths    []string          `json:"paths"`
	Hostname string            `json:"hostname"`
	Username string            `json:"username"`
	UID      int               `json:"uid"`
	GID      int               `json:"gid"`
	Tags     []string          `json:"tags"`
	Parent   string            `json:"parent,omitempty"`
	Summary  *SnapshotSummary  `json:"summary,omitempty"`
}

// SnapshotSummary represents the summary information of a snapshot
type SnapshotSummary struct {
	FilesNew            int   `json:"files_new"`
	FilesChanged        int   `json:"files_changed"`
	FilesUnmodified     int   `json:"files_unmodified"`
	DirsNew             int   `json:"dirs_new"`
	DirsChanged         int   `json:"dirs_changed"`
	DirsUnmodified      int   `json:"dirs_unmodified"`
	DataBlobs           int   `json:"data_blobs"`
	TreeBlobs           int   `json:"tree_blobs"`
	DataAdded           int64 `json:"data_added"`
	TotalFilesProcessed int   `json:"total_files_processed"`
	TotalBytesProcessed int64 `json:"total_bytes_processed"`
	TotalDuration       int64 `json:"total_duration"`
	SnapshotID          string `json:"snapshot_id"`
}

// ListSnapshots returns all snapshots in the repository
func (c *Client) ListSnapshots(ctx context.Context) ([]SnapshotInfo, error) {
	args := []string{
		"snapshots",
		"--repo", c.GetRepository(),
		"--json",
	}

	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = append(os.Environ(), c.getEnv()...)

	c.log.Debugf("Executing command: restic %s", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots: %v, output: %s", err, string(output))
	}

	var snapshots []SnapshotInfo
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return nil, fmt.Errorf("failed to parse snapshots JSON: %v", err)
	}

	return snapshots, nil
}

// ListSnapshotsByPVC returns snapshots for a specific PVC
func (c *Client) ListSnapshotsByPVC(ctx context.Context, pvcID string) ([]SnapshotInfo, error) {
	args := []string{
		"snapshots",
		"--repo", c.GetRepository(),
		"--json",
		"--tag", fmt.Sprintf("pvc-id=%s", pvcID),
	}

	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = append(os.Environ(), c.getEnv()...)

	c.log.Debugf("Executing command: restic %s", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots for PVC %s: %v, output: %s", pvcID, err, string(output))
	}

	var snapshots []SnapshotInfo
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return nil, fmt.Errorf("failed to parse snapshots JSON: %v", err)
	}

	return snapshots, nil
}

// FindSnapshotByTime finds the snapshot closest to (but not after) the target time
func (c *Client) FindSnapshotByTime(ctx context.Context, pvcID string, targetTime time.Time) (*SnapshotInfo, error) {
	snapshots, err := c.ListSnapshotsByPVC(ctx, pvcID)
	if err != nil {
		return nil, err
	}

	if len(snapshots) == 0 {
		return nil, fmt.Errorf("no snapshots found for PVC %s", pvcID)
	}

	// Find the snapshot with the latest time that is still before or equal to targetTime
	var closest *SnapshotInfo
	for i := range snapshots {
		snapshot := &snapshots[i]
		if snapshot.Time.Before(targetTime) || snapshot.Time.Equal(targetTime) {
			if closest == nil || snapshot.Time.After(closest.Time) {
				closest = snapshot
			}
		}
	}

	if closest == nil {
		return nil, fmt.Errorf("no snapshot found before time %s for PVC %s", targetTime.Format(time.RFC3339), pvcID)
	}

	return closest, nil
}

// RestoreSnapshot restores a specific snapshot to a target path
func (c *Client) RestoreSnapshot(ctx context.Context, snapshotID string, targetPath string) error {
	args := []string{
		"restore",
		snapshotID,
		"--repo", c.GetRepository(),
		"--target", targetPath,
	}

	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = append(os.Environ(), c.getEnv()...)

	c.log.Debugf("Executing command: restic %s", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restore snapshot %s: %v, output: %s", snapshotID, err, string(output))
	}

	return nil
}
