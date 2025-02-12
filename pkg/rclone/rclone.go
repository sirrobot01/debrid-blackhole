package rclone

import (
	"bufio"
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/pkg/webdav"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Remote struct {
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Url        string            `json:"url"`
	MountPoint string            `json:"mount_point"`
	Flags      map[string]string `json:"flags"`
}

func (rc *Rclone) Config() string {
	var content string

	for _, remote := range rc.Remotes {
		content += fmt.Sprintf("[%s]\n", remote.Name)
		content += fmt.Sprintf("type = %s\n", remote.Type)
		content += fmt.Sprintf("url = %s\n", remote.Url)
		content += fmt.Sprintf("vendor = other\n")

		for key, value := range remote.Flags {
			content += fmt.Sprintf("%s = %s\n", key, value)
		}
		content += "\n\n"
	}

	return content
}

type Rclone struct {
	Remotes    map[string]Remote `json:"remotes"`
	logger     zerolog.Logger
	cmd        *exec.Cmd
	configPath string
}

func New(webdav *webdav.WebDav) (*Rclone, error) {
	// Check if rclone is installed
	cfg := config.GetConfig()
	configPath := fmt.Sprintf("%s/rclone.conf", cfg.Path)

	if _, err := exec.LookPath("rclone"); err != nil {
		return nil, fmt.Errorf("rclone is not installed: %w", err)
	}
	remotes := make(map[string]Remote)
	for _, handler := range webdav.Handlers {
		url := fmt.Sprintf("http://localhost:%s/webdav/%s/", cfg.QBitTorrent.Port, strings.ToLower(handler.Name))
		rmt := Remote{
			Type:       "webdav",
			Name:       handler.Name,
			Url:        url,
			MountPoint: filepath.Join("/mnt/rclone/", handler.Name),
			Flags:      map[string]string{},
		}
		remotes[handler.Name] = rmt
	}

	rc := &Rclone{
		logger:     logger.NewLogger("rclone", "info", os.Stdout),
		Remotes:    remotes,
		configPath: configPath,
	}
	if err := rc.WriteConfig(); err != nil {
		return nil, err
	}
	return rc, nil
}

func (rc *Rclone) WriteConfig() error {

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(rc.configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write the config file
	if err := os.WriteFile(rc.configPath, []byte(rc.Config()), 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	rc.logger.Info().Msgf("Wrote rclone config with %d remotes to %s", len(rc.Remotes), rc.configPath)
	return nil
}

func (rc *Rclone) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	errChan := make(chan error)
	for _, remote := range rc.Remotes {
		wg.Add(1)
		go func(remote Remote) {
			defer wg.Done()
			if err := rc.Mount(ctx, &remote); err != nil {
				rc.logger.Error().Err(err).Msgf("failed to mount %s", remote.Name)
				select {
				case errChan <- err:
				default:
				}
			}
		}(remote)
	}
	return <-errChan
}

func (rc *Rclone) testConnection(ctx context.Context, remote *Remote) error {
	testArgs := []string{
		"ls",
		"--config", rc.configPath,
		"--log-level", "DEBUG",
		remote.Name + ":",
	}

	cmd := exec.CommandContext(ctx, "rclone", testArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		rc.logger.Error().Err(err).Str("output", string(output)).Msg("Connection test failed")
		return fmt.Errorf("connection test failed: %w", err)
	}

	rc.logger.Info().Msg("Connection test successful")
	return nil
}

func (rc *Rclone) Mount(ctx context.Context, remote *Remote) error {
	// Ensure the mount point directory exists
	if err := os.MkdirAll(remote.MountPoint, 0755); err != nil {
		rc.logger.Info().Err(err).Msgf("failed to create mount point directory: %s", remote.MountPoint)
		return err
	}

	//if err := rc.testConnection(ctx, remote); err != nil {
	//	return err
	//}

	// Basic arguments
	args := []string{
		"mount",
		remote.Name + ":",
		remote.MountPoint,
		"--config", rc.configPath,
		"--vfs-cache-mode", "full",
		"--log-level", "DEBUG", // Keep this, remove -vv
		"--allow-other",         // Keep this
		"--allow-root",          // Add this
		"--default-permissions", // Add this
		"--vfs-cache-max-age", "24h",
		"--timeout", "1m",
		"--transfers", "4",
		"--buffer-size", "32M",
	}

	// Add any additional flags
	for key, value := range remote.Flags {
		args = append(args, "--"+key, value)
	}

	// Create command
	rc.cmd = exec.CommandContext(ctx, "rclone", args...)

	// Set up pipes for stdout and stderr
	stdout, err := rc.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := rc.cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the command
	if err := rc.cmd.Start(); err != nil {
		return err
	}

	// Channel to signal mount success
	mountReady := make(chan bool)
	mountError := make(chan error)

	// Monitor stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			text := scanner.Text()
			rc.logger.Info().Msg("stdout: " + text)
			if strings.Contains(text, "Mount succeeded") {
				mountReady <- true
				return
			}
		}
	}()

	// Monitor stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			text := scanner.Text()
			rc.logger.Info().Msg("stderr: " + text)
			if strings.Contains(text, "error") {
				mountError <- fmt.Errorf("mount error: %s", text)
				return
			}
		}
	}()

	// Wait for mount with timeout
	select {
	case <-mountReady:
		rc.logger.Info().Msgf("Successfully mounted %s at %s", remote.Name, remote.MountPoint)
		return nil
	case err := <-mountError:
		err = rc.cmd.Process.Kill()
		if err != nil {
			return err
		}
		return err
	case <-ctx.Done():
		err := rc.cmd.Process.Kill()
		if err != nil {
			return err
		}
		return ctx.Err()
	case <-time.After(30 * time.Second):
		err := rc.cmd.Process.Kill()
		if err != nil {
			return err
		}
		return fmt.Errorf("mount timeout after 30 seconds")
	}
}

func (rc *Rclone) Unmount(ctx context.Context, remote *Remote) error {
	if rc.cmd != nil && rc.cmd.Process != nil {
		// First try graceful shutdown
		if err := rc.cmd.Process.Signal(os.Interrupt); err != nil {
			rc.logger.Warn().Err(err).Msg("failed to send interrupt signal")
		}

		// Wait for a bit to allow graceful shutdown
		done := make(chan error)
		go func() {
			done <- rc.cmd.Wait()
		}()

		select {
		case err := <-done:
			if err != nil {
				rc.logger.Warn().Err(err).Msg("process exited with error")
			}
		case <-time.After(5 * time.Second):
			// Force kill if it doesn't shut down gracefully
			if err := rc.cmd.Process.Kill(); err != nil {
				rc.logger.Error().Err(err).Msg("failed to kill process")
				return err
			}
		}
	}

	// Use fusermount to ensure the mountpoint is unmounted
	cmd := exec.CommandContext(ctx, "fusermount", "-u", remote.MountPoint)
	if err := cmd.Run(); err != nil {
		rc.logger.Warn().Err(err).Msg("fusermount unmount failed")
		// Don't return error here as the process might already be dead
	}

	rc.logger.Info().Msgf("Successfully unmounted %s", remote.MountPoint)
	return nil
}
