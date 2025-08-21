package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cctv-agent/internal/logger"
	"github.com/hashicorp/go-version"
)

// Updater handles OTA updates
type Updater struct {
	logger         logger.Logger
	currentVersion string
	binaryPath     string
}

// UpdateInfo contains update information
type UpdateInfo struct {
	Version      string `json:"version"`
	DownloadURL  string `json:"download_url"`
	Checksum     string `json:"checksum"`
	ReleaseNotes string `json:"release_notes"`
	Force        bool   `json:"force"`
}

// NewUpdater creates a new updater instance
func NewUpdater(log logger.Logger, currentVersion string) *Updater {
	binaryPath, _ := os.Executable()

	return &Updater{
		logger:         log,
		currentVersion: currentVersion,
		binaryPath:     binaryPath,
	}
}

// CheckForUpdate checks if an update is available
func (u *Updater) CheckForUpdate(info UpdateInfo) (bool, error) {
	u.logger.Info("Checking for updates",
		"current_version", u.currentVersion,
		"available_version", info.Version)

	// Parse versions
	current, err := version.NewVersion(u.currentVersion)
	if err != nil {
		return false, fmt.Errorf("failed to parse current version: %w", err)
	}

	available, err := version.NewVersion(info.Version)
	if err != nil {
		return false, fmt.Errorf("failed to parse available version: %w", err)
	}

	// Compare versions
	if available.GreaterThan(current) || info.Force {
		u.logger.Info("Update available",
			"current", u.currentVersion,
			"available", info.Version,
			"force", info.Force)
		return true, nil
	}

	u.logger.Info("No update needed", "current", u.currentVersion)
	return false, nil
}

// PerformUpdate performs the update
func (u *Updater) PerformUpdate(info UpdateInfo) error {
	u.logger.Info("Starting update process", "version", info.Version)

	// Create temp directory for download
	tempDir, err := os.MkdirTemp("", "cctv-agent-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Download new binary
	tempBinary := filepath.Join(tempDir, "cctv-agent-new")
	if err := u.downloadBinary(info.DownloadURL, tempBinary); err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}

	// Verify checksum
	if info.Checksum != "" {
		if err := u.verifyChecksum(tempBinary, info.Checksum); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Make binary executable
	if err := os.Chmod(tempBinary, 0755); err != nil {
		return fmt.Errorf("failed to set executable permission: %w", err)
	}

	// Backup current binary
	backupPath := u.binaryPath + ".backup"
	if err := u.backupBinary(backupPath); err != nil {
		u.logger.Warn("Failed to backup current binary", "error", err)
	}

	// Replace binary
	if err := u.replaceBinary(tempBinary); err != nil {
		// Restore backup if replacement failed
		if _, err := os.Stat(backupPath); err == nil {
			u.restoreBinary(backupPath)
		}
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	u.logger.Info("Update completed successfully", "version", info.Version)

	// Schedule restart
	u.scheduleRestart()

	return nil
}

// downloadBinary downloads the new binary
func (u *Updater) downloadBinary(url, destination string) error {
	u.logger.Info("Downloading update", "url", url)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Create destination file
	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy content
	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	u.logger.Info("Download completed", "bytes", written)

	return nil
}

// verifyChecksum verifies the checksum of the downloaded file
func (u *Updater) verifyChecksum(filepath, expectedChecksum string) error {
	u.logger.Info("Verifying checksum")

	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	actualChecksum := hex.EncodeToString(hash.Sum(nil))

	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s",
			expectedChecksum, actualChecksum)
	}

	u.logger.Info("Checksum verified successfully")
	return nil
}

// backupBinary creates a backup of the current binary
func (u *Updater) backupBinary(backupPath string) error {
	u.logger.Info("Creating backup", "path", backupPath)

	source, err := os.Open(u.binaryPath)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer source.Close()

	destination, err := os.Create(backupPath)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	// Preserve permissions
	sourceInfo, _ := source.Stat()
	os.Chmod(backupPath, sourceInfo.Mode())

	return nil
}

// replaceBinary replaces the current binary with the new one
func (u *Updater) replaceBinary(newBinary string) error {
	u.logger.Info("Replacing binary")

	// RegisterEventHandler Windows, we need to rename the old binary first
	if runtime.GOOS == "windows" {
		tempPath := u.binaryPath + ".old"
		if err := os.Rename(u.binaryPath, tempPath); err != nil {
			return fmt.Errorf("failed to rename old binary: %w", err)
		}
		defer os.Remove(tempPath)
	}

	// Copy new binary to destination
	source, err := os.Open(newBinary)
	if err != nil {
		return fmt.Errorf("failed to open new binary: %w", err)
	}
	defer source.Close()

	destination, err := os.Create(u.binaryPath)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Preserve permissions
	sourceInfo, _ := source.Stat()
	os.Chmod(u.binaryPath, sourceInfo.Mode())

	return nil
}

// restoreBinary restores the backup binary
func (u *Updater) restoreBinary(backupPath string) error {
	u.logger.Info("Restoring backup")

	source, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup: %w", err)
	}
	defer source.Close()

	destination, err := os.Create(u.binaryPath)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	// Preserve permissions
	sourceInfo, _ := source.Stat()
	os.Chmod(u.binaryPath, sourceInfo.Mode())

	return nil
}

// scheduleRestart schedules a restart of the service
func (u *Updater) scheduleRestart() {
	u.logger.Info("Scheduling restart in 5 seconds")

	go func() {
		time.Sleep(5 * time.Second)

		// Try to restart via systemd
		if runtime.GOOS == "linux" {
			cmd := exec.Command("systemctl", "restart", "cctv-agent")
			if err := cmd.Run(); err != nil {
				u.logger.Error("Failed to restart via systemd", "error", err)
				// Fall back to exit
				os.Exit(0)
			}
		} else {
			// RegisterEventHandler other systems, just exit and let the supervisor restart
			os.Exit(0)
		}
	}()
}

// GetCurrentVersion returns the current version
func (u *Updater) GetCurrentVersion() string {
	return u.currentVersion
}

// SetBinaryPath sets the binary path (for testing)
func (u *Updater) SetBinaryPath(path string) {
	u.binaryPath = path
}
