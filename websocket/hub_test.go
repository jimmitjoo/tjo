package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHub(t *testing.T) {
	hub := NewHub(nil)
	assert.NotNil(t, hub)
	assert.NotNil(t, hub.clients)
	assert.NotNil(t, hub.rooms)
	assert.NotNil(t, hub.broadcast)
	assert.NotNil(t, hub.register)
	assert.NotNil(t, hub.unregister)
	assert.NotNil(t, hub.roomMessages)
	assert.NotNil(t, hub.config)
}

func TestHubWithCustomConfig(t *testing.T) {
	config := NewConfig(
		WithMaxMessageSize(1024),
		WithBufferSizes(128, 128, 128),
	)
	hub := NewHub(config)
	
	assert.Equal(t, int64(1024), hub.config.MaxMessageSize)
	assert.Equal(t, 128, hub.config.BroadcastBuffer)
}

func TestHubRun(t *testing.T) {
	hub := NewHub(DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	
	done := make(chan bool)
	go func() {
		hub.Run(ctx)
		done <- true
	}()
	
	cancel()
	
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Hub did not shut down within timeout")
	}
}

func TestHubClientRegistration(t *testing.T) {
	hub := NewHub(DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	go hub.Run(ctx)
	
	client := &Client{
		hub:      hub,
		send:     make(chan []byte, 256),
		id:       "test-client",
		userID:   "test-user",
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
	
	hub.register <- client
	time.Sleep(10 * time.Millisecond)
	
	assert.Equal(t, 1, hub.GetConnectedClients())
	
	hub.unregister <- client
	time.Sleep(10 * time.Millisecond)
	
	assert.Equal(t, 0, hub.GetConnectedClients())
}

func TestHubBroadcasting(t *testing.T) {
	hub := NewHub(DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	go hub.Run(ctx)
	
	client1 := &Client{
		hub:      hub,
		send:     make(chan []byte, 256),
		id:       "client1",
		userID:   "user1",
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
	
	client2 := &Client{
		hub:      hub,
		send:     make(chan []byte, 256),
		id:       "client2",
		userID:   "user2",
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
	
	hub.register <- client1
	hub.register <- client2
	time.Sleep(10 * time.Millisecond)
	
	message := []byte("test broadcast message")
	hub.BroadcastToAll(message)
	
	select {
	case msg := <-client1.send:
		assert.Equal(t, message, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Client1 did not receive broadcast message")
	}
	
	select {
	case msg := <-client2.send:
		assert.Equal(t, message, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Client2 did not receive broadcast message")
	}
}

func TestRoomFunctionality(t *testing.T) {
	hub := NewHub(DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	go hub.Run(ctx)
	
	client1 := &Client{
		hub:      hub,
		send:     make(chan []byte, 256),
		id:       "client1",
		userID:   "user1",
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
	
	client2 := &Client{
		hub:      hub,
		send:     make(chan []byte, 256),
		id:       "client2",
		userID:   "user2",
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
	
	hub.register <- client1
	hub.register <- client2
	time.Sleep(10 * time.Millisecond)
	
	roomName := "test-room"
	hub.JoinRoom(client1, roomName)
	time.Sleep(10 * time.Millisecond)
	
	assert.Equal(t, 1, hub.GetRoomClients(roomName))
	assert.Contains(t, hub.GetRooms(), roomName)
	assert.Contains(t, client1.GetRooms(), roomName)
	
	message := []byte("room message")
	hub.BroadcastToRoom(roomName, message, nil)
	
	select {
	case msg := <-client1.send:
		assert.Equal(t, message, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Client1 did not receive room message")
	}
	
	select {
	case <-client2.send:
		t.Fatal("Client2 should not receive room message")
	case <-time.After(50 * time.Millisecond):
	}
	
	hub.LeaveRoom(client1, roomName)
	time.Sleep(10 * time.Millisecond)
	
	assert.Equal(t, 0, hub.GetRoomClients(roomName))
	assert.NotContains(t, hub.GetRooms(), roomName)
	assert.NotContains(t, client1.GetRooms(), roomName)
}

func TestClientMessageHandling(t *testing.T) {
	client := &Client{
		id:       "test-client",
		userID:   "test-user",
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
	
	client.SetMetadata("key1", "value1")
	assert.Equal(t, "value1", client.GetMetadata("key1"))
	
	assert.Equal(t, "test-client", client.GetID())
	assert.Equal(t, "test-user", client.GetUserID())
}

func TestWebSocketUpgrade(t *testing.T) {
	config := NewConfig(WithAllowedOrigins([]string{"*"}))
	hub := NewHub(config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, hub.GetConnectedClients())
}

func TestMessageTypes(t *testing.T) {
	config := NewConfig(WithAllowedOrigins([]string{"*"}))
	hub := NewHub(config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	roomMsg := Message{
		Type: "join_room",
		Data: "test-room",
	}

	msgBytes, err := json.Marshal(roomMsg)
	require.NoError(t, err)

	err = conn.WriteMessage(websocket.TextMessage, msgBytes)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, hub.GetRoomClients("test-room"))
}

func TestOriginValidation(t *testing.T) {
	t.Run("rejects when no origins configured", func(t *testing.T) {
		hub := NewHub(DefaultConfig()) // No origins = reject all
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go hub.Run(ctx)

		server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

		_, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		assert.Error(t, err, "should reject connection when no origins configured")
	})

	t.Run("accepts wildcard origin", func(t *testing.T) {
		config := NewConfig(WithAllowedOrigins([]string{"*"}))
		hub := NewHub(config)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go hub.Run(ctx)

		server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		assert.NoError(t, err, "should accept connection with wildcard origin")
		if conn != nil {
			conn.Close()
		}
	})

	t.Run("accepts matching origin", func(t *testing.T) {
		config := NewConfig(WithAllowedOrigins([]string{"http://example.com", "http://localhost"}))
		hub := NewHub(config)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go hub.Run(ctx)

		server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

		header := http.Header{}
		header.Set("Origin", "http://example.com")
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		assert.NoError(t, err, "should accept connection from allowed origin")
		if conn != nil {
			conn.Close()
		}
	})

	t.Run("rejects non-matching origin", func(t *testing.T) {
		config := NewConfig(WithAllowedOrigins([]string{"http://example.com"}))
		hub := NewHub(config)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go hub.Run(ctx)

		server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

		header := http.Header{}
		header.Set("Origin", "http://malicious.com")
		_, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		assert.Error(t, err, "should reject connection from non-allowed origin")
	})
}

func TestConfigOptions(t *testing.T) {
	config := NewConfig(
		WithWriteTimeout(5*time.Second),
		WithPongTimeout(30*time.Second),
		WithMaxMessageSize(2048),
		WithBufferSizes(512, 512, 512),
		WithAllowedOrigins([]string{"http://example.com"}),
	)

	assert.Equal(t, 5*time.Second, config.WriteWait)
	assert.Equal(t, 30*time.Second, config.PongWait)
	assert.Equal(t, int64(2048), config.MaxMessageSize)
	assert.Equal(t, 512, config.BroadcastBuffer)
	assert.Equal(t, 512, config.RoomMessageBuffer)
	assert.Equal(t, 512, config.ClientBuffer)
	assert.Equal(t, []string{"http://example.com"}, config.AllowedOrigins)
}

func TestEventHandlers(t *testing.T) {
	connectCalled := false
	disconnectCalled := false
	joinRoomCalled := false
	leaveRoomCalled := false
	
	config := NewConfig(
		WithOnConnect(func(c *Client) {
			connectCalled = true
		}),
		WithOnDisconnect(func(c *Client) {
			disconnectCalled = true
		}),
		WithOnJoinRoom(func(c *Client, room string) {
			joinRoomCalled = true
		}),
		WithOnLeaveRoom(func(c *Client, room string) {
			leaveRoomCalled = true
		}),
	)
	
	hub := NewHub(config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	go hub.Run(ctx)
	
	client := &Client{
		hub:      hub,
		send:     make(chan []byte, 256),
		id:       "test-client",
		userID:   "test-user",
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
	
	hub.register <- client
	time.Sleep(10 * time.Millisecond)
	assert.True(t, connectCalled)
	
	hub.JoinRoom(client, "test-room")
	time.Sleep(10 * time.Millisecond)
	assert.True(t, joinRoomCalled)
	
	hub.LeaveRoom(client, "test-room")
	time.Sleep(10 * time.Millisecond)
	assert.True(t, leaveRoomCalled)
	
	hub.unregister <- client
	time.Sleep(10 * time.Millisecond)
	assert.True(t, disconnectCalled)
}