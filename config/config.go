package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
)

type UpdaterConfig struct {
	Enabled        bool          `json:"enabled" mapstructure:"enabled"`
	URL            string        `json:"url" mapstructure:"url"` // optional direct artifact URL
	Interval       time.Duration `json:"interval" mapstructure:"interval"`
	KeepReleases   int           `json:"keep_releases" mapstructure:"keep_releases"`
	HealthTimeout  time.Duration `json:"health_timeout" mapstructure:"health_timeout"`
	BaseDir        string        `json:"base_dir" mapstructure:"base_dir"`
	ServiceName    string        `json:"service_name" mapstructure:"service_name"`
	Channel        string        `json:"channel" mapstructure:"channel"`
	AllowDowngrade bool          `json:"allow_downgrade" mapstructure:"allow_downgrade"`
}

// Config represents the main configuration structure
type Config struct {
	Agent      AgentConfig      `json:"agent" mapstructure:"agent"`
	Logger     LoggerConfig     `json:"logger" mapstructure:"logger"`
	SocketIO   SocketIOConfig   `json:"socketio" mapstructure:"socketio"`
	Cameras    []CameraConfig   `json:"cameras" mapstructure:"cameras"`
	FFmpeg     FFmpegConfig     `json:"ffmpeg" mapstructure:"ffmpeg"`
	RTMP       RTMPConfig       `json:"rtmp" mapstructure:"rtmp"`
	Updater    UpdaterConfig    `json:"updater" mapstructure:"updater"`
	Monitoring MonitoringConfig `json:"monitoring" mapstructure:"monitoring"`
}

// AgentConfig represents agent-specific configuration
type AgentConfig struct {
	ID             string        `json:"id" mapstructure:"id"`
	Name           string        `json:"name" mapstructure:"name"`
	Location       string        `json:"location" mapstructure:"location"`
	UpdateInterval time.Duration `json:"update_interval" mapstructure:"update_interval"`
	LogLevel       string        `json:"log_level" mapstructure:"log_level"`
	MaxConcurrency int           `json:"max_concurrency" mapstructure:"max_concurrency"`
}

// LoggerConfig represents logger configuration
type LoggerConfig struct {
	Level         string `json:"level" mapstructure:"level"`                   // Log level: debug, info, warn, error
	ConsoleOutput bool   `json:"console_output" mapstructure:"console_output"` // Enable console output
	ConsoleFormat string `json:"console_format" mapstructure:"console_format"` // Console format: json or text
	FileOutput    bool   `json:"file_output" mapstructure:"file_output"`       // Enable file output
	FileFormat    string `json:"file_format" mapstructure:"file_format"`       // File format: json or text
	LogDir        string `json:"log_dir" mapstructure:"log_dir"`               // Directory for log files
	MaxSize       int    `json:"max_size" mapstructure:"max_size"`             // Max size in MB before rotation
	MaxBackups    int    `json:"max_backups" mapstructure:"max_backups"`       // Max number of old log files
	MaxAge        int    `json:"max_age" mapstructure:"max_age"`               // Max age in days for log files
	Compress      bool   `json:"compress" mapstructure:"compress"`             // Compress rotated files
}

// CameraConfig represents camera configuration
type CameraConfig struct {
	ID         string        `json:"id" mapstructure:"id"`
	Name       string        `json:"name" mapstructure:"name"`
	RTSPUrl    string        `json:"rtsp_url" mapstructure:"rtsp_url"`
	Username   string        `json:"username" mapstructure:"username"`
	Password   string        `json:"password" mapstructure:"password"`
	ONVIFPort  int           `json:"onvif_port" mapstructure:"onvif_port"`
	StreamID   string        `json:"stream_id" mapstructure:"stream_id"`
	Enabled    bool          `json:"enabled" mapstructure:"enabled"`
	PTZEnabled bool          `json:"ptz_enabled" mapstructure:"ptz_enabled"`
	RetryCount int           `json:"retry_count" mapstructure:"retry_count"`
	RetryDelay time.Duration `json:"retry_delay" mapstructure:"retry_delay"`
}

// SocketIOConfig represents Socket.IO configuration
type SocketIOConfig struct {
	Host           string        `json:"host" mapstructure:"host"`
	Port           int           `json:"port" mapstructure:"port"`
	Path           string        `json:"path" mapstructure:"path"`
	ReconnectDelay time.Duration `json:"reconnect_delay" mapstructure:"reconnect_delay"`
	PingInterval   time.Duration `json:"ping_interval" mapstructure:"ping_interval"`
	TLS            bool          `json:"tls" mapstructure:"tls"`
}

// RTMPConfig represents RTMP server configuration
type RTMPConfig struct {
	Host    string `json:"host" mapstructure:"host"`
	Port    int    `json:"port" mapstructure:"port"`
	AppName string `json:"app_name" mapstructure:"app_name"`
}

// FFmpegConfig represents FFmpeg configuration
type FFmpegConfig struct {
	Preset       string `json:"preset" mapstructure:"preset"`
	Tune         string `json:"tune" mapstructure:"tune"`
	CRF          int    `json:"crf" mapstructure:"crf"`
	MaxRate      string `json:"max_rate" mapstructure:"max_rate"`
	BufSize      string `json:"buf_size" mapstructure:"buf_size"`
	AudioBitrate string `json:"audio_bitrate" mapstructure:"audio_bitrate"`
	AudioRate    int    `json:"audio_rate" mapstructure:"audio_rate"`
	VideoCodec   string `json:"video_codec" mapstructure:"video_codec"`
	AudioCodec   string `json:"audio_codec" mapstructure:"audio_codec"`
	LogLevel     string `json:"log_level" mapstructure:"log_level"`
	ExtraArgs    string `json:"extra_args" mapstructure:"extra_args"`
}

// MonitoringConfig represents monitoring configuration
type MonitoringConfig struct {
	HealthCheckInterval time.Duration `json:"health_check_interval" mapstructure:"health_check_interval"`
	MetricsEnabled      bool          `json:"metrics_enabled" mapstructure:"metrics_enabled"`
	MetricsPort         int           `json:"metrics_port" mapstructure:"metrics_port"`
}

// LoadConfig loads configuration from file
func LoadConfig(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("json")

	// Set defaults
	setDefaults()

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// LoadConfigFromJSON loads configuration from JSON data
func LoadConfigFromJSON(data []byte) (*Config, error) {
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// SaveConfig saves configuration to file
func SaveConfig(config *Config, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	return nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Agent.ID == "" {
		return fmt.Errorf("agent ID is required")
	}

	if c.Agent.MaxConcurrency <= 0 {
		c.Agent.MaxConcurrency = 4
	}

	if len(c.Cameras) == 0 {
		return fmt.Errorf("at least one camera must be configured")
	}

	for i, camera := range c.Cameras {
		if camera.ID == "" {
			return fmt.Errorf("camera[%d]: ID is required", i)
		}
		if camera.RTSPUrl == "" {
			return fmt.Errorf("camera[%d]: RTSP URL is required", i)
		}
		if camera.RetryCount <= 0 {
			c.Cameras[i].RetryCount = 3
		}
		if camera.RetryDelay <= 0 {
			c.Cameras[i].RetryDelay = 5 * time.Second
		}
	}

	if c.SocketIO.Host == "" {
		return fmt.Errorf("Socket.IO host is required")
	}

	if c.SocketIO.Port <= 0 {
		c.SocketIO.Port = 8080
	}

	return nil
}

// GetCameraByID returns camera configuration by ID
func (c *Config) GetCameraByID(id string) (*CameraConfig, error) {
	for i := range c.Cameras {
		if c.Cameras[i].ID == id {
			return &c.Cameras[i], nil
		}
	}
	return nil, fmt.Errorf("camera not found: %s", id)
}

// GetEnabledCameras returns all enabled cameras
func (c *Config) GetEnabledCameras() []CameraConfig {
	var enabled []CameraConfig
	for _, camera := range c.Cameras {
		if camera.Enabled {
			enabled = append(enabled, camera)
		}
	}
	return enabled
}

// setDefaults sets default configuration values
func setDefaults() {
	viper.SetDefault("agent.max_concurrency", 4)
	viper.SetDefault("agent.update_interval", "30s")
	viper.SetDefault("agent.log_level", "info")

	viper.SetDefault("socketio.host", "localhost")
	viper.SetDefault("socketio.port", 8080)
	viper.SetDefault("socketio.path", "/socket.io")
	viper.SetDefault("socketio.reconnect_delay", "5s")
	viper.SetDefault("socketio.ping_interval", "30s")
	viper.SetDefault("socketio.tls", false)

	viper.SetDefault("ffmpeg.preset", "veryfast")
	viper.SetDefault("ffmpeg.tune", "zerolatency")
	viper.SetDefault("ffmpeg.crf", 28)
	viper.SetDefault("ffmpeg.max_rate", "800k")
	viper.SetDefault("ffmpeg.buf_size", "1600k")
	viper.SetDefault("ffmpeg.audio_bitrate", "96k")
	viper.SetDefault("ffmpeg.audio_rate", 44100)
	viper.SetDefault("ffmpeg.video_codec", "libx264")
	viper.SetDefault("ffmpeg.audio_codec", "aac")
	viper.SetDefault("ffmpeg.log_level", "warning")

	viper.SetDefault("monitoring.health_check_interval", "10s")
	viper.SetDefault("monitoring.metrics_enabled", true)
	viper.SetDefault("monitoring.metrics_port", 9090)

	// Updater defaults
	viper.SetDefault("updater.enabled", true)
	viper.SetDefault("updater.interval", "2h")
	viper.SetDefault("updater.keep_releases", 3)
	viper.SetDefault("updater.health_timeout", "30s")
	viper.SetDefault("updater.base_dir", "/opt/cctv-agent")
	viper.SetDefault("updater.service_name", "cctv-agent")
	viper.SetDefault("updater.channel", "stable")
	viper.SetDefault("updater.allow_downgrade", false)
}
