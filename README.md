# CCTV Agent

A modular, performant, and resilient Go background agent for Raspberry Pi that manages CCTV cameras with RTSP streaming, ONVIF PTZ control, and remote management capabilities.

## Features

- **RTSP Stream Management**: Connect to IP cameras via RTSP with automatic reconnection
- **FFmpeg Integration**: Stream video processing with configurable encoding parameters
- **ONVIF PTZ Control**: Control camera pan, tilt, zoom, and presets via ONVIF protocol
- **WebSocket Communication**: Real-time bidirectional communication for commands and status
- **OTA Updates**: Over-the-air updates with version checking and safe rollback
- **System Monitoring**: Track CPU, memory, disk, network, and temperature metrics
- **Auto-start Service**: Systemd integration for automatic startup on boot
- **Structured Logging**: JSON-formatted logs with configurable levels
- **Configuration Management**: JSON-based configuration with hot-reload support

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     CCTV Agent                          │
├─────────────────────────────────────────────────────────┤
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐ │
│  │  Config  │  │  Logger  │  │ Monitor  │  │Updater │ │
│  └──────────┘  └──────────┘  └──────────┘  └────────┘ │
│  ┌──────────────────────────────────────────────────┐  │
│  │              Stream Manager                      │  │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐      │  │
│  │  │ Stream 1 │  │ Stream 2 │  │ Stream N │ ...  │  │
│  │  └──────────┘  └──────────┘  └──────────┘      │  │
│  └──────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────┐  │
│  │            WebSocket Client                      │  │
│  │  Commands: PTZ, Config, Update, Stream Control  │  │
│  └──────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────┐  │
│  │            ONVIF Controller                      │  │
│  │  PTZ Commands, Presets, Device Management       │  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Requirements

- Raspberry Pi (3/4/5) running Raspbian OS or compatible Linux distribution
- Go 1.21 or higher (for building from source)
- FFmpeg installed on the system
- Network connectivity to IP cameras
- Systemd (for service management)

## Installation

### Quick Install

```bash
# Download and run the installation script
curl -sSL https://raw.githubusercontent.com/your-org/cctv-agent/main/scripts/install.sh | sudo bash
```

### Manual Installation

1. **Build from source:**
```bash
# Clone the repository
git clone https://github.com/your-org/cctv-agent.git
cd cctv-agent

# Install dependencies
make deps

# Build for Raspberry Pi
make build-arm    # For 32-bit ARM (RPi 3 and older)
# OR
make build-arm64  # For 64-bit ARM (RPi 4 and newer)

# Install the binary
sudo cp cctv-agent /usr/local/bin/
sudo chmod +x /usr/local/bin/cctv-agent
```

2. **Create configuration:**
```bash
# Create config directory
sudo mkdir -p /etc/cctv-agent

# Generate sample configuration
cctv-agent --generate-config > /etc/cctv-agent/config.json

# Edit configuration
sudo nano /etc/cctv-agent/config.json
```

3. **Install as service:**
```bash
# Copy service file
sudo cp scripts/cctv-agent.service /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload

# Enable and start service
sudo systemctl enable cctv-agent
sudo systemctl start cctv-agent
```

## Configuration

The agent uses a JSON configuration file located at `/etc/cctv-agent/config.json`:

```json
{
  "agent": {
    "id": "cctv-agent-001",
    "log_level": "info"
  },
  "websocket": {
    "host": "localhost",
    "port": 8080,
    "path": "/ws",
    "reconnect_delay": "5s",
    "ping_interval": "30s",
    "tls": false
  },
  "cameras": [
    {
      "id": "camera1",
      "name": "Front Door Camera",
      "rtsp_url": "rtsp://admin:password@192.168.1.100:554/stream1",
      "ptz_enabled": true,
      "username": "admin",
      "password": "password",
      "onvif_port": 80
    }
  ],
  "ffmpeg": {
    "preset": "ultrafast",
    "tune": "zerolatency",
    "crf": 23,
    "max_rate": "2M",
    "buf_size": "4M",
    "audio_bitrate": "128k",
    "audio_rate": 44100,
    "video_codec": "libx264",
    "audio_codec": "aac",
    "log_level": "error",
    "extra_args": "-rtsp_transport tcp"
  },
  "updater": {
    "enabled": true,
    "url": "https://updates.example.com/cctv-agent",
    "interval": 3600,
    "auto_update": false
  }
}
```

### Configuration Parameters

#### Agent Configuration
- `id`: Unique identifier for this agent instance
- `log_level`: Logging level (debug, info, warn, error)

#### WebSocket Configuration
- `host`: WebSocket server hostname
- `port`: WebSocket server port
- `path`: WebSocket endpoint path
- `reconnect_delay`: Delay between reconnection attempts
- `ping_interval`: Interval for WebSocket ping messages
- `tls`: Enable TLS/SSL for WebSocket connection

#### Camera Configuration
- `id`: Unique camera identifier
- `name`: Human-readable camera name
- `rtsp_url`: RTSP stream URL
- `ptz_enabled`: Enable PTZ control for this camera
- `username`: Camera authentication username
- `password`: Camera authentication password
- `onvif_port`: ONVIF service port (usually 80)

#### FFmpeg Configuration
- `preset`: Encoding preset (ultrafast, superfast, veryfast, faster, fast, medium, slow, slower, veryslow)
- `tune`: Encoding tuning (zerolatency for live streaming)
- `crf`: Constant Rate Factor (0-51, lower = better quality)
- `max_rate`: Maximum bitrate
- `buf_size`: Buffer size
- `audio_bitrate`: Audio encoding bitrate
- `audio_rate`: Audio sample rate
- `video_codec`: Video codec (libx264, h264_omx for hardware encoding)
- `audio_codec`: Audio codec (aac, mp3)
- `log_level`: FFmpeg log level
- `extra_args`: Additional FFmpeg arguments

#### Updater Configuration
- `enabled`: Enable OTA updates
- `url`: Update server URL
- `interval`: Update check interval in seconds
- `auto_update`: Automatically install updates

## Usage

### Command Line Options

```bash
# Start the agent
cctv-agent

# Start with custom config
cctv-agent --config /path/to/config.json

# Generate sample configuration
cctv-agent --generate-config

# Show version
cctv-agent --version

# Enable debug logging
cctv-agent --debug
```

### Service Management

```bash
# Start service
sudo systemctl start cctv-agent

# Stop service
sudo systemctl stop cctv-agent

# Restart service
sudo systemctl restart cctv-agent

# Check status
sudo systemctl status cctv-agent

# View logs
sudo journalctl -u cctv-agent -f
```

## WebSocket API

The agent communicates with a central server via WebSocket. The following message types are supported:

### Outgoing Messages (Agent → Server)

#### Registration
```json
{
  "type": "registration",
  "timestamp": "2024-01-01T12:00:00Z",
  "data": {
    "agent_id": "cctv-agent-001",
    "name": "raspberrypi",
    "location": "Raspberry Pi",
    "version": "1.0.0"
  }
}
```

#### Status Report
```json
{
  "type": "status",
  "timestamp": "2024-01-01T12:00:00Z",
  "data": {
    "agent_id": "cctv-agent-001",
    "version": "1.0.0",
    "uptime": 3600,
    "camera_status": {
      "camera1": {
        "id": "camera1",
        "connected": true,
        "streaming": true,
        "last_update": "2024-01-01T12:00:00Z",
        "error": ""
      }
    },
    "system_info": {
      "cpu": {
        "usage": 25.5,
        "cores": 4
      },
      "memory": {
        "total": 4096,
        "used": 1024,
        "percent": 25.0
      },
      "disk": {
        "total": 32000,
        "used": 8000,
        "percent": 25.0
      },
      "temperature": 45.5
    }
  }
}
```

### Incoming Commands (Server → Agent)

#### PTZ Control
```json
{
  "type": "ptz",
  "camera_id": "camera1",
  "data": {
    "action": "move",
    "pan": 0.5,
    "tilt": 0.3,
    "zoom": 0.1
  }
}
```

#### Stream Control
```json
{
  "type": "stream",
  "camera_id": "camera1",
  "data": {
    "action": "start"
  }
}
```

#### Configuration Update
```json
{
  "type": "config",
  "data": {
    "cameras": [...]
  }
}
```

#### Update Command
```json
{
  "type": "update",
  "data": {
    "version": "1.0.1",
    "url": "https://updates.example.com/cctv-agent/v1.0.1"
  }
}
```

## Development

### Project Structure

```
cctv-agent/
├── main.go                 # Main application entry point
├── config/
│   └── config.go          # Configuration structures and loading
├── internal/
│   ├── logger/
│   │   └── logger.go      # Structured logging
│   ├── monitor/
│   │   └── system.go      # System monitoring
│   ├── onvif/
│   │   └── controller.go  # ONVIF PTZ control
│   ├── stream/
│   │   ├── manager.go     # Stream management
│   │   └── stream.go      # Individual stream handling
│   ├── updater/
│   │   └── updater.go     # OTA update handling
│   └── websocket/
│       ├── client.go      # WebSocket client
│       └── types.go       # Message types
├── scripts/
│   ├── install.sh         # Installation script
│   └── cctv-agent.service # Systemd service file
├── Makefile               # Build automation
├── go.mod                 # Go module definition
└── go.sum                 # Dependency checksums
```

### Building

```bash
# Build for current platform
make build

# Build for Raspberry Pi ARM
make build-arm

# Build for Raspberry Pi ARM64
make build-arm64

# Run tests
make test

# Format code
make fmt

# Run linter
make lint

# Clean build artifacts
make clean
```

### Testing

```bash
# Run unit tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/stream

# Run with verbose output
go test -v ./...
```

## Troubleshooting

### Common Issues

#### Camera Connection Failed
- Verify RTSP URL is correct
- Check network connectivity to camera
- Ensure camera credentials are correct
- Try accessing RTSP stream with VLC or ffplay

#### High CPU Usage
- Adjust FFmpeg encoding parameters
- Use hardware encoding if available (h264_omx on Raspberry Pi)
- Reduce video resolution or framerate
- Increase CRF value for lower quality/bitrate

#### WebSocket Connection Issues
- Check WebSocket server is running
- Verify firewall rules allow WebSocket traffic
- Check TLS certificate if using secure WebSocket
- Review agent logs for connection errors

#### Service Won't Start
- Check configuration file syntax
- Verify FFmpeg is installed
- Ensure proper file permissions
- Review systemd journal logs

### Debug Mode

Enable debug logging for detailed troubleshooting:

```bash
# Via command line
cctv-agent --debug

# Via configuration
{
  "agent": {
    "log_level": "debug"
  }
}

# Via environment variable
export CCTV_AGENT_DEBUG=true
```

## Security Considerations

- Store camera credentials securely
- Use TLS for WebSocket connections in production
- Regularly update the agent software
- Implement network segmentation for cameras
- Use strong passwords for camera access
- Enable firewall rules to restrict access
- Monitor agent logs for suspicious activity

## Performance Optimization

### Raspberry Pi Specific

- Use hardware video encoding when available
- Optimize FFmpeg parameters for ARM processors
- Monitor temperature and throttling
- Use adequate cooling for sustained operation
- Consider using SSD for better I/O performance
- Adjust GPU memory split if needed

### Network Optimization

- Use wired Ethernet when possible
- Configure appropriate buffer sizes
- Use TCP transport for RTSP streams
- Implement local caching if needed
- Monitor network bandwidth usage

## Contributing

Contributions are welcome! Please follow these guidelines:

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues, questions, or suggestions:
- Open an issue on GitHub
- Contact support at support@example.com
- Check the documentation wiki

## Acknowledgments

- FFmpeg team for video processing
- ONVIF for camera control standards
- Gorilla WebSocket for Go WebSocket implementation
- Raspberry Pi Foundation for hardware platform
