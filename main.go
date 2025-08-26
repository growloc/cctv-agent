package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/cctv-agent/config"
	"github.com/cctv-agent/internal/logger"
	"github.com/cctv-agent/internal/monitor"
	"github.com/cctv-agent/internal/onvif"
	"github.com/cctv-agent/internal/socketio"
	"github.com/cctv-agent/internal/stream"
	"github.com/cctv-agent/internal/updater"
	"github.com/spf13/pflag"
)

const (
	version           = "1.0.0"
	defaultConfigPath = "/etc/cctv-agent/config.json"
)

// Application represents the main application
type Application struct {
	config        *config.Config
	logger        logger.Logger
	streamManager *stream.Manager
	onvifCtrl     *onvif.Controller
	sioClient     *socketio.Client
	updater       *updater.Updater
	systemMonitor *monitor.SystemMonitor
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	startTime     time.Time
}

func main() {
	// Parse command line flags using pflag
	configPath := pflag.String("config", defaultConfigPath, "Path to configuration file")
	generateConfig := pflag.Bool("generate-config", false, "Generate sample configuration file")
	showVersion := pflag.Bool("version", false, "Show version information")
	pflag.Parse()

	// Show version if requested
	if *showVersion {
		fmt.Printf("CCTV Agent version %s\n", version)
		os.Exit(0)
	}

	// Generate sample config if requested
	if *generateConfig {
		if *configPath != "" {
			if err := generateSampleConfigToFile(*configPath); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate config: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Sample configuration generated at %s\n", *configPath)
		} else {
			generateSampleConfig()
		}
		os.Exit(0)
	}

	// Create application
	app := NewApplication(*configPath)

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start application
	if err := app.Start(); err != nil {
		app.logger.Error("Failed to start application", "error", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	<-sigChan
	app.logger.Info("Shutdown signal received")

	// Shutdown application
	app.Shutdown()
}

// NewApplication creates a new application instance
func NewApplication(configPath string) *Application {
	ctx, cancel := context.WithCancel(context.Background())

	app := &Application{
		ctx:       ctx,
		cancel:    cancel,
		startTime: time.Now(),
	}

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		// Use default config if loading fails
		cfg = &config.Config{
			Agent: config.AgentConfig{
				ID:             "cctv-agent-001",
				Name:           "CCTV Agent",
				Location:       "Main Building",
				UpdateInterval: 30 * time.Minute,
				LogLevel:       "info",
				MaxConcurrency: 4,
			},
			Logger: config.LoggerConfig{
				Level:         "debug",
				ConsoleOutput: true,
				ConsoleFormat: "text",
				FileOutput:    true,
				FileFormat:    "json",
				LogDir:        "/opt/grw/cctv-agent/logs",
				MaxSize:       100,
				MaxBackups:    3,
				MaxAge:        7,
				Compress:      true,
			},
			SocketIO: config.SocketIOConfig{
				Host:           "localhost",
				Port:           9054,
				Path:           "/socket.io",
				ReconnectDelay: 5 * time.Second,
				PingInterval:   30 * time.Second,
				TLS:            false,
			},
			Cameras: []config.CameraConfig{},
			FFmpeg: config.FFmpegConfig{
				Preset:       "ultrafast",
				Tune:         "zerolatency",
				CRF:          23,
				MaxRate:      "2M",
				BufSize:      "4M",
				AudioBitrate: "128k",
				AudioRate:    44100,
				VideoCodec:   "libx264",
				AudioCodec:   "aac",
				LogLevel:     "error",
				ExtraArgs:    "-rtsp_transport tcp",
			},
			RTMP: config.RTMPConfig{
				Host:    "localhost",
				Port:    1935,
				AppName: "live",
			},
		}
		fmt.Fprintf(os.Stderr, "Failed to load config, using defaults: %v\n", err)
	}
	app.config = cfg

	// Initialize logger with configuration
	loggerCfg := cfg.Logger
	if loggerCfg.Level == "" {
		loggerCfg.Level = cfg.Agent.LogLevel
		if loggerCfg.Level == "" {
			loggerCfg.Level = "info"
		}
	}
	if loggerCfg.LogDir == "" {
		loggerCfg.LogDir = "logs"
	}
	app.logger = logger.NewLoggerWithConfig(&loggerCfg)
	app.logger.Info("CCTV Agent starting", "version", version)

	// Initialize Socket.IO client
	sioURL := fmt.Sprintf("ws://%s:%d", cfg.SocketIO.Host, cfg.SocketIO.Port)
	if cfg.SocketIO.TLS {
		sioURL = fmt.Sprintf("wss://%s:%d", cfg.SocketIO.Host, cfg.SocketIO.Port)
	}
	if cfg.SocketIO.Path != "" && cfg.SocketIO.Path != "/socket.io" {
		sioURL = fmt.Sprintf("%s%s", sioURL, cfg.SocketIO.Path)
	}
	app.logger.Info("Socket.IO URL configured", "url", sioURL, "path", cfg.SocketIO.Path)
	app.sioClient = socketio.NewClient(sioURL, app.logger)
	app.streamManager = stream.NewManager(app.config, app.logger)
	app.onvifCtrl = onvif.NewController(app.logger)
	app.updater = updater.NewUpdater(app.logger, version)
	// Set the SocketIO client for update checks
	app.updater.SetSocketIOClient(app.sioClient)
	// Apply updater config directly
	// Preserve some fallbacks when fields are zero-valued
	uc := cfg.Updater
	if uc.Interval == 0 {
		if cfg.Agent.UpdateInterval > 0 {
			uc.Interval = cfg.Agent.UpdateInterval
		}
	}
	if uc.KeepReleases <= 0 {
		uc.KeepReleases = 3
	}
	if uc.HealthTimeout == 0 {
		uc.HealthTimeout = 30 * time.Second
	}
	if uc.BaseDir == "" {
		uc.BaseDir = "/opt/cctv-agent"
	}
	if uc.ServiceName == "" {
		uc.ServiceName = "cctv-agent"
	}
	if uc.Channel == "" {
		uc.Channel = "stable"
	}
	app.updater.ApplyConfig(uc)
	app.systemMonitor = monitor.NewSystemMonitor(app.logger)

	return app
}

// Start starts the application
func (app *Application) Start() error {
	app.logger.Info("Starting application components")

	// Updater startup finalize/health
	if app.updater != nil {
		app.updater.HandleStartup()
	}

	// Initialize ONVIF controller if cameras have PTZ
	for _, camera := range app.config.Cameras {
		if camera.PTZEnabled {
			if err := app.onvifCtrl.Connect(
				camera.ID,
				camera.RTSPUrl,
				camera.Username,
				camera.Password,
			); err != nil {
				app.logger.Error("Failed to connect ONVIF device",
					"camera_id", camera.ID,
					"error", err)
			}
		}
	}

	// Start stream manager
	if err := app.streamManager.Start(); err != nil {
		return fmt.Errorf("failed to start stream manager: %w", err)
	}

	app.sioClient.RegisterEventHandler("pong", func(data json.RawMessage) error {
		app.logger.Info("Socket.IO pong", "pong", data)

		return nil
	})

	app.sioClient.RegisterEventHandler("welcome", func(data json.RawMessage) error {
		app.logger.Info("Socket.IO welcome", "data", data)

		return nil
	})

	app.sioClient.RegisterEventHandler("command", func(data json.RawMessage) error {
		return app.handleCommand(data)
	})

	// Connect to Socket.IO server
	if err := app.sioClient.Connect(); err != nil {
		app.logger.Error("Failed to connect to Socket.IO server", "error", err)
		// Continue running even if Socket.IO fails initially
	}

	// Set up Socket.IO handlers
	app.sioClient.OnConnect(func() {
		app.logger.Info("Socket.IO connected")
		app.sendRegistration()
	})

	app.sioClient.OnDisconnect(func() {
		app.logger.Warn("Socket.IO disconnected")
	})

	// Start background tasks
	bgCount := 2
	if app.updater != nil && app.config.Updater.Enabled {
		bgCount++
	}
	app.wg.Add(bgCount)
	go app.processCommands()
	go app.reportStatus()
	if app.updater != nil && app.config.Updater.Enabled {
		go func() {
			defer app.wg.Done()
			app.updater.RunPeriodic(app.ctx)
		}()
	}

	app.logger.Info("Application started successfully")
	return nil
}

// Shutdown gracefully shuts down the application
func (app *Application) Shutdown() {
	app.logger.Info("Shutting down application")

	// Cancel context to stop all components
	app.cancel()

	// Stop stream manager
	if app.streamManager != nil {
		app.streamManager.Stop()
	}

	// Disconnect Socket.IO
	if app.sioClient != nil {
		app.sioClient.Disconnect()
	}

	// Wait for goroutines to finish
	app.wg.Wait()

	app.logger.Info("Application shutdown complete")
}

// processCommands processes commands from Socket.IO
func (app *Application) processCommands() {
	defer app.wg.Done()

	for {
		select {
		case <-app.ctx.Done():
			return
		}
	}
}

// handleCommand handles a single command
func (app *Application) handleCommand(cmd json.RawMessage) error {
	app.logger.Info("Processing command")

	// TODO: Implement command handling
	return nil
}

// restartComponents restarts components with new configuration
func (app *Application) restartComponents() {
	app.logger.Info("Restarting components with new configuration")

	// Restart stream manager
	app.streamManager.Stop()
	app.streamManager = stream.NewManager(app.config, app.logger)
	app.streamManager.Start()

	// Set up Socket.IO handlers
	app.sioClient.OnConnect(func() {
		app.logger.Info("Socket.IO connected")
		app.sendRegistration()
	})

	app.sioClient.OnDisconnect(func() {
		app.logger.Warn("Socket.IO disconnected")
	})

	app.sioClient.RegisterEventHandler("command", func(data json.RawMessage) error {
		return app.handleCommand(data)
	})

	// Re-initialize ONVIF devices
	app.onvifCtrl = onvif.NewController(app.logger)
	for _, camera := range app.config.Cameras {
		if camera.PTZEnabled {
			app.onvifCtrl.Connect(
				camera.ID,
				camera.RTSPUrl,
				camera.Username,
				camera.Password,
			)
		}
	}
}

// reportStatus periodically reports status to server
func (app *Application) reportStatus() {
	defer app.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			app.sendStatusReport()
		}
	}
}

// sendRegistration sends registration message
func (app *Application) sendRegistration() {
	hostname, _ := os.Hostname()
	reg := socketio.Registration{
		AgentID:  app.config.Agent.ID,
		Name:     hostname,
		Location: "Raspberry Pi",
		Version:  version,
	}

	if err := app.sioClient.Emit("registration", reg); err != nil {
		app.logger.Error("Failed to send registration", "error", err)
	}
}

// sendStatusReport sends status report
func (app *Application) sendStatusReport() {
	// Get camera statuses
	cameraStatuses := make(map[string]socketio.CameraStatus)
	// TODO: Get actual camera statuses from stream manager
	for _, camera := range app.config.Cameras {
		cameraStatuses[camera.ID] = socketio.CameraStatus{
			ID:         camera.ID,
			Connected:  false,
			Streaming:  false,
			LastUpdate: time.Now(),
			Error:      "",
		}
	}

	// Get system info
	systemInfo := app.getSystemInfo()

	// Create status report
	report := socketio.StatusReport{
		AgentID:      app.config.Agent.ID,
		Version:      version,
		Uptime:       time.Since(app.startTime),
		CameraStatus: cameraStatuses,
		SystemInfo:   systemInfo,
		Timestamp:    time.Now(),
	}

	if err := app.sioClient.Emit("status", report); err != nil {
		app.logger.Error("Failed to send status report", "error", err)
	}
}

// getSystemInfo gets system information
func (app *Application) getSystemInfo() socketio.SystemInfo {
	stats, err := app.systemMonitor.GetSystemStats()
	if err != nil {
		app.logger.Error("Failed to get system stats", "error", err)
		return socketio.SystemInfo{}
	}

	return socketio.SystemInfo{
		CPU: socketio.CPUInfo{
			Usage: stats.CPUUsage,
			Cores: 4, // Default to 4 cores for Raspberry Pi
		},
		Memory: socketio.MemoryInfo{
			Total:   0, // Would need additional system calls to get total memory
			Used:    0, // Would need additional system calls to get used memory
			Percent: stats.MemoryUsage,
		},
		Disk: socketio.DiskInfo{
			Total:   0, // Would need additional system calls to get total disk
			Used:    0, // Would need additional system calls to get used disk
			Percent: stats.DiskUsage,
		},
		Temperature: stats.Temperature,
		Network: socketio.NetworkInfo{
			BytesSent:       stats.Network.BytesSent,
			BytesReceived:   stats.Network.BytesReceived,
			PacketsSent:     stats.Network.PacketsSent,
			PacketsReceived: stats.Network.PacketsReceived,
		},
	}
}

// Helper functions

func generateSampleConfig() {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			ID:             "cctv-agent-001",
			Name:           "CCTV Agent",
			Location:       "Main Building",
			UpdateInterval: 30 * time.Second,
			LogLevel:       "info",
			MaxConcurrency: 4,
		},
		Logger: config.LoggerConfig{
			Level:         "info",
			ConsoleOutput: true,
			ConsoleFormat: "text",
			FileOutput:    true,
			FileFormat:    "json",
			LogDir:        "logs",
			MaxSize:       100,
			MaxBackups:    3,
			MaxAge:        7,
			Compress:      true,
		},
		SocketIO: config.SocketIOConfig{
			Host:           "localhost",
			Port:           9054,
			Path:           "/custom-socket",
			ReconnectDelay: 5 * time.Second,
			PingInterval:   30 * time.Second,
			TLS:            false,
		},
		Cameras: []config.CameraConfig{
			{
				ID:         "camera1",
				Name:       "Front Door Camera",
				RTSPUrl:    "rtsp://admin:password@192.168.1.100:554/stream1",
				PTZEnabled: true,
				Username:   "admin",
				Password:   "password",
				ONVIFPort:  80,
			},
		},
		FFmpeg: config.FFmpegConfig{
			Preset:       "ultrafast",
			Tune:         "zerolatency",
			CRF:          23,
			MaxRate:      "2M",
			BufSize:      "4M",
			AudioBitrate: "128k",
			AudioRate:    44100,
			VideoCodec:   "libx264",
			AudioCodec:   "aac",
			LogLevel:     "error",
			ExtraArgs:    "-rtsp_transport tcp",
		},
		RTMP: config.RTMPConfig{
			Host:    "localhost",
			Port:    1935,
			AppName: "live",
		},
		Updater: config.UpdaterConfig{
			URL: "https://updates.example.com/cctv-agent",
		},
	}

	cfgJSON, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Println(string(cfgJSON))
}

func generateSampleConfigToFile(path string) error {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			ID:             "cctv-agent-001",
			Name:           "CCTV Agent",
			Location:       "Main Building",
			UpdateInterval: 30 * time.Second,
			LogLevel:       "info",
			MaxConcurrency: 4,
		},
		Logger: config.LoggerConfig{
			Level:         "info",
			ConsoleOutput: true,
			ConsoleFormat: "text",
			FileOutput:    true,
			FileFormat:    "json",
			LogDir:        "logs",
			MaxSize:       100,
			MaxBackups:    3,
			MaxAge:        7,
			Compress:      true,
		},
		SocketIO: config.SocketIOConfig{
			Host:           "localhost",
			Port:           8080,
			Path:           "/socket.io",
			ReconnectDelay: 5 * time.Second,
			PingInterval:   30 * time.Second,
			TLS:            false,
		},
		Cameras: []config.CameraConfig{
			{
				ID:         "camera1",
				Name:       "Front Door Camera",
				RTSPUrl:    "rtsp://admin:password@192.168.1.100:554/stream1",
				PTZEnabled: true,
				Username:   "admin",
				Password:   "password",
				ONVIFPort:  80,
			},
		},
		FFmpeg: config.FFmpegConfig{
			Preset:       "ultrafast",
			Tune:         "zerolatency",
			CRF:          23,
			MaxRate:      "2M",
			BufSize:      "4M",
			AudioBitrate: "128k",
			AudioRate:    44100,
			VideoCodec:   "libx264",
			AudioCodec:   "aac",
			LogLevel:     "error",
			ExtraArgs:    "-rtsp_transport tcp",
		},
		RTMP: config.RTMPConfig{
			Host:    "localhost",
			Port:    1935,
			AppName: "live",
		},
		Updater: config.UpdaterConfig{
			URL: "https://updates.example.com/cctv-agent",
		},
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write config file
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func getPlatform() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}
