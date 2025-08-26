package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cctv-agent/config"
	"github.com/cctv-agent/internal/logger"
	"github.com/cctv-agent/internal/socketio"
	"github.com/hashicorp/go-version"
)

// Updater handles OTA updates
type Updater struct {
	logger         logger.Logger
	currentVersion string
	binaryPath     string
	opts           config.UpdaterConfig
	sioClient      *socketio.Client
	responseMap    map[string]chan *UpdateCheckResponse
	responseMu     sync.RWMutex
}

// RunPeriodic starts a background loop to periodically check and apply updates based on options
func (u *Updater) RunPeriodic(ctx context.Context) {
	if !u.opts.Enabled {
		u.logger.Info("Updater disabled; not starting periodic loop")
		return
	}
	// small jitter to spread load
	jitter := time.Duration(1+time.Now().UnixNano()%10) * time.Second
	timer := time.NewTimer(jitter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := u.checkAndMaybeUpdate(ctx); err != nil {
				u.logger.Error("Update cycle error", "error", err)
			}
			interval := u.opts.Interval
			if interval <= 0 {
				interval = 2 * time.Hour
			}
			j := time.Duration(float64(interval) * 0.1)
			next := interval
			if j > 0 {
				next += time.Duration(time.Now().UnixNano() % int64(j))
			}
			timer.Reset(next)
		}
	}
}

func (u *Updater) checkAndMaybeUpdate(ctx context.Context) error {
	m, err := u.fetchManifest(ctx)
	if err != nil {
		if u.opts.URL == "" { // no fallback URL
			return err
		}
		// fabricate manifest from direct URL
		m = &Manifest{Version: u.currentVersion, URL: u.opts.URL, OS: runtime.GOOS, Arch: runtime.GOARCH, Channel: u.opts.Channel}
	}

	// channel filter
	if m.Channel != "" && u.opts.Channel != "" && !strings.EqualFold(m.Channel, u.opts.Channel) {
		u.logger.Info("Skipping manifest due to channel mismatch", "manifest", m.Channel, "desired", u.opts.Channel)
		return nil
	}
	// os/arch filter
	if (m.OS != "" && m.OS != runtime.GOOS) || (m.Arch != "" && m.Arch != runtime.GOARCH) {
		u.logger.Warn("Skipping manifest due to os/arch mismatch", "os", m.OS, "arch", m.Arch)
		return nil
	}

	need, err := u.needUpdate(m.Version)
	if err != nil {
		return err
	}
	if !need {
		u.logger.Info("No update available", "current", u.currentVersion)
		return nil
	}

	// download to updates dir
	updatesDir := filepath.Join(u.opts.BaseDir, "updates")
	if err := os.MkdirAll(updatesDir, 0o755); err != nil {
		return fmt.Errorf("mkdir updates: %w", err)
	}
	staging := filepath.Join(updatesDir, m.Version+".partial")
	final := filepath.Join(updatesDir, m.Version)
	if err := u.downloadWithResume(ctx, m.URL, staging); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return fmt.Errorf("finalize download: %w", err)
	}
	if m.SHA256 != "" {
		if err := u.verifyChecksum(final, m.SHA256); err != nil {
			return err
		}
	}
	if err := u.installRelease(final, m.Version); err != nil {
		return fmt.Errorf("install release: %w", err)
	}
	u.scheduleRestart()
	return nil
}

func (u *Updater) fetchManifest(ctx context.Context) (*Manifest, error) {
	// Use SocketIO client if available, otherwise fall back to HTTP
	if u.sioClient != nil && u.sioClient.IsConnected() {
		return u.fetchManifestViaSocketIO(ctx)
	}

	return nil, errors.New("SocketIO client error")
}

// fetchManifestViaSocketIO fetches update information via SocketIO
func (u *Updater) fetchManifestViaSocketIO(ctx context.Context) (*Manifest, error) {
	u.logger.Info("Checking for updates via SocketIO", "current_version", u.currentVersion)

	// Create a unique request ID and response channel
	requestID := fmt.Sprintf("update_check_%d", time.Now().UnixNano())
	responseCh := make(chan *UpdateCheckResponse, 1)

	u.responseMu.Lock()
	u.responseMap[requestID] = responseCh
	u.responseMu.Unlock()

	// Clean up on function exit
	defer func() {
		u.responseMu.Lock()
		delete(u.responseMap, requestID)
		u.responseMu.Unlock()
		close(responseCh)
	}()

	// Send update check request
	request := UpdateCheckRequest{
		CurrentVersion: u.currentVersion,
	}

	if err := u.sioClient.Emit("is_update_available", request); err != nil {
		return nil, fmt.Errorf("failed to send update check request: %w", err)
	}

	// Wait for response with timeout
	timeout := 30 * time.Second
	select {
	case response := <-responseCh:
		if response == nil {
			return nil, errors.New("received nil update check response")
		}

		if !response.UpdateAvailable {
			u.logger.Info("No update available via SocketIO")
			return nil, errors.New("no update available")
		}

		// Convert response to Manifest format
		manifest := &Manifest{
			Version: response.NewVersion,
			URL:     response.DownloadURL,
			SHA256:  response.Checksum,
			OS:      runtime.GOOS,
			Arch:    runtime.GOARCH,
			Channel: u.opts.Channel,
		}

		u.logger.Info("Update available via SocketIO",
			"current_version", u.currentVersion,
			"new_version", response.NewVersion)

		return manifest, nil

	case <-ctx.Done():
		return nil, ctx.Err()

	case <-time.After(timeout):
		return nil, errors.New("timeout waiting for update check response")
	}
}

func (u *Updater) needUpdate(avail string) (bool, error) {
	if avail == "" {
		return false, nil
	}
	cur, err := version.NewVersion(u.currentVersion)
	if err != nil {
		return false, fmt.Errorf("parse current version: %w", err)
	}
	av, err := version.NewVersion(avail)
	if err != nil {
		return false, fmt.Errorf("parse available version: %w", err)
	}
	if av.GreaterThan(cur) {
		return true, nil
	}
	if u.opts.AllowDowngrade && av.LessThan(cur) {
		return true, nil
	}
	return false, nil
}

func (u *Updater) downloadWithResume(ctx context.Context, url, dest string) error {
	// simple full download (resume can be added later)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	cli := &http.Client{Timeout: 10 * time.Minute}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download http %d", resp.StatusCode)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return os.Chmod(dest, 0o755)
}

func (u *Updater) installRelease(artifactPath, versionStr string) error {
	base := u.opts.BaseDir
	releases := filepath.Join(base, "releases", versionStr)
	if err := os.MkdirAll(releases, 0o755); err != nil {
		return err
	}
	targetBin := filepath.Join(releases, "cctv-agent")
	if err := copyFile(artifactPath, targetBin); err != nil {
		return err
	}
	if err := os.Chmod(targetBin, 0o755); err != nil {
		return err
	}
	current := filepath.Join(base, "current")
	tmp := filepath.Join(base, ".current.tmp")
	_ = os.Remove(tmp)
	if err := os.Symlink(targetBin, tmp); err != nil {
		return fmt.Errorf("create tmp symlink: %w", err)
	}
	if err := os.Rename(tmp, current); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename symlink: %w", err)
	}
	u.pruneOldReleases(filepath.Join(base, "releases"))
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func (u *Updater) pruneOldReleases(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if len(entries) <= u.opts.KeepReleases {
		return
	}
	// naive pruning: remove oldest lexicographically
	toDelete := len(entries) - u.opts.KeepReleases
	for i := 0; i < toDelete; i++ {
		_ = os.RemoveAll(filepath.Join(dir, entries[i].Name()))
	}
}

// UpdateInfo contains update information
type UpdateInfo struct {
	Version      string `json:"version"`
	DownloadURL  string `json:"download_url"`
	Checksum     string `json:"checksum"`
	ReleaseNotes string `json:"release_notes"`
	Force        bool   `json:"force"`
}

// UpdateCheckRequest represents the request to check for updates via SocketIO
type UpdateCheckRequest struct {
	CurrentVersion string `json:"current_version"`
}

// UpdateCheckResponse represents the response from update check via SocketIO
type UpdateCheckResponse struct {
	NewVersion      string `json:"newVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	DownloadURL     string `json:"downloadURL,omitempty"`
	Checksum        string `json:"checksum,omitempty"`
}

// NewUpdater creates a new updater instance
func NewUpdater(log logger.Logger, currentVersion string) *Updater {
	binaryPath, _ := os.Executable()

	return &Updater{
		logger:         log,
		currentVersion: currentVersion,
		binaryPath:     binaryPath,
		responseMap:    make(map[string]chan *UpdateCheckResponse),
		opts: config.UpdaterConfig{ // sensible defaults; can be overridden via ApplyConfig
			Enabled:        true,
			BaseDir:        "/opt/cctv-agent",
			ServiceName:    "cctv-agent",
			Interval:       2 * time.Hour,
			KeepReleases:   3,
			HealthTimeout:  30 * time.Second,
			Channel:        "stable",
			AllowDowngrade: false,
		},
	}
}

// ApplyConfig sets runtime options from config.UpdaterConfig, filling in sane fallbacks
func (u *Updater) ApplyConfig(o config.UpdaterConfig) {
	if o.BaseDir == "" {
		o.BaseDir = u.opts.BaseDir
	}
	if o.ServiceName == "" {
		o.ServiceName = u.opts.ServiceName
	}
	if o.Interval == 0 {
		o.Interval = u.opts.Interval
	}
	if o.KeepReleases <= 0 {
		o.KeepReleases = u.opts.KeepReleases
	}
	if o.HealthTimeout == 0 {
		o.HealthTimeout = u.opts.HealthTimeout
	}
	if o.Channel == "" {
		o.Channel = u.opts.Channel
	}
	u.opts = o
}

// SetSocketIOClient sets the SocketIO client for update checks
func (u *Updater) SetSocketIOClient(client *socketio.Client) {
	u.sioClient = client
	// Register handler for update check responses
	if client != nil {
		client.RegisterEventHandler("update_check_response", u.handleUpdateCheckResponse)
	}
}

// handleUpdateCheckResponse handles update check responses from SocketIO
func (u *Updater) handleUpdateCheckResponse(data json.RawMessage) error {
	var response UpdateCheckResponse
	if err := json.Unmarshal(data, &response); err != nil {
		u.logger.Error("Failed to unmarshal update check response", "error", err)
		return err
	}

	// Find the waiting channel and send the response
	u.responseMu.Lock()
	defer u.responseMu.Unlock()

	for key, ch := range u.responseMap {
		select {
		case ch <- &response:
			delete(u.responseMap, key)
			return nil
		default:
			// Channel is not ready, continue to next
		}
	}

	u.logger.Warn("Received update check response but no waiting channel found")
	return nil
}

// Manifest describes update metadata hosted remotely
type Manifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Channel string `json:"channel"`
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
	u.logger.Info("Scheduling service restart in 5 seconds", "service", u.opts.ServiceName)

	go func() {
		time.Sleep(5 * time.Second)

		// Try to restart via systemd
		if runtime.GOOS == "linux" {
			svc := u.opts.ServiceName
			if svc == "" {
				svc = "cctv-agent"
			}
			cmd := exec.Command("systemctl", "restart", svc)
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

// HandleStartup should be called on process start to finalize pending updates
func (u *Updater) HandleStartup() {
	// For now, just log successful start; further health checks can be added
	u.logger.Info("Updater startup check complete", "version", u.currentVersion)
}

// GetCurrentVersion returns the current version
func (u *Updater) GetCurrentVersion() string {
	return u.currentVersion
}

// SetBinaryPath sets the binary path (for testing)
func (u *Updater) SetBinaryPath(path string) {
	u.binaryPath = path
}

// GetSocketIOClient returns the SocketIO client
func (u *Updater) GetSocketIOClient() *socketio.Client {
	return u.sioClient
}
