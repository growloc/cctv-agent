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
				Name:           "CCTV Agent Reliance",
				Location:       "BETC Jamnagar",
				UpdateInterval: 30 * time.Minute,
				LogLevel:       "debug",
				MaxConcurrency: 4,
			},
			Logger: config.LoggerConfig{
				Level:         "debug",
				ConsoleOutput: true,
				ConsoleFormat: "text",
				FileOutput:    false, // Changed from true
				FileFormat:    "json",
				LogDir:        "/home/growloc/cctv-agent/logs",
				MaxSize:       100,
				MaxBackups:    3,
				MaxAge:        7,
				Compress:      true,
			},
			SocketIO: config.SocketIOConfig{
				Host:           "api-sio.growloc.farm",
				Port:           443,
				Path:           "/socket.io",
				ReconnectDelay: 5 * time.Second,
				PingInterval:   30 * time.Second,
				TLS:            true,
			},
			Cameras: []config.CameraConfig{
				{
					ID:         "67f8b433854b6df4713f418b",
					Name:       "R&D Zone 1",
					RTSPUrl:    "rtsp://admin:Secure04@192.168.0.105:554/Streaming/Unicast/channels/101",
					Enabled:    true,
					PTZEnabled: true,
					Username:   "admin",
					Password:   "Secure04",
					ONVIFPort:  80,
					StreamID:   "camera1",
					// LiveUrl:    "https://surveillance-apis.sandbox.growloc.farm/live/camera1.flv",
					LiveUrl: "https://surveillance-api.growloc.farm/live/camera1.flv",
				},
				{
					ID:         "67f8b442854b6df4713f418c",
					Name:       "R&D Zone 2",
					RTSPUrl:    "rtsp://admin:Secure04@192.168.0.105:554/Streaming/Unicast/channels/201",
					Enabled:    true,
					PTZEnabled: false,
					Username:   "admin",
					Password:   "Secure04",
					ONVIFPort:  80,
					StreamID:   "camera2",
					// LiveUrl:    "https://surveillance-apis.sandbox.growloc.farm/live/camera2.flv",
					LiveUrl: "https://surveillance-api.growloc.farm/live/camera2.flv",
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
				Host:    "surveillance-stream.growloc.farm",
				Port:    9052,
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

	// // Set binary path to user's home directory to avoid permission issues
	// homeDir, err := os.UserHomeDir()
	// if err != nil {
	// 	app.logger.Warn("Could not get user home directory, using default path", "error", err)
	// } else {
	// 	// Use ~/.cctv-agent/cctv-agent as the binary path
	// 	binaryPath := filepath.Join(homeDir, ".cctv-agent", "cctv-agent")
	// 	app.updater.SetBinaryPath(binaryPath)
	// 	app.logger.Info("Binary path set", "path", binaryPath)
	// }

	app.systemMonitor = monitor.NewSystemMonitor(app.logger)

	return app
}

// Start starts the application
func (app *Application) Start() error {
	app.logger.Info("Starting application components")

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
	app.sioClient.RegisterEventHandler("update_agent", func(data json.RawMessage) error {
		app.logger.Info("Socket.IO update_agent", "update_agent", data)

		// Parse the incoming data
		// var updateData struct {
		// 	UpdateAgent []struct {
		// 		IsUpdateAvailable bool   `json:"isUpdateAvailable"`
		// 		SourceClientId    string `json:"sourceClientId"`
		// 		Timestamp         string `json:"timestamp"`
		// 	} `json:"update_agent"`
		// }

		// if err := json.Unmarshal(data, &updateData); err != nil {
		// 	app.logger.Error("Failed to parse update_agent data", "error", err)
		// 	return err
		// }

		// Check if update is available
		// if len(updateData.UpdateAgent) > 0 && updateData.UpdateAgent[0].IsUpdateAvailable {
		// 	app.logger.Info("Update available, starting update process",
		// 		"source_client_id", updateData.UpdateAgent[0].SourceClientId,
		// 		"timestamp", updateData.UpdateAgent[0].Timestamp)

		// Create UpdateInfo for the updater
		// Note: You'll need to get these values from your update server or config
		updateInfo := updater.UpdateInfo{
			Version:      "1.0.1",                                                                   // This should come from the server or be determined dynamically
			DownloadURL:  "https://s3.ap-south-2.amazonaws.com/prod.growloc.farm/agents/cctv-agent", // Use the URL from config
			Checksum:     "",                                                                        // Optional: Add checksum if available
			ReleaseNotes: "Automatic update triggered by server",
			Force:        true, // Since server requested update
		}

		// Perform the update in a goroutine to avoid blocking the event handler
		go func() {
			if err := app.updater.PerformUpdate(updateInfo); err != nil {
				app.logger.Error("Update failed", "error", err)
				// Optionally send failure notification back to server
				return
			}
			app.logger.Info("Update completed successfully")
		}()
		// } else {
		// 	app.logger.Info("No update available or update not required")
		// }

		return nil
	})

	app.sioClient.RegisterEventHandler("welcome", func(data json.RawMessage) error {
		app.logger.Info("Socket.IO welcome", "data", data)

		return nil
	})

	app.sioClient.RegisterEventHandler("custom_response", func(data json.RawMessage) error {
		return app.handleCommand(data)
	})

	app.sioClient.RegisterEventHandler("camera_control_response", func(data json.RawMessage) error {
		app.logger.Info("Socket.IO camera_control_response", "camera_control_response", data)

		return app.handleCameraControlResponse(data)
	})

	// Connect to Socket.IO server
	// Connect to Socket.IO server
	if err := app.sioClient.Connect(); err != nil {
		app.logger.Error("Failed to connect to Socket.IO server", "error", err)
		// Continue running even if Socket.IO fails initially
	}

	// Set up Socket.IO handlers
	app.sioClient.OnConnect(func() {
		app.logger.Info("Socket.IO connected")
		app.sendRegistration()
		app.sendCameraDetails()
	})

	app.sioClient.OnDisconnect(func() {
		app.logger.Warn("Socket.IO disconnected")
	})

	// Start background tasks
	app.wg.Add(2)
	go app.processCommands()
	go app.reportStatus()

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
	app.logger.Info("Processing custom_response command")

	// TODO: Implement command handling
	return nil
}

func (app *Application) handleCameraControlResponse(data json.RawMessage) error {
	app.logger.Info("Processing camera_control_response", "data", string(data))

	// // Parse the incoming data
	// var response map[string]interface{}
	// if err := json.Unmarshal(data, &response); err != nil {
	//     app.logger.Error("Failed to parse camera_control_response", "error", err)
	//     return err
	// }

	// // Extract camera ID if present
	// cameraID, _ := response["camera_id"].(string)
	// command, _ := response["command"].(string)

	// app.logger.Info("Camera control response received",
	//     "camera_id", cameraID,
	//     "command", command,
	//     "response", response)

	// // Handle different types of camera control responses
	// switch command {
	// case "ptz":
	//     return app.handlePTZResponse(cameraID, response)
	// case "stream":
	//     return app.handleStreamResponse(cameraID, response)
	// case "preset":
	//     return app.handlePresetResponse(cameraID, response)
	// default:
	//     app.logger.Warn("Unknown camera control command", "command", command)
	// }

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

	app.sioClient.RegisterEventHandler("custom_response", func(data json.RawMessage) error {
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

func (app *Application) sendCameraDetails() {
	// Collect basic camera info
	cameraList := make([]map[string]interface{}, 0, len(app.config.Cameras))
	enabledCount := 0
	for _, camera := range app.config.Cameras {
		cameraList = append(cameraList, map[string]interface{}{
			"id":         camera.ID,
			"name":       camera.Name,
			"rtspUrl":    camera.RTSPUrl,
			"enabled":    camera.Enabled,
			"ptzEnabled": camera.PTZEnabled,
			"username":   camera.Username,
			"password":   camera.Password,
			"onvifPort":  camera.ONVIFPort,
			"liveUrl":    camera.LiveUrl,
		})
		if camera.Enabled {
			enabledCount++
		}
	}

	// Create welcome data
	cameraData := map[string]interface{}{
		"agent_id":        app.config.Agent.ID,
		"message":         "Agent connected and ready",
		"timestamp":       time.Now(),
		"total_cameras":   len(app.config.Cameras),
		"enabled_cameras": enabledCount,
		"cameras":         cameraList,
	}

	// Marshal to JSON
	data, err := json.Marshal(cameraData)
	if err != nil {
		app.logger.Error("Failed to marshal camera data", "error", err)
		return
	}

	// Create Message
	msg := socketio.Message{
		Type:      "send_cameras_details",
		Timestamp: time.Now(),
		Data:      json.RawMessage(data),
	}

	// Send using SendMessage
	if err := app.sioClient.SendMessage(msg); err != nil {
		app.logger.Error("Failed to send camera details", "error", err)
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
			Host:           "api-sio.growloc.farm",
			Port:           443,
			Path:           "/socket.io",
			ReconnectDelay: 5 * time.Second,
			PingInterval:   30 * time.Second,
			TLS:            true,
		},
		Cameras: []config.CameraConfig{
			{
				ID:         "67f8b433854b6df4713f418b",
				Name:       "R&D Zone 1",
				RTSPUrl:    "rtsp://admin:Secure04@192.168.0.105:554/Streaming/Unicast/channels/101",
				Enabled:    true,
				PTZEnabled: true,
				Username:   "admin",
				Password:   "Secure04",
				ONVIFPort:  80,
				StreamID:   "camera1",
				// LiveUrl:    "https://surveillance-apis.sandbox.growloc.farm/live/camera1.flv",
				LiveUrl: "https://surveillance-api.growloc.farm/live/camera1.flv",
			},
			{
				ID:         "67f8b442854b6df4713f418c",
				Name:       "R&D Zone 2",
				RTSPUrl:    "rtsp://admin:Secure04@192.168.0.105:554/Streaming/Unicast/channels/201",
				Enabled:    true,
				PTZEnabled: false,
				Username:   "admin",
				Password:   "Secure04",
				ONVIFPort:  80,
				StreamID:   "camera2",
				// LiveUrl:    "https://surveillance-apis.sandbox.growloc.farm/live/camera2.flv",
				LiveUrl: "https://surveillance-api.growloc.farm/live/camera2.flv",
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
			ExtraArgs:    "",
		},
		RTMP: config.RTMPConfig{
			Host:    "surveillance-stream.growloc.farm",
			Port:    9052,
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
			Host:           "api-sio.growloc.farm",
			Port:           443,
			Path:           "/socket.io",
			ReconnectDelay: 5 * time.Second,
			PingInterval:   30 * time.Second,
			TLS:            true,
		},
		Cameras: []config.CameraConfig{
			{
				ID:         "67f8b433854b6df4713f418b",
				Name:       "R&D Zone 1",
				RTSPUrl:    "rtsp://admin:Secure04@192.168.0.105:554/Streaming/Unicast/channels/101",
				Enabled:    true,
				PTZEnabled: true,
				Username:   "admin",
				Password:   "Secure04",
				ONVIFPort:  80,
				StreamID:   "camera1",
				// LiveUrl:    "https://surveillance-apis.sandbox.growloc.farm/live/camera1.flv",
				LiveUrl: "https://surveillance-api.growloc.farm/live/camera1.flv",
			},
			{
				ID:         "67f8b442854b6df4713f418c",
				Name:       "R&D Zone 2",
				RTSPUrl:    "rtsp://admin:Secure04@192.168.0.105:554/Streaming/Unicast/channels/201",
				Enabled:    true,
				PTZEnabled: false,
				Username:   "admin",
				Password:   "Secure04",
				ONVIFPort:  80,
				StreamID:   "camera2",
				// LiveUrl:    "https://surveillance-apis.sandbox.growloc.farm/live/camera2.flv",
				LiveUrl: "https://surveillance-api.growloc.farm/live/camera2.flv",
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
			Host:    "surveillance-stream.growloc.farm",
			Port:    9052,
			AppName: "live",
		},
		Updater: config.UpdaterConfig{
			URL: "https://s3.ap-south-2.amazonaws.com/prod.growloc.farm/agents/cctv-agent",
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
