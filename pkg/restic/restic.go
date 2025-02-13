package restic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

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

// getRepository returns the S3 repository URL
func (c *Client) getRepository() string {
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
	cmd := exec.CommandContext(ctx, "restic", "init", "--repo", c.getRepository())
	cmd.Env = append(os.Environ(), c.getEnv()...)
	c.log.Debugf("Executing command: restic init --repo %s", c.getRepository())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to initialize repository: %v, output: %s", err, string(output))
	}
	return nil
}

// Backup performs a backup of the specified paths
func (c *Client) Backup(ctx context.Context, sourcePaths []string, excludePatterns []string) error {
	args := []string{
		"backup",
		"--repo", c.getRepository(),
		"--host", c.nodeName,
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
		"--repo", c.getRepository(),
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
	cmd := exec.CommandContext(ctx, "restic", "check", "--repo", c.getRepository())
	cmd.Env = append(os.Environ(), c.getEnv()...)

	// Log the full command
	c.log.Debugf("Executing command: restic check --repo %s", c.getRepository())

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
