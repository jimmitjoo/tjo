package websocket

import (
	"context"
	"net/http"
	"sync"
)

// Module implements the tjo.Module interface for WebSocket functionality.
// Use this to opt-in to WebSocket support in your application.
//
// Example:
//
//	app := tjo.Tjo{}
//	app.New(rootPath, websocket.NewModule(
//	    websocket.WithAllowedOrigins([]string{"https://example.com"}),
//	    websocket.WithAuthenticateConnection(myAuthFunc),
//	))
//
//	// Later, use the hub:
//	if wsModule := app.Modules.Get("websocket"); wsModule != nil {
//	    hub := wsModule.(*websocket.Module).Hub
//	    hub.Broadcast([]byte("Hello everyone!"))
//	}
type Module struct {
	Hub    *Hub
	config *Config

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewModule creates a new WebSocket module with the given configuration options.
// It uses the same Option functions as NewConfig.
func NewModule(opts ...Option) *Module {
	return &Module{
		config: NewConfig(opts...),
	}
}

// Name returns the module identifier
func (m *Module) Name() string {
	return "websocket"
}

// Initialize creates the Hub and starts its run loop.
// This is called automatically during app.New().
func (m *Module) Initialize(g interface{}) error {
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.Hub = NewHub(m.config)

	// Start the hub in a goroutine
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.Hub.Run(m.ctx)
	}()

	return nil
}

// Shutdown gracefully stops the WebSocket hub.
// This closes all connections and waits for the hub to finish.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}

	// Wait for hub to finish (with timeout from context)
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Handler returns an HTTP handler for WebSocket connections.
// Mount this at your desired path (e.g., "/ws").
func (m *Module) Handler() http.HandlerFunc {
	return m.Hub.ServeWS
}

// Broadcast sends a message to all connected clients
func (m *Module) Broadcast(message []byte) {
	if m.Hub != nil {
		m.Hub.BroadcastToAll(message)
	}
}

// BroadcastToRoom sends a message to all clients in a room.
// Use exclude to exclude a specific client (e.g., the sender), or nil to include all.
func (m *Module) BroadcastToRoom(room string, message []byte, exclude *Client) {
	if m.Hub != nil {
		m.Hub.BroadcastToRoom(room, message, exclude)
	}
}

// GetClientCount returns the number of connected clients
func (m *Module) GetClientCount() int {
	if m.Hub != nil {
		return m.Hub.GetConnectedClients()
	}
	return 0
}
