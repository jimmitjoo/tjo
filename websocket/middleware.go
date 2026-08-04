package websocket

import (
	"context"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

var (
	defaultHub     *Hub
	defaultHubOnce sync.Once
)

func GetDefaultHub() *Hub {
	defaultHubOnce.Do(func() {
		defaultHub = NewHub(DefaultConfig())
		go defaultHub.Run(context.Background())
	})
	return defaultHub
}

func SetDefaultHub(hub *Hub) {
	defaultHub = hub
}

func WSHandler(w http.ResponseWriter, r *http.Request) {
	hub := GetDefaultHub()
	hub.ServeWS(w, r)
}

func RegisterRoutes(router chi.Router, path string) {
	if path == "" {
		path = "/ws"
	}
	router.Get(path, WSHandler)
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

type ContextKey string

const (
	ClientContextKey ContextKey = "websocket_client"
	HubContextKey    ContextKey = "websocket_hub"
)

func WithClient(ctx context.Context, client *Client) context.Context {
	return context.WithValue(ctx, ClientContextKey, client)
}

func GetClient(ctx context.Context) (*Client, bool) {
	client, ok := ctx.Value(ClientContextKey).(*Client)
	return client, ok
}

func WithHub(ctx context.Context, hub *Hub) context.Context {
	return context.WithValue(ctx, HubContextKey, hub)
}

func GetHub(ctx context.Context) (*Hub, bool) {
	hub, ok := ctx.Value(HubContextKey).(*Hub)
	return hub, ok
}

func BroadcastToAll(message []byte) {
	hub := GetDefaultHub()
	hub.BroadcastToAll(message)
}

func BroadcastToRoom(roomName string, message []byte, exclude *Client) {
	hub := GetDefaultHub()
	hub.BroadcastToRoom(roomName, message, exclude)
}

func GetConnectedClients() int {
	hub := GetDefaultHub()
	return hub.GetConnectedClients()
}

func GetRoomClients(roomName string) int {
	hub := GetDefaultHub()
	return hub.GetRoomClients(roomName)
}

func GetRooms() []string {
	hub := GetDefaultHub()
	return hub.GetRooms()
}
