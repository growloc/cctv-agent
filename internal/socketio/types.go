package socketio

import (
	"encoding/json"
	"time"
)

// Message represents a Socket.IO message
type Message struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// Registration represents agent registration data
type Registration struct {
	AgentID  string `json:"agent_id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Version  string `json:"version"`
}

// StatusReport represents agent status report
type StatusReport struct {
	AgentID      string                    `json:"agent_id"`
	Version      string                    `json:"version"`
	Uptime       time.Duration             `json:"uptime"`
	CameraStatus map[string]CameraStatus   `json:"camera_status"`
	SystemInfo   SystemInfo                `json:"system_info"`
	Timestamp    time.Time                 `json:"timestamp"`
}

// CameraStatus represents individual camera status
type CameraStatus struct {
	ID         string    `json:"id"`
	Connected  bool      `json:"connected"`
	Streaming  bool      `json:"streaming"`
	LastUpdate time.Time `json:"last_update"`
	Error      string    `json:"error,omitempty"`
}

// SystemInfo represents system information
type SystemInfo struct {
	CPU         CPUInfo     `json:"cpu"`
	Memory      MemoryInfo  `json:"memory"`
	Disk        DiskInfo    `json:"disk"`
	Network     NetworkInfo `json:"network"`
	Temperature float64     `json:"temperature"`
}

// CPUInfo represents CPU information
type CPUInfo struct {
	Usage float64 `json:"usage"`
	Cores int     `json:"cores"`
}

// MemoryInfo represents memory information
type MemoryInfo struct {
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
}

// DiskInfo represents disk information
type DiskInfo struct {
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
}

// NetworkInfo represents network information
type NetworkInfo struct {
	BytesSent       uint64 `json:"bytes_sent"`
	BytesReceived   uint64 `json:"bytes_received"`
	PacketsSent     uint64 `json:"packets_sent"`
	PacketsReceived uint64 `json:"packets_received"`
}

// Command represents a command from the server
type Command struct {
	Type     string          `json:"type"`
	CameraID string          `json:"camera_id,omitempty"`
	Data     json.RawMessage `json:"data"`
}

// PTZCommand represents PTZ control command
type PTZCommand struct {
	Action string  `json:"action"`
	Pan    float64 `json:"pan,omitempty"`
	Tilt   float64 `json:"tilt,omitempty"`
	Zoom   float64 `json:"zoom,omitempty"`
	Preset int     `json:"preset,omitempty"`
}

// StreamCommand represents stream control command
type StreamCommand struct {
	Action string `json:"action"` // start, stop, restart
}

// UpdateCommand represents update command
type UpdateCommand struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}
