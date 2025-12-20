package websocket

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	clients      map[*Client]bool
	rooms        map[string]*Room
	broadcast    chan []byte
	register     chan *Client
	unregister   chan *Client
	roomMessages chan *RoomMessage
	mu           sync.RWMutex
	config       *Config
	upgrader     websocket.Upgrader
}

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	id       string
	userID   string
	rooms    map[string]bool
	metadata map[string]interface{}
	mu       sync.RWMutex
}

type Room struct {
	name    string
	clients map[*Client]bool
	mu      sync.RWMutex
}

type RoomMessage struct {
	Room    string
	Message []byte
	Exclude *Client
}

type Message struct {
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data,omitempty"`
	Room      string                 `json:"room,omitempty"`
	UserID    string                 `json:"user_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

func NewHub(config *Config) *Hub {
	if config == nil {
		config = DefaultConfig()
	}

	hub := &Hub{
		clients:      make(map[*Client]bool),
		rooms:        make(map[string]*Room),
		broadcast:    make(chan []byte, config.BroadcastBuffer),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		roomMessages: make(chan *RoomMessage, config.RoomMessageBuffer),
		config:       config,
	}

	// Configure upgrader with origin checking
	hub.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     hub.checkOrigin,
	}

	return hub
}

// checkOrigin validates the origin of WebSocket connections.
// If AllowedOrigins is empty, all origins are rejected (secure default).
// If AllowedOrigins contains "*", all origins are allowed.
// Otherwise, origin must match one of the allowed origins exactly.
func (h *Hub) checkOrigin(r *http.Request) bool {
	// If no origins configured, reject all (secure default)
	if len(h.config.AllowedOrigins) == 0 {
		log.Printf("WebSocket connection rejected: no allowed origins configured")
		return false
	}

	origin := r.Header.Get("Origin")

	// Check each allowed origin
	for _, allowed := range h.config.AllowedOrigins {
		// Wildcard allows all origins (use with caution)
		if allowed == "*" {
			return true
		}
		// Exact match
		if origin == allowed {
			return true
		}
	}

	log.Printf("WebSocket connection rejected: origin %q not in allowed list", origin)
	return false
}

func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.shutdown()
			return
		case client := <-h.register:
			h.registerClient(client)
		case client := <-h.unregister:
			h.unregisterClient(client)
		case message := <-h.broadcast:
			h.broadcastToAll(message)
		case roomMsg := <-h.roomMessages:
			h.broadcastToRoom(roomMsg)
		}
	}
}

func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	h.clients[client] = true
	h.mu.Unlock()

	if h.config.OnConnect != nil {
		h.config.OnConnect(client)
	}

	log.Printf("Client %s connected. Total clients: %d", client.id, len(h.clients))
}

func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client.send)

		for roomName := range client.rooms {
			h.leaveRoom(client, roomName)
		}
	}
	h.mu.Unlock()

	if h.config.OnDisconnect != nil {
		h.config.OnDisconnect(client)
	}

	log.Printf("Client %s disconnected. Total clients: %d", client.id, len(h.clients))
}

func (h *Hub) broadcastToAll(message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.send <- message:
		default:
			delete(h.clients, client)
			close(client.send)
		}
	}
}

func (h *Hub) broadcastToRoom(roomMsg *RoomMessage) {
	h.mu.RLock()
	room, exists := h.rooms[roomMsg.Room]
	h.mu.RUnlock()

	if !exists {
		return
	}

	room.mu.RLock()
	defer room.mu.RUnlock()

	for client := range room.clients {
		if client == roomMsg.Exclude {
			continue
		}
		select {
		case client.send <- roomMsg.Message:
		default:
			delete(room.clients, client)
		}
	}
}

func (h *Hub) JoinRoom(client *Client, roomName string) {
	h.mu.Lock()
	room, exists := h.rooms[roomName]
	if !exists {
		room = &Room{
			name:    roomName,
			clients: make(map[*Client]bool),
		}
		h.rooms[roomName] = room
	}
	h.mu.Unlock()

	room.mu.Lock()
	room.clients[client] = true
	room.mu.Unlock()

	client.mu.Lock()
	if client.rooms == nil {
		client.rooms = make(map[string]bool)
	}
	client.rooms[roomName] = true
	client.mu.Unlock()

	if h.config.OnJoinRoom != nil {
		h.config.OnJoinRoom(client, roomName)
	}

	log.Printf("Client %s joined room %s", client.id, roomName)
}

func (h *Hub) LeaveRoom(client *Client, roomName string) {
	h.leaveRoom(client, roomName)
}

func (h *Hub) leaveRoom(client *Client, roomName string) {
	h.mu.RLock()
	room, exists := h.rooms[roomName]
	h.mu.RUnlock()

	if !exists {
		return
	}

	room.mu.Lock()
	delete(room.clients, client)
	isEmpty := len(room.clients) == 0
	room.mu.Unlock()

	client.mu.Lock()
	delete(client.rooms, roomName)
	client.mu.Unlock()

	if isEmpty {
		h.mu.Lock()
		delete(h.rooms, roomName)
		h.mu.Unlock()
	}

	if h.config.OnLeaveRoom != nil {
		h.config.OnLeaveRoom(client, roomName)
	}

	log.Printf("Client %s left room %s", client.id, roomName)
}

func (h *Hub) BroadcastToAll(message []byte) {
	select {
	case h.broadcast <- message:
	default:
		log.Printf("Broadcast channel is full, dropping message")
	}
}

func (h *Hub) BroadcastToRoom(roomName string, message []byte, exclude *Client) {
	roomMsg := &RoomMessage{
		Room:    roomName,
		Message: message,
		Exclude: exclude,
	}

	select {
	case h.roomMessages <- roomMsg:
	default:
		log.Printf("Room message channel is full, dropping message for room %s", roomName)
	}
}

func (h *Hub) GetConnectedClients() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) GetRoomClients(roomName string) int {
	h.mu.RLock()
	room, exists := h.rooms[roomName]
	h.mu.RUnlock()

	if !exists {
		return 0
	}

	room.mu.RLock()
	defer room.mu.RUnlock()
	return len(room.clients)
}

func (h *Hub) GetRooms() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	rooms := make([]string, 0, len(h.rooms))
	for name := range h.rooms {
		rooms = append(rooms, name)
	}
	return rooms
}

func (h *Hub) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		if client.conn != nil {
			client.conn.Close()
		}
		close(client.send)
	}

	close(h.broadcast)
	close(h.register)
	close(h.unregister)
	close(h.roomMessages)
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientID := generateClientID()
	userID := r.Header.Get("User-ID")
	if userID == "" {
		userID = clientID
	}

	client := &Client{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, h.config.ClientBuffer),
		id:       clientID,
		userID:   userID,
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
	}

	// Safely register - recover if hub is shutting down
	func() {
		defer func() {
			if recover() != nil {
				conn.Close()
			}
		}()
		client.hub.register <- client
		go client.writePump()
		go client.readPump()
	}()
}

func (c *Client) readPump() {
	defer c.conn.Close()
	defer func() {
		// Safely unregister - recover if hub is shutting down
		defer func() { recover() }()
		c.hub.unregister <- c
	}()

	c.conn.SetReadLimit(c.hub.config.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.hub.config.PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(c.hub.config.PongWait))
		return nil
	})

	for {
		_, messageBytes, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		msg.UserID = c.userID
		msg.Timestamp = time.Now()

		if c.hub.config.OnMessage != nil {
			c.hub.config.OnMessage(c, &msg)
		}

		c.handleMessage(&msg)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(c.hub.config.PingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(c.hub.config.WriteWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.hub.config.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case "join_room":
		if roomName, ok := msg.Data.(string); ok {
			c.hub.JoinRoom(c, roomName)
		}
	case "leave_room":
		if roomName, ok := msg.Data.(string); ok {
			c.hub.LeaveRoom(c, roomName)
		}
	case "room_message":
		if msg.Room != "" {
			messageBytes, _ := json.Marshal(msg)
			c.hub.BroadcastToRoom(msg.Room, messageBytes, c)
		}
	case "broadcast":
		messageBytes, _ := json.Marshal(msg)
		c.hub.BroadcastToAll(messageBytes)
	}
}

func (c *Client) Send(message []byte) {
	select {
	case c.send <- message:
	default:
		close(c.send)
		delete(c.hub.clients, c)
	}
}

func (c *Client) SendMessage(msgType string, data interface{}) error {
	msg := Message{
		Type:      msgType,
		Data:      data,
		UserID:    c.userID,
		Timestamp: time.Now(),
	}

	messageBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.Send(messageBytes)
	return nil
}

func (c *Client) GetID() string {
	return c.id
}

func (c *Client) GetUserID() string {
	return c.userID
}

func (c *Client) SetMetadata(key string, value interface{}) {
	c.mu.Lock()
	c.metadata[key] = value
	c.mu.Unlock()
}

func (c *Client) GetMetadata(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metadata[key]
}

func (c *Client) GetRooms() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rooms := make([]string, 0, len(c.rooms))
	for room := range c.rooms {
		rooms = append(rooms, room)
	}
	return rooms
}