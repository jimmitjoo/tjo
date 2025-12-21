package websocket

import (
	"net/http"
	"time"
)

type Config struct {
	WriteWait         time.Duration
	PongWait          time.Duration
	PingPeriod        time.Duration
	MaxMessageSize    int64
	BroadcastBuffer   int
	RoomMessageBuffer int
	ClientBuffer      int
	AllowedOrigins    []string // List of allowed origins for WebSocket connections

	// AuthenticateConnection validates the WebSocket connection and returns a verified user ID.
	// If nil, connections are treated as anonymous with auto-generated IDs.
	// IMPORTANT: Never trust client-provided headers for user identity - always validate
	// using session cookies, JWT tokens, or other secure authentication mechanisms.
	AuthenticateConnection func(r *http.Request) (userID string, err error)

	OnConnect    func(*Client)
	OnDisconnect func(*Client)
	OnMessage    func(*Client, *Message)
	OnJoinRoom   func(*Client, string)
	OnLeaveRoom  func(*Client, string)
}

func DefaultConfig() *Config {
	return &Config{
		WriteWait:         10 * time.Second,
		PongWait:          60 * time.Second,
		PingPeriod:        (60 * time.Second * 9) / 10,
		MaxMessageSize:    512,
		BroadcastBuffer:   256,
		RoomMessageBuffer: 256,
		ClientBuffer:      256,
	}
}

type Option func(*Config)

func WithWriteTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.WriteWait = timeout
	}
}

func WithPongTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.PongWait = timeout
		c.PingPeriod = (timeout * 9) / 10
	}
}

func WithMaxMessageSize(size int64) Option {
	return func(c *Config) {
		c.MaxMessageSize = size
	}
}

func WithBufferSizes(broadcast, roomMessage, client int) Option {
	return func(c *Config) {
		c.BroadcastBuffer = broadcast
		c.RoomMessageBuffer = roomMessage
		c.ClientBuffer = client
	}
}

func WithOnConnect(handler func(*Client)) Option {
	return func(c *Config) {
		c.OnConnect = handler
	}
}

func WithOnDisconnect(handler func(*Client)) Option {
	return func(c *Config) {
		c.OnDisconnect = handler
	}
}

func WithOnMessage(handler func(*Client, *Message)) Option {
	return func(c *Config) {
		c.OnMessage = handler
	}
}

func WithOnJoinRoom(handler func(*Client, string)) Option {
	return func(c *Config) {
		c.OnJoinRoom = handler
	}
}

func WithOnLeaveRoom(handler func(*Client, string)) Option {
	return func(c *Config) {
		c.OnLeaveRoom = handler
	}
}

// WithAllowedOrigins sets the allowed origins for WebSocket connections.
// If empty, all origins are rejected (secure default).
// Use []string{"*"} to allow all origins (not recommended for production).
func WithAllowedOrigins(origins []string) Option {
	return func(c *Config) {
		c.AllowedOrigins = origins
	}
}

// WithAuthenticateConnection sets the authentication callback for WebSocket connections.
// The callback should validate the request (e.g., check JWT token, session cookie) and
// return a verified user ID. If authentication fails, return an error.
//
// Example with JWT:
//
//	WithAuthenticateConnection(func(r *http.Request) (string, error) {
//	    token := r.Header.Get("Authorization")
//	    claims, err := auth.ValidateJWT(strings.TrimPrefix(token, "Bearer "))
//	    if err != nil {
//	        return "", err
//	    }
//	    return claims.UserID, nil
//	})
//
// Example with session:
//
//	WithAuthenticateConnection(func(r *http.Request) (string, error) {
//	    session := sessionManager.Get(r.Context())
//	    userID := session.GetString("user_id")
//	    if userID == "" {
//	        return "", errors.New("not authenticated")
//	    }
//	    return userID, nil
//	})
func WithAuthenticateConnection(fn func(r *http.Request) (string, error)) Option {
	return func(c *Config) {
		c.AuthenticateConnection = fn
	}
}

func NewConfig(options ...Option) *Config {
	config := DefaultConfig()
	for _, option := range options {
		option(config)
	}
	return config
}