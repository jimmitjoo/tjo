package websocket

import (
	"context"
	"encoding/json"
	"fmt"
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

	// done is closed once when the hub shuts down. It replaces closing the
	// channels above: their producers are application code, and a closed
	// channel makes every subsequent send a panic rather than a no-op.
	done      chan struct{}
	closeOnce sync.Once
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

	// closed is closed once when the client is unregistered. Nothing ever
	// closes send: Send is public, so a closed send channel would let any
	// caller panic the process on a disconnected client.
	closed    chan struct{}
	closeOnce sync.Once
}

// newClient builds a Client with its internal channels initialised. Always use
// this rather than a struct literal: a Client whose closed channel is nil
// cannot signal writePump to stop, and unregistering it panics.
func newClient(hub *Hub, conn *websocket.Conn, id, userID string, sendBuffer int) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, sendBuffer),
		id:       id,
		userID:   userID,
		rooms:    make(map[string]bool),
		metadata: make(map[string]interface{}),
		closed:   make(chan struct{}),
	}
}

// close signals writePump to finish. Safe to call more than once.
func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
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
		done:         make(chan struct{}),
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
	total := len(h.clients)
	h.mu.Unlock()

	if h.config.OnConnect != nil {
		h.config.OnConnect(client)
	}

	log.Printf("Client %s connected. Total clients: %d", client.id, total)
}

func (h *Hub) unregisterClient(client *Client) {
	var leftRooms []string
	var total int

	h.mu.Lock()
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		client.close()

		// Snapshot first: leaveRoomLocked mutates client.rooms, and the read
		// needs client.mu anyway. Lock order is h.mu -> client.mu throughout.
		client.mu.RLock()
		roomNames := make([]string, 0, len(client.rooms))
		for roomName := range client.rooms {
			roomNames = append(roomNames, roomName)
		}
		client.mu.RUnlock()

		for _, roomName := range roomNames {
			if h.leaveRoomLocked(client, roomName) {
				leftRooms = append(leftRooms, roomName)
			}
		}
	}
	total = len(h.clients)
	h.mu.Unlock()

	// Callbacks run outside the lock so a handler cannot deadlock the hub.
	if h.config.OnLeaveRoom != nil {
		for _, roomName := range leftRooms {
			h.config.OnLeaveRoom(client, roomName)
		}
	}

	if h.config.OnDisconnect != nil {
		h.config.OnDisconnect(client)
	}

	log.Printf("Client %s disconnected. Total clients: %d", client.id, total)
}

func (h *Hub) broadcastToAll(message []byte) {
	h.mu.RLock()
	var deadClients []*Client
	for client := range h.clients {
		select {
		case client.send <- message:
		default:
			// Collect dead clients - don't modify map while holding read lock
			deadClients = append(deadClients, client)
		}
	}
	h.mu.RUnlock()

	// Clean up dead clients with write lock
	if len(deadClients) > 0 {
		h.mu.Lock()
		for _, client := range deadClients {
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.close()
			}
		}
		h.mu.Unlock()
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
	var deadClients []*Client
	for client := range room.clients {
		if client == roomMsg.Exclude {
			continue
		}
		select {
		case client.send <- roomMsg.Message:
		default:
			// Collect dead clients - don't modify map while holding read lock
			deadClients = append(deadClients, client)
		}
	}
	room.mu.RUnlock()

	// Clean up dead clients with write lock
	if len(deadClients) > 0 {
		room.mu.Lock()
		for _, client := range deadClients {
			delete(room.clients, client)
		}
		room.mu.Unlock()
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
	// Use write lock from start to prevent TOCTOU race
	h.mu.Lock()
	left := h.leaveRoomLocked(client, roomName)
	h.mu.Unlock()

	if !left {
		return
	}

	if h.config.OnLeaveRoom != nil {
		h.config.OnLeaveRoom(client, roomName)
	}

	log.Printf("Client %s left room %s", client.id, roomName)
}

// leaveRoomLocked removes client from roomName and reports whether the room
// existed. The caller must already hold h.mu -- sync.Mutex is not reentrant,
// so callers that hold it must never route through the exported leaveRoom.
func (h *Hub) leaveRoomLocked(client *Client, roomName string) bool {
	room, exists := h.rooms[roomName]
	if !exists {
		return false
	}

	room.mu.Lock()
	delete(room.clients, client)
	isEmpty := len(room.clients) == 0
	room.mu.Unlock()

	client.mu.Lock()
	delete(client.rooms, roomName)
	client.mu.Unlock()

	if isEmpty {
		delete(h.rooms, roomName)
	}

	return true
}

func (h *Hub) BroadcastToAll(message []byte) {
	select {
	case <-h.done:
		return
	default:
	}

	select {
	case h.broadcast <- message:
	case <-h.done:
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
	case <-h.done:
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

// shutdown signals every producer to stop and drops the connections.
//
// It deliberately closes none of the hub's channels. Application code sends on
// broadcast/register/unregister/roomMessages and on client.send, so closing
// them turns any in-flight send into a panic -- during graceful shutdown, of
// all moments. Closing done instead lets each sender bail out and return an
// error, and lets writePump exit on its own.
func (h *Hub) shutdown() {
	h.closeOnce.Do(func() {
		close(h.done)
	})

	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		if client.conn != nil {
			client.conn.Close()
		}
	}

	h.clients = make(map[*Client]bool)
	h.rooms = make(map[string]*Room)
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientID := generateClientID()
	var userID string

	// Use authentication callback if configured
	if h.config.AuthenticateConnection != nil {
		userID, err = h.config.AuthenticateConnection(r)
		if err != nil {
			log.Printf("WebSocket authentication failed for client %s: %v", clientID, err)
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(4001, "authentication failed"))
			conn.Close()
			return
		}
	}

	// If no auth callback or user not authenticated, use client ID
	if userID == "" {
		userID = clientID
	}

	client := newClient(h, conn, clientID, userID, h.config.ClientBuffer)

	select {
	case client.hub.register <- client:
		go client.writePump()
		go client.readPump()
	case <-h.done:
		conn.Close()
	}
}

func (c *Client) readPump() {
	defer c.conn.Close()
	defer func() {
		select {
		case c.hub.unregister <- c:
		case <-c.hub.done:
		}
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
		case <-c.closed:
			c.conn.SetWriteDeadline(time.Now().Add(c.hub.config.WriteWait))
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		case <-c.hub.done:
			c.conn.SetWriteDeadline(time.Now().Add(c.hub.config.WriteWait))
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
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
		} else {
			log.Printf("Client %s: join_room requires string room name", c.id)
		}
	case "leave_room":
		if roomName, ok := msg.Data.(string); ok {
			c.hub.LeaveRoom(c, roomName)
		} else {
			log.Printf("Client %s: leave_room requires string room name", c.id)
		}
	case "room_message":
		if msg.Room == "" {
			log.Printf("Client %s: room_message requires room field", c.id)
			return
		}
		messageBytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("Client %s: failed to marshal room message: %v", c.id, err)
			return
		}
		c.hub.BroadcastToRoom(msg.Room, messageBytes, c)
	case "broadcast":
		messageBytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("Client %s: failed to marshal broadcast message: %v", c.id, err)
			return
		}
		c.hub.BroadcastToAll(messageBytes)
	default:
		log.Printf("Client %s: unknown message type: %s", c.id, msg.Type)
	}
}

func (c *Client) Send(message []byte) error {
	select {
	case <-c.closed:
		return fmt.Errorf("client %s is closed", c.id)
	case <-c.hub.done:
		return fmt.Errorf("hub is shut down")
	default:
	}

	select {
	case c.send <- message:
		return nil
	default:
		// Channel full - let the writePump/unregister handle cleanup
		// Do NOT modify hub.clients here - causes race condition
		return fmt.Errorf("send buffer full for client %s", c.id)
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

	return c.Send(messageBytes)
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

// GetTypedMetadata is a generic helper for type-safe metadata retrieval.
// Example: userID, err := GetTypedMetadata[int](client, "user_id")
func GetTypedMetadata[T any](c *Client, key string) (T, error) {
	var zero T
	c.mu.RLock()
	value, exists := c.metadata[key]
	c.mu.RUnlock()

	if !exists {
		return zero, fmt.Errorf("key %s not found in metadata", key)
	}

	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("key %s is not the expected type", key)
	}

	return typed, nil
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
