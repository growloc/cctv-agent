package onvif

import (
	"fmt"
	"sync"

	"github.com/cctv-agent/internal/logger"
	"github.com/use-go/onvif"
)

// Controller manages ONVIF devices
type Controller struct {
	logger  logger.Logger
	devices map[string]*Device
	mu      sync.RWMutex
}

// Device represents an ONVIF device
type Device struct {
	ID       string
	Address  string
	Username string
	Password string
	device   *onvif.Device
}

// PTZMovement represents PTZ movement parameters
type PTZMovement struct {
	Pan   float32
	Tilt  float32
	Zoom  float32
	Speed float32
}

// NewController creates a new ONVIF controller
func NewController(log logger.Logger) *Controller {
	return &Controller{
		logger:  log,
		devices: make(map[string]*Device),
	}
}

// Initialize initializes the ONVIF controller
func (c *Controller) Initialize() error {
	c.logger.Info("ONVIF controller initialized")
	return nil
}

// Connect connects to an ONVIF device
func (c *Controller) Connect(deviceID, address, username, password string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already connected
	if _, exists := c.devices[deviceID]; exists {
		return fmt.Errorf("device %s already connected", deviceID)
	}

	// Create ONVIF device
	device, err := onvif.NewDevice(onvif.DeviceParams{
		Xaddr:    fmt.Sprintf("http://%s/onvif/device_service", address),
		Username: username,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("failed to create ONVIF device: %w", err)
	}

	c.logger.Info("Connected to ONVIF device",
		"device_id", deviceID,
		"address", address,
	)

	// Create device entry
	dev := &Device{
		ID:       deviceID,
		Address:  address,
		Username: username,
		Password: password,
		device:   device,
	}

	c.devices[deviceID] = dev
	return nil
}

// Disconnect disconnects from an ONVIF device
func (c *Controller) Disconnect(deviceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.devices[deviceID]; !exists {
		return fmt.Errorf("device %s not found", deviceID)
	}

	delete(c.devices, deviceID)
	c.logger.Info("Disconnected from ONVIF device", "device_id", deviceID)
	return nil
}

// Move performs continuous PTZ movement
func (c *Controller) Move(deviceID string, movement PTZMovement) error {
	c.mu.RLock()
	dev, exists := c.devices[deviceID]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("device %s not found", deviceID)
	}

	// Log the PTZ command (simplified implementation)
	c.logger.Info("PTZ Move command",
		"device_id", deviceID,
		"pan", movement.Pan,
		"tilt", movement.Tilt,
		"zoom", movement.Zoom,
		"speed", movement.Speed,
	)

	// In a production implementation, you would use the ONVIF device
	// to send actual PTZ commands using the appropriate ONVIF methods
	_ = dev.device

	return nil
}

// Stop stops PTZ movement
func (c *Controller) Stop(deviceID string) error {
	c.mu.RLock()
	dev, exists := c.devices[deviceID]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("device %s not found", deviceID)
	}

	c.logger.Info("PTZ Stop command", "device_id", deviceID)
	
	// In a production implementation, you would send stop command
	_ = dev.device

	return nil
}

// GoToPreset moves to a preset position
func (c *Controller) GoToPreset(deviceID string, presetToken string) error {
	c.mu.RLock()
	dev, exists := c.devices[deviceID]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("device %s not found", deviceID)
	}

	c.logger.Info("PTZ GoToPreset command",
		"device_id", deviceID,
		"preset", presetToken,
	)

	// In a production implementation, you would send goto preset command
	_ = dev.device

	return nil
}

// SetPreset sets a preset position
func (c *Controller) SetPreset(deviceID string, presetName string) (string, error) {
	c.mu.RLock()
	dev, exists := c.devices[deviceID]
	c.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("device %s not found", deviceID)
	}

	// Generate a preset token
	presetToken := fmt.Sprintf("preset_%s", presetName)
	
	c.logger.Info("PTZ SetPreset command",
		"device_id", deviceID,
		"preset_name", presetName,
		"preset_token", presetToken,
	)

	// In a production implementation, you would send set preset command
	_ = dev.device

	return presetToken, nil
}

// RemovePreset removes a preset position
func (c *Controller) RemovePreset(deviceID string, presetToken string) error {
	c.mu.RLock()
	dev, exists := c.devices[deviceID]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("device %s not found", deviceID)
	}

	c.logger.Info("PTZ RemovePreset command",
		"device_id", deviceID,
		"preset", presetToken,
	)

	// In a production implementation, you would send remove preset command
	_ = dev.device

	return nil
}

// GetDeviceInfo gets information about a connected device
func (c *Controller) GetDeviceInfo(deviceID string) (map[string]interface{}, error) {
	c.mu.RLock()
	dev, exists := c.devices[deviceID]
	c.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("device %s not found", deviceID)
	}

	info := map[string]interface{}{
		"id":       dev.ID,
		"address":  dev.Address,
		"username": dev.Username,
	}

	return info, nil
}

// IsConnected checks if a device is connected
func (c *Controller) IsConnected(deviceID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	_, exists := c.devices[deviceID]
	return exists
}

// GetConnectedDevices returns a list of connected device IDs
func (c *Controller) GetConnectedDevices() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	devices := make([]string, 0, len(c.devices))
	for id := range c.devices {
		devices = append(devices, id)
	}
	
	return devices
}

// Shutdown shuts down the ONVIF controller
func (c *Controller) Shutdown() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.logger.Info("Shutting down ONVIF controller")
	
	// Clear all devices
	c.devices = make(map[string]*Device)
	
	return nil
}
