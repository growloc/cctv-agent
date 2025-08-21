package socketio

import (
	"context"
	"encoding/json"
	"github.com/zishang520/engine.io/v2/events"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cctv-agent/internal/logger"
	eio_transports "github.com/zishang520/engine.io-client-go/transports"
	eio_types "github.com/zishang520/engine.io/v2/types"
	sio_socket "github.com/zishang520/socket.io-client-go/socket"
)

// Client represents a Socket.IO client
type Client struct {
	rawURL       string
	baseURL      string
	namespace    string
	manager      *sio_socket.Manager
	logger       logger.Logger
	mu           sync.RWMutex
	connected    bool
	reconnecting bool
	ctx          context.Context
	cancel       context.CancelFunc
	handlers     map[string]func(json.RawMessage) error
	onConnect    func()
	onDisconnect func()
	socket       *sio_socket.Socket
}

// NewClient creates a new Socket.IO client
func NewClient(raw string, logger logger.Logger) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		rawURL:   raw,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
		handlers: make(map[string]func(json.RawMessage) error),
	}
	c.parseURL()
	return c
}

// Connect establishes connection to Socket.IO server
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Configure manager options: Engine.IO path + allow Polling and WebSocket
	opts := sio_socket.DefaultOptions()
	opts.SetTransports(eio_types.NewSet(eio_transports.WebSocket))

	c.logger.Info("Connecting to Socket.IO server", "base", c.baseURL, "ns", c.namespace, "transport", "websocket")

	mgr := sio_socket.NewManager(c.baseURL, opts)
	// Use root namespace for application events; Engine.IO path is set above
	io := mgr.Socket(c.namespace, opts)
	c.socket = io

	// Register manager-level events
	mgr.On(events.EventName("error"), func(args ...any) {
		c.logger.Error("Manager error", "args", args)
	})
	mgr.On(events.EventName("ping"), func(args ...any) {
		c.logger.Debug("Manager ping", "data", args)
		c.socket.Emit("ping", args)
	})
	mgr.On(events.EventName("reconnect"), func(args ...any) {
		c.logger.Warn("Manager reconnected")
	})
	mgr.On(events.EventName("reconnect_attempt"), func(args ...any) {
		c.logger.Warn("Manager reconnect attempt")
	})
	mgr.On(events.EventName("reconnect_error"), func(args ...any) {
		c.logger.Error("Manager reconnect error", "args", args)
	})
	mgr.On(events.EventName("reconnect_failed"), func(args ...any) {
		c.logger.Error("Manager reconnect failed", "args", args)
	})

	// Register core socket events
	// connect
	io.On(events.EventName("connect"), func(args ...any) {
		c.logger.Info("Connected to Socket.IO server")
		c.mu.Lock()
		c.connected = true
		c.reconnecting = false
		c.mu.Unlock()
		if c.onConnect != nil {
			c.onConnect()
		}
	})
	// disconnect
	io.On(events.EventName("disconnect"), func(args ...any) {
		c.logger.Warn("Disconnected from Socket.IO server")
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
		if c.onDisconnect != nil {
			c.onDisconnect()
		}
		go c.reconnect()
	})
	// error
	io.On(events.EventName("connect_error"), func(args ...any) {
		c.logger.Error("Socket.IO connect_error", "args", args)
	})

	// Register custom event handlers
	for event, handler := range c.handlers {
		ev := event
		h := handler
		io.On(events.EventName(ev), func(args ...any) {
			b, err := json.Marshal(args)
			if err != nil {
				c.logger.Error("Failed to marshal event args", "event", ev, "error", err)
				return
			}
			if err := h(json.RawMessage(b)); err != nil {
				c.logger.Error("Handler error", "event", ev, "error", err)
			}
		})
	}

	c.manager = mgr

	// Note: We do not keep a separate reference to the Manager. Socket-level events
	// like connect/connect_error/disconnect are sufficient for our use-cases.

	c.connected = false // will flip on "connect"
	c.logger.Info("Socket.IO client initialized", "base", c.baseURL, "ns", c.namespace)
	return nil
}

// Disconnect closes the Socket.IO connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cancel()

	if c.manager != nil {
		c.manager.Clear()
	}
	c.connected = false
	c.logger.Info("Socket.IO client disconnected")
	return nil
}

// Emit sends an event to the server
func (c *Client) Emit(event string, data interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.socket == nil {
		return nil
	}

	err := c.socket.Emit(event, data)
	if err != nil {
		return err
	}

	return nil
}

// RegisterEventHandler registers an event handler
func (c *Client) RegisterEventHandler(event string, handler func(json.RawMessage) error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handlers[event] = handler
}

// OnConnect sets the connection handler
func (c *Client) OnConnect(handler func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnect = handler
}

// OnDisconnect sets the disconnection handler
func (c *Client) OnDisconnect(handler func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDisconnect = handler
}

// IsConnected returns the connection status
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// reconnect attempts to reconnect to the server
func (c *Client) reconnect() {
	c.mu.Lock()
	if c.reconnecting {
		c.mu.Unlock()
		return
	}
	c.reconnecting = true
	c.mu.Unlock()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.logger.Info("Attempting to reconnect to Socket.IO server")
			if err := c.Connect(); err != nil {
				c.logger.Error("Reconnection failed", "error", err)
				continue
			}
			return
		}
	}
}

// SendMessage sends a message with a specific event type
func (c *Client) SendMessage(msg Message) error {
	return c.Emit(msg.Type, msg.Data)
}

// parseURL splits rawURL into baseURL and namespace.
// Examples:
//   - ws://host:9054/custom-socket -> base: http://host:9054, ns: /custom-socket
//   - http://host:9054/custom-socket -> base: http://host:9054, ns: /custom-socket
//   - http://host:9054 -> base: http://host:9054, ns: "/"
func (c *Client) parseURL() {
	u := c.rawURL
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") &&
		!strings.HasPrefix(u, "ws://") && !strings.HasPrefix(u, "wss://") {
		u = "http://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		c.logger.Error("invalid socket.io url", "url", c.rawURL, "error", err)
		// fallback
		c.baseURL = u
		c.namespace = "/"
		return
	}
	// Map ws(s) -> http(s) for manager
	scheme := parsed.Scheme
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	}
	host := parsed.Host
	base := scheme + "://" + host
	ns := parsed.Path
	if ns == "" {
		ns = "/"
	}
	// ensure leading '/'
	if !strings.HasPrefix(ns, "/") {
		ns = "/" + ns
	}
	c.baseURL = base
	c.namespace = ns
}
