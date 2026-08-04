package websocket

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type ChatMessage struct {
	Username string    `json:"username"`
	Message  string    `json:"message"`
	Room     string    `json:"room"`
	Time     time.Time `json:"time"`
}

func ExampleChatServer() {
	config := NewConfig(
		WithMaxMessageSize(1024),
		WithBufferSizes(512, 512, 256),
		WithOnConnect(func(client *Client) {
			log.Printf("User %s connected", client.GetUserID())
		}),
		WithOnDisconnect(func(client *Client) {
			log.Printf("User %s disconnected", client.GetUserID())
		}),
		WithOnMessage(func(client *Client, msg *Message) {
			log.Printf("Message from %s: %s", client.GetUserID(), msg.Type)

			if msg.Type == "chat_message" {
				chatMsg := ChatMessage{
					Username: client.GetUserID(),
					Message:  msg.Data.(string),
					Room:     msg.Room,
					Time:     time.Now(),
				}

				response := Message{
					Type:      "chat_message",
					Data:      chatMsg,
					Room:      msg.Room,
					Timestamp: time.Now(),
				}

				responseBytes, _ := json.Marshal(response)

				if msg.Room != "" {
					client.hub.BroadcastToRoom(msg.Room, responseBytes, nil)
				} else {
					client.hub.BroadcastToAll(responseBytes)
				}
			}
		}),
		WithOnJoinRoom(func(client *Client, room string) {
			log.Printf("User %s joined room %s", client.GetUserID(), room)

			notification := Message{
				Type: "user_joined",
				Data: map[string]string{
					"username": client.GetUserID(),
					"room":     room,
				},
				Room:      room,
				Timestamp: time.Now(),
			}

			notificationBytes, _ := json.Marshal(notification)
			client.hub.BroadcastToRoom(room, notificationBytes, client)
		}),
		WithOnLeaveRoom(func(client *Client, room string) {
			log.Printf("User %s left room %s", client.GetUserID(), room)

			notification := Message{
				Type: "user_left",
				Data: map[string]string{
					"username": client.GetUserID(),
					"room":     room,
				},
				Room:      room,
				Timestamp: time.Now(),
			}

			notificationBytes, _ := json.Marshal(notification)
			client.hub.BroadcastToRoom(room, notificationBytes, client)
		}),
	)

	hub := NewHub(config)
	SetDefaultHub(hub)

	ctx := context.Background()
	go hub.Run(ctx)

	r := chi.NewRouter()

	RegisterRoutes(r, "/ws")

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		html := `
<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Chat Example</title>
</head>
<body>
    <div id="messages"></div>
    <input type="text" id="messageInput" placeholder="Type a message...">
    <button onclick="sendMessage()">Send</button>
    <button onclick="joinRoom()">Join Room</button>
    
    <script>
        const ws = new WebSocket('ws://localhost:8080/ws');
        const messages = document.getElementById('messages');
        
        ws.onmessage = function(event) {
            const msg = JSON.parse(event.data);
            const div = document.createElement('div');
            div.innerHTML = msg.type + ': ' + JSON.stringify(msg.data);
            messages.appendChild(div);
        };
        
        function sendMessage() {
            const input = document.getElementById('messageInput');
            const message = {
                type: 'chat_message',
                data: input.value,
                room: 'general'
            };
            ws.send(JSON.stringify(message));
            input.value = '';
        }
        
        function joinRoom() {
            const message = {
                type: 'join_room',
                data: 'general'
            };
            ws.send(JSON.stringify(message));
        }
    </script>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})

	r.Get("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := map[string]interface{}{
			"connected_clients": GetConnectedClients(),
			"rooms":             GetRooms(),
			"room_counts": func() map[string]int {
				counts := make(map[string]int)
				for _, room := range GetRooms() {
					counts[room] = GetRoomClients(room)
				}
				return counts
			}(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	log.Println("Chat server starting on :8080")
	http.ListenAndServe(":8080", r)
}

func ExampleNotificationServer() {
	config := NewConfig(
		WithOnConnect(func(client *Client) {
			userID := client.GetUserID()
			client.hub.JoinRoom(client, "notifications_"+userID)
		}),
	)

	hub := NewHub(config)
	SetDefaultHub(hub)

	ctx := context.Background()
	go hub.Run(ctx)

	r := chi.NewRouter()
	RegisterRoutes(r, "/notifications/ws")

	r.Post("/send-notification/{userID}", func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userID")

		var notification struct {
			Title   string      `json:"title"`
			Message string      `json:"message"`
			Data    interface{} `json:"data"`
		}

		if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		msg := Message{
			Type:      "notification",
			Data:      notification,
			Timestamp: time.Now(),
		}

		msgBytes, _ := json.Marshal(msg)
		BroadcastToRoom("notifications_"+userID, msgBytes, nil)

		w.WriteHeader(http.StatusOK)
	})

	log.Println("Notification server starting on :8080")
	http.ListenAndServe(":8080", r)
}
