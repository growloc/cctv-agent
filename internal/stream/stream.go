package stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cctv-agent/config"
	"github.com/cctv-agent/internal/logger"
)

// Stream represents a single camera stream
type Stream struct {
	camera     *config.CameraConfig
	config     *config.Config
	logger     logger.Logger
	cmd        *exec.Cmd
	status     StreamStatus
	statusMu   sync.RWMutex
	cancelFunc context.CancelFunc
	startTime  time.Time
	lastError  error
}

// NewStream creates a new stream instance
func NewStream(camera *config.CameraConfig, cfg *config.Config, log logger.Logger) *Stream {
	return &Stream{
		camera: camera,
		config: cfg,
		logger: log,
		status: StatusDisconnected,
	}
}

// Start starts the stream
func (s *Stream) Start(ctx context.Context) error {
	s.statusMu.Lock()
	if s.status == StatusConnected || s.status == StatusConnecting {
		s.statusMu.Unlock()
		return fmt.Errorf("stream already running")
	}
	s.status = StatusConnecting
	s.statusMu.Unlock()

	// Create context for this stream
	streamCtx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	// Build FFmpeg command
	cmd := s.buildFFmpegCommand(streamCtx)
	s.cmd = cmd

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.setStatus(StatusError)
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.setStatus(StatusError)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start FFmpeg process
	s.logger.Info("Starting FFmpeg stream", "camera_id", s.camera.ID)
	if err := cmd.Start(); err != nil {
		s.setStatus(StatusError)
		s.logger.Error("Failed to start FFmpeg process", "camera_id", s.camera.ID, "error", err, "command", "ffmpeg "+strings.Join(cmd.Args[1:], " "))
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	s.startTime = time.Now()
	s.setStatus(StatusConnected)

	// Monitor stdout in goroutine
	go s.monitorOutput(stdout, "stdout")

	// Monitor stderr in goroutine
	go s.monitorOutput(stderr, "stderr")

	// Wait for process to complete
	err = cmd.Wait()

	s.setStatus(StatusDisconnected)

	if err != nil {
		if streamCtx.Err() == context.Canceled {
			s.logger.Info("Stream stopped by context cancellation", "camera_id", s.camera.ID)
			return nil
		}
		s.lastError = err
		if exitError, ok := err.(*exec.ExitError); ok {
			s.logger.Error("FFmpeg process exited with error", "camera_id", s.camera.ID, "error", err, "exit_code", exitError.ExitCode())
		} else {
			s.logger.Error("FFmpeg process exited with error", "camera_id", s.camera.ID, "error", err)
		}
		return fmt.Errorf("FFmpeg process exited: %w", err)
	}

	return nil
}

// Stop stops the stream
func (s *Stream) Stop() {
	s.logger.Info("Stopping stream", "camera_id", s.camera.ID)

	if s.cancelFunc != nil {
		s.cancelFunc()
	}

	if s.cmd != nil && s.cmd.Process != nil {
		// Give FFmpeg time to exit gracefully
		time.Sleep(2 * time.Second)

		// Force kill if still running
		if s.cmd.ProcessState == nil {
			s.logger.Warn("Force killing FFmpeg process", "camera_id", s.camera.ID)
			s.cmd.Process.Kill()
		}
	}

	s.setStatus(StatusDisconnected)
}

// buildFFmpegCommand builds the FFmpeg command
func (s *Stream) buildFFmpegCommand(ctx context.Context) *exec.Cmd {
	rtmpURL := fmt.Sprintf("rtmp://%s:%d/%s/%s",
		s.config.RTMP.Host,
		s.config.RTMP.Port,
		s.config.RTMP.AppName,
		s.camera.StreamID,
	)

	args := []string{}

	// Add log level first
	args = append(args, "-loglevel", "warning")

	// Add RTSP transport options
	args = append(args, "-rtsp_transport", "tcp")
	args = append(args, "-rtsp_flags", "+prefer_tcp")

	// Add input source
	args = append(args, "-i", s.camera.RTSPUrl)

	// Add video encoding options
	args = append(args,
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-b:v", "800k",
		"-maxrate", "800k",
		"-bufsize", "1600k",
		"-g", "30",
		"-keyint_min", "30",
	)

	// Add video filter for scaling and fps
	args = append(args, "-vf", "scale=640:-1,fps=15")

	// Add audio encoding options
	args = append(args,
		"-c:a", "aac",
		"-b:a", "64k",
		"-ar", "22050",
	)

	// Add audio filter for resampling
	args = append(args, "-af", "aresample=async=1:first_pts=0")

	// Add flags for handling corrupt data
	args = append(args, "-fflags", "+discardcorrupt")

	// Add output format and destination
	args = append(args, "-f", "flv", rtmpURL)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	s.logger.Debug("FFmpeg command", "full_command", "ffmpeg "+strings.Join(args, " "))

	return cmd
}

// monitorOutput monitors FFmpeg output
func (s *Stream) monitorOutput(pipe io.ReadCloser, source string) {
	defer pipe.Close()

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()

		// Log based on content
		if strings.Contains(line, "error") || strings.Contains(line, "Error") {
			s.logger.Error("FFmpeg error", "camera_id", s.camera.ID, "source", source, "message", line)
		} else if strings.Contains(line, "warning") || strings.Contains(line, "Warning") {
			s.logger.Warn("FFmpeg warning", "camera_id", s.camera.ID, "source", source, "message", line)
		} else {
			s.logger.Debug("FFmpeg output", "camera_id", s.camera.ID, "source", source, "message", line)
		}
	}

	if err := scanner.Err(); err != nil {
		s.logger.Error("Error reading FFmpeg output", "camera_id", s.camera.ID, "source", source, "error", err)
	}
}

// GetStatus returns the current stream status
func (s *Stream) GetStatus() StreamStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.status
}

// setStatus sets the stream status
func (s *Stream) setStatus(status StreamStatus) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status = status
}

// GetUptime returns the stream uptime
func (s *Stream) GetUptime() time.Duration {
	if s.startTime.IsZero() {
		return 0
	}
	return time.Since(s.startTime)
}

// GetLastError returns the last error
func (s *Stream) GetLastError() error {
	return s.lastError
}

// IsRunning checks if the stream is running
func (s *Stream) IsRunning() bool {
	status := s.GetStatus()
	return status == StatusConnected || status == StatusConnecting
}
