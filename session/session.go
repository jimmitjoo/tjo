package session

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/postgresstore"
	"github.com/alexedwards/scs/redisstore"
	"github.com/alexedwards/scs/v2"
	"github.com/gomodule/redigo/redis"
)

type Session struct {
	CookieLifetime string
	CookiePersist  string
	CookieName     string
	CookieDomain   string
	SessionType    string
	CookieSecure   string
	DBPool         *sql.DB
	RedisPool      *redis.Pool
}

// SecureSessionConfig holds secure session configuration
type SecureSessionConfig struct {
	EnableRotation    bool
	RotateOnAuth      bool
	MaxLifetime       time.Duration
	IdleTimeout       time.Duration
	RegenerationTime  time.Duration
	HttpOnlyDefault   bool
	SecureDefault     bool
	SameSiteDefault   http.SameSite
}

func (g *Session) InitSession() *scs.SessionManager {
	return g.InitSecureSession(DefaultSecureSessionConfig())
}

// InitSecureSession creates a session manager with enhanced security
func (g *Session) InitSecureSession(config SecureSessionConfig) *scs.SessionManager {
	var persist, secure bool

	// how long should sessions last?
	minutes, err := strconv.Atoi(g.CookieLifetime)
	if err != nil {
		minutes = 30 // Safer default: 30 minutes
	}

	// should cookies persist?
	if strings.ToLower(g.CookiePersist) == "true" {
		persist = true
	}

	// must cookies be secure? Default to true for enhanced security
	if strings.ToLower(g.CookieSecure) == "true" || config.SecureDefault {
		secure = true
	}

	// create session with secure defaults
	session := scs.New()
	session.Lifetime = time.Duration(minutes) * time.Minute
	
	// Apply secure configuration
	if config.MaxLifetime > 0 {
		session.Lifetime = config.MaxLifetime
	}
	if config.IdleTimeout > 0 {
		session.IdleTimeout = config.IdleTimeout
	}

	session.Cookie.Persist = persist
	session.Cookie.Name = g.CookieName
	session.Cookie.Secure = secure
	session.Cookie.HttpOnly = config.HttpOnlyDefault // Enable HttpOnly by default
	session.Cookie.Domain = g.CookieDomain
	session.Cookie.SameSite = config.SameSiteDefault

	// which session store?
	switch strings.ToLower(g.SessionType) {
	case "redis":
		session.Store = redisstore.New(g.RedisPool)
	case "mysql", "mariadb":
		session.Store = mysqlstore.New(g.DBPool)
	case "postgres", "postgresql":
		session.Store = postgresstore.New(g.DBPool)
	default:
		// cookie store as fallback
	}

	return session
}

// DefaultSecureSessionConfig returns secure default configuration
func DefaultSecureSessionConfig() SecureSessionConfig {
	return SecureSessionConfig{
		EnableRotation:   true,
		RotateOnAuth:     true,
		MaxLifetime:      30 * time.Minute,
		IdleTimeout:      15 * time.Minute,
		RegenerationTime: 5 * time.Minute,
		HttpOnlyDefault:  true,
		SecureDefault:    true,
		SameSiteDefault:  http.SameSiteStrictMode, // Stricter default
	}
}

// RegenerateSession regenerates session ID to prevent fixation attacks
func RegenerateSession(sessionManager *scs.SessionManager, w http.ResponseWriter, r *http.Request) error {
	// Renew the session token to prevent session fixation
	return sessionManager.RenewToken(r.Context())
}

// SecureSessionRotationMiddleware provides automatic session rotation
func SecureSessionRotationMiddleware(sessionManager *scs.SessionManager, config SecureSessionConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if config.EnableRotation {
				// Check if session needs rotation based on age
				if shouldRotateSession(sessionManager, r, config) {
					if err := sessionManager.RenewToken(r.Context()); err != nil {
						// Log error but don't fail the request
						// In production, you might want to log this
					}
				}
			}
			
			next.ServeHTTP(w, r)
		})
	}
}

// shouldRotateSession determines if session should be rotated
func shouldRotateSession(sessionManager *scs.SessionManager, r *http.Request, config SecureSessionConfig) bool {
	// Check if session exists
	if !sessionManager.Exists(r.Context(), "created_at") {
		// Set creation time for new sessions
		sessionManager.Put(r.Context(), "created_at", time.Now().Unix())
		return false
	}

	// Get session creation time
	createdAt := sessionManager.GetInt64(r.Context(), "created_at")
	if createdAt == 0 {
		sessionManager.Put(r.Context(), "created_at", time.Now().Unix())
		return false
	}

	// Check if session is older than regeneration time
	sessionAge := time.Since(time.Unix(createdAt, 0))
	if sessionAge > config.RegenerationTime {
		sessionManager.Put(r.Context(), "created_at", time.Now().Unix())
		return true
	}

	return false
}

// AuthenticationSessionHandler handles secure session operations for authentication
func AuthenticationSessionHandler(sessionManager *scs.SessionManager, config SecureSessionConfig) *AuthSessionHandler {
	return &AuthSessionHandler{
		sessionManager: sessionManager,
		config:         config,
	}
}

// AuthSessionHandler handles authentication-related session operations
type AuthSessionHandler struct {
	sessionManager *scs.SessionManager
	config         SecureSessionConfig
}

// LoginUser securely establishes user session after authentication
func (ash *AuthSessionHandler) LoginUser(w http.ResponseWriter, r *http.Request, userID string) error {
	// Always regenerate session on login to prevent fixation
	if err := ash.sessionManager.RenewToken(r.Context()); err != nil {
		return err
	}

	// Set user session data
	ash.sessionManager.Put(r.Context(), "user_id", userID)
	ash.sessionManager.Put(r.Context(), "auth_time", time.Now().Unix())
	ash.sessionManager.Put(r.Context(), "created_at", time.Now().Unix())
	
	// Generate and store session fingerprint for additional security
	fingerprint, err := generateSessionFingerprint(r)
	if err == nil {
		ash.sessionManager.Put(r.Context(), "fingerprint", fingerprint)
	}

	return nil
}

// LogoutUser securely destroys user session
func (ash *AuthSessionHandler) LogoutUser(w http.ResponseWriter, r *http.Request) error {
	// Destroy the session completely
	return ash.sessionManager.Destroy(r.Context())
}

// ValidateSession validates session integrity and security
func (ash *AuthSessionHandler) ValidateSession(r *http.Request) bool {
	// Check if user is authenticated
	if !ash.sessionManager.Exists(r.Context(), "user_id") {
		return false
	}

	// Validate session fingerprint if enabled
	if ash.sessionManager.Exists(r.Context(), "fingerprint") {
		storedFingerprint := ash.sessionManager.GetString(r.Context(), "fingerprint")
		currentFingerprint, err := generateSessionFingerprint(r)
		if err != nil || storedFingerprint != currentFingerprint {
			// Session hijacking attempt detected
			ash.sessionManager.Destroy(r.Context())
			return false
		}
	}

	// Check session age limits
	if ash.config.MaxLifetime > 0 {
		authTime := ash.sessionManager.GetInt64(r.Context(), "auth_time")
		if authTime > 0 {
			sessionAge := time.Since(time.Unix(authTime, 0))
			if sessionAge > ash.config.MaxLifetime {
				ash.sessionManager.Destroy(r.Context())
				return false
			}
		}
	}

	return true
}

// generateSessionFingerprint creates a fingerprint for session validation
// based on stable client characteristics. This helps detect session hijacking.
func generateSessionFingerprint(r *http.Request) (string, error) {
	// Create fingerprint from stable client characteristics
	// Note: Be careful not to include characteristics that change frequently
	var fingerprintBuilder strings.Builder
	fingerprintBuilder.WriteString(r.UserAgent())
	fingerprintBuilder.WriteString("|")
	fingerprintBuilder.WriteString(r.Header.Get("Accept-Language"))

	// Hash the fingerprint for storage using SHA-256
	hash := sha256.Sum256([]byte(fingerprintBuilder.String()))

	// Return first 32 hex characters (16 bytes) for a compact fingerprint
	return hex.EncodeToString(hash[:16]), nil
}
