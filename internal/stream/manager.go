package stream

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cctv-agent/config"
	"github.com/cctv-agent/internal/logger"
	"golang.org/x/sync/errgroup"
)

// StreamStatus represents the status of a stream
type StreamStatus string

const (
	StatusDisconnected StreamStatus = "disconnected"
	StatusConnecting   StreamStatus = "connecting"
	StatusConnected    StreamStatus = "connected"
	StatusError        StreamStatus = "error"
	StatusReconnecting StreamStatus = "reconnecting"
)

// StatusUpdate represents a stream status update
type StatusUpdate struct {
	CameraID  string
	Status    StreamStatus
	Error     string
	Timestamp time.Time
}

// Manager manages multiple camera streams
type Manager struct {
	config       *config.Config
	logger       logger.Logger
	streams      map[string]*Stream
	statusChan   chan StatusUpdate
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	eg           *errgroup.Group
	maxRetries   int
	retryDelay   time.Duration
}

// NewManager creates a new stream manager
func NewManager(cfg *config.Config, log logger.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	eg, egCtx := errgroup.WithContext(ctx)
	
	return &Manager{
		config:     cfg,
		logger:     log,
		streams:    make(map[string]*Stream),
		statusChan: make(chan StatusUpdate, 100),
		ctx:        egCtx,
		cancel:     cancel,
		eg:         eg,
		maxRetries: 3,
		retryDelay: 5 * time.Second,
	}
}

// Start starts all enabled camera streams
func (m *Manager) Start() error {
	m.logger.Info("Starting stream manager")
	
	cameras := m.config.GetEnabledCameras()
	if len(cameras) == 0 {
		m.logger.Warn("No enabled cameras found")
		return nil
	}
	
	// Create semaphore for concurrency control
	sem := make(chan struct{}, m.config.Agent.MaxConcurrency)
	
	for _, camera := range cameras {
		cam := camera // Capture loop variable
		
		// Create stream instance
		stream := NewStream(&cam, m.config, m.logger.With("camera_id", cam.ID))
		
		m.mu.Lock()
		m.streams[cam.ID] = stream
		m.mu.Unlock()
		
		// Start stream in independent goroutine (not using errgroup)
		go func(s *Stream) {
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore
			
			// Run stream with retry in isolation
			for {
				select {
				case <-m.ctx.Done():
					return
				default:
				}
				
				err := m.runStreamWithRetry(s)
				if err != nil {
					// Log the error but don't let it affect other streams
					m.logger.Error("Stream permanently failed", 
						"camera_id", s.camera.ID, 
						"error", err)
					
					// Wait before attempting to restart the failed stream
					select {
					case <-time.After(30 * time.Second):
						continue // Retry the entire stream
					case <-m.ctx.Done():
						return
					}
				}
			}
		}(stream)
	}
	
	m.logger.Info("Stream manager started", "camera_count", len(cameras))
	return nil
}

// runStreamWithRetry runs a stream with automatic retry on failure
func (m *Manager) runStreamWithRetry(stream *Stream) error {
	retryCount := 0
	
	for {
		select {
		case <-m.ctx.Done():
			return m.ctx.Err()
		default:
		}
		
		// Update status
		m.sendStatusUpdate(stream.camera.ID, StatusConnecting, "")
		
		// Start stream
		err := stream.Start(m.ctx)
		
		if err != nil {
			retryCount++
			
			if retryCount > m.maxRetries && m.maxRetries > 0 {
				m.logger.Error("Max retries exceeded for stream", 
					"camera_id", stream.camera.ID,
					"retries", retryCount,
					"error", err)
				m.sendStatusUpdate(stream.camera.ID, StatusError, err.Error())
				// Return error to trigger restart cycle in Start() method
				return fmt.Errorf("stream failed after %d retries: %w", retryCount, err)
			}
			
			m.logger.Warn("Stream failed, retrying",
				"camera_id", stream.camera.ID,
				"retry", retryCount,
				"error", err)
			
			m.sendStatusUpdate(stream.camera.ID, StatusReconnecting, err.Error())
			
			// Wait before retry with exponential backoff
			delay := m.retryDelay * time.Duration(retryCount)
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			
			select {
			case <-time.After(delay):
				continue
			case <-m.ctx.Done():
				return m.ctx.Err()
			}
		}
		
		// Stream ended normally (shouldn't happen for continuous streams)
		m.logger.Info("Stream ended", "camera_id", stream.camera.ID)
		m.sendStatusUpdate(stream.camera.ID, StatusDisconnected, "")
		
		// Reset retry count on successful connection
		if retryCount > 0 {
			retryCount = 0
		}
		
		// Wait before reconnecting
		select {
		case <-time.After(m.retryDelay):
			continue
		case <-m.ctx.Done():
			return m.ctx.Err()
		}
	}
}

// Stop stops all streams
func (m *Manager) Stop() {
	m.logger.Info("Stopping stream manager")
	
	// Cancel context to stop all streams
	m.cancel()
	
	// Wait for all goroutines to finish
	if err := m.eg.Wait(); err != nil && err != context.Canceled {
		m.logger.Error("Error stopping streams", "error", err)
	}
	
	// Close status channel
	close(m.statusChan)
	
	m.logger.Info("Stream manager stopped")
}

// RestartStream restarts a specific stream
func (m *Manager) RestartStream(cameraID string) error {
	m.mu.RLock()
	stream, exists := m.streams[cameraID]
	m.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("stream not found: %s", cameraID)
	}
	
	m.logger.Info("Restarting stream", "camera_id", cameraID)
	
	// Stop the stream
	stream.Stop()
	
	// Wait a moment
	time.Sleep(2 * time.Second)
	
	// Start it again
	go m.runStreamWithRetry(stream)
	
	return nil
}

// GetStatus returns the status of all streams
func (m *Manager) GetStatus() map[string]StreamStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	status := make(map[string]StreamStatus)
	for id, stream := range m.streams {
		status[id] = stream.GetStatus()
	}
	
	return status
}

// GetStreamStatus returns the status of a specific stream
func (m *Manager) GetStreamStatus(cameraID string) (StreamStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	stream, exists := m.streams[cameraID]
	if !exists {
		return StatusDisconnected, fmt.Errorf("stream not found: %s", cameraID)
	}
	
	return stream.GetStatus(), nil
}

// GetStatusChannel returns the status update channel
func (m *Manager) GetStatusChannel() <-chan StatusUpdate {
	return m.statusChan
}

// sendStatusUpdate sends a status update
func (m *Manager) sendStatusUpdate(cameraID string, status StreamStatus, errorMsg string) {
	update := StatusUpdate{
		CameraID:  cameraID,
		Status:    status,
		Error:     errorMsg,
		Timestamp: time.Now(),
	}
	
	select {
	case m.statusChan <- update:
	default:
		// Channel full, drop update
		m.logger.Warn("Status channel full, dropping update", "camera_id", cameraID)
	}
}

// AddCamera adds a new camera stream
func (m *Manager) AddCamera(camera *config.CameraConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if _, exists := m.streams[camera.ID]; exists {
		return fmt.Errorf("camera already exists: %s", camera.ID)
	}
	
	stream := NewStream(camera, m.config, m.logger.With("camera_id", camera.ID))
	m.streams[camera.ID] = stream
	
	// Start stream in background
	m.eg.Go(func() error {
		return m.runStreamWithRetry(stream)
	})
	
	return nil
}

// RemoveCamera removes a camera stream
func (m *Manager) RemoveCamera(cameraID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	stream, exists := m.streams[cameraID]
	if !exists {
		return fmt.Errorf("camera not found: %s", cameraID)
	}
	
	// Stop the stream
	stream.Stop()
	
	// Remove from map
	delete(m.streams, cameraID)
	
	return nil
}

// UpdateCameraConfig updates camera configuration
func (m *Manager) UpdateCameraConfig(camera *config.CameraConfig) error {
	// Remove old stream
	if err := m.RemoveCamera(camera.ID); err != nil {
		m.logger.Warn("Failed to remove old stream", "error", err)
	}
	
	// Add new stream with updated config
	return m.AddCamera(camera)
}
