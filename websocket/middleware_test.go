package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDefaultHub(t *testing.T) {
	hub1 := GetDefaultHub()
	hub2 := GetDefaultHub()
	
	assert.Same(t, hub1, hub2, "GetDefaultHub should return the same instance")
}

func TestSetDefaultHub(t *testing.T) {
	customHub := NewHub(DefaultConfig())
	SetDefaultHub(customHub)
	
	retrievedHub := GetDefaultHub()
	assert.Same(t, customHub, retrievedHub)
}

func TestRegisterRoutes(t *testing.T) {
	config := NewConfig(WithAllowedOrigins([]string{"*"}))
	hub := NewHub(config)
	SetDefaultHub(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	router := chi.NewRouter()
	RegisterRoutes(router, "/websocket")

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/websocket"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, GetConnectedClients())
}

func TestRegisterRoutesWithDefaultPath(t *testing.T) {
	config := NewConfig(WithAllowedOrigins([]string{"*"}))
	hub := NewHub(config)
	SetDefaultHub(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	router := chi.NewRouter()
	RegisterRoutes(router, "")

	ts := httptest.NewServer(router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()
	
	client := &Client{
		id:       "test-client",
		userID:   "test-user",
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
	
	hub := NewHub(DefaultConfig())
	
	ctxWithClient := WithClient(ctx, client)
	ctxWithHub := WithHub(ctxWithClient, hub)
	
	retrievedClient, ok := GetClient(ctxWithHub)
	assert.True(t, ok)
	assert.Same(t, client, retrievedClient)
	
	retrievedHub, ok := GetHub(ctxWithHub)
	assert.True(t, ok)
	assert.Same(t, hub, retrievedHub)
	
	_, ok = GetClient(ctx)
	assert.False(t, ok)
	
	_, ok = GetHub(ctx)
	assert.False(t, ok)
}

func TestGlobalBroadcastFunctions(t *testing.T) {
	hub := NewHub(DefaultConfig())
	SetDefaultHub(hub)
	
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
	
	message := []byte("global broadcast test")
	BroadcastToAll(message)
	
	select {
	case msg := <-client.send:
		assert.Equal(t, message, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Client did not receive global broadcast message")
	}
	
	hub.JoinRoom(client, "test-room")
	time.Sleep(10 * time.Millisecond)
	
	roomMessage := []byte("room broadcast test")
	BroadcastToRoom("test-room", roomMessage, nil)
	
	select {
	case msg := <-client.send:
		assert.Equal(t, roomMessage, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Client did not receive room broadcast message")
	}
	
	assert.Equal(t, 1, GetConnectedClients())
	assert.Equal(t, 1, GetRoomClients("test-room"))
	assert.Contains(t, GetRooms(), "test-room")
}

func TestAuthMiddleware(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	
	middleware := AuthMiddleware(handler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	
	middleware.ServeHTTP(w, req)
	
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}