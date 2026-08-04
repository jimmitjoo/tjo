package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withSession runs fn inside scs's LoadAndSave so r.Context() carries a live
// session, and returns the response so cookies can be inspected.
func withSession(t *testing.T, sm *scs.SessionManager, req *http.Request, fn func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	sm.LoadAndSave(http.HandlerFunc(fn)).ServeHTTP(w, req)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c.Value
		}
	}
	return ""
}

// TestLoginRegeneratesTheSessionToken is the session fixation defence: an
// attacker who plants a known session ID must not still hold a valid one after
// the victim authenticates.
func TestLoginRegeneratesTheSessionToken(t *testing.T) {
	sm := scs.New()
	handler := AuthenticationSessionHandler(sm, DefaultSecureSessionConfig())

	// The attacker establishes a session and learns its token.
	req := httptest.NewRequest("GET", "/", nil)
	before := withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "planted", "yes")
	})
	plantedToken := sessionCookie(t, before)
	require.NotEmpty(t, plantedToken, "no session cookie was issued")

	// The victim arrives carrying that token and logs in.
	req = httptest.NewRequest("POST", "/login", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: plantedToken})
	after := withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, handler.LoginUser(w, r, "user-42"))
	})

	newToken := sessionCookie(t, after)
	require.NotEmpty(t, newToken)
	assert.NotEqual(t, plantedToken, newToken,
		"the session token survived login; a planted session ID would still be valid")
}

func TestLogoutDestroysTheSession(t *testing.T) {
	sm := scs.New()
	handler := AuthenticationSessionHandler(sm, DefaultSecureSessionConfig())

	req := httptest.NewRequest("POST", "/login", nil)
	w := withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, handler.LoginUser(w, r, "user-42"))
	})
	token := sessionCookie(t, w)

	req = httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, handler.LogoutUser(w, r))
		assert.False(t, sm.Exists(r.Context(), "user_id"), "user_id survived logout")
	})
}

// TestValidateSessionRejectsChangedFingerprint covers the hijacking check: a
// stolen cookie replayed from a different client should not validate.
func TestValidateSessionRejectsChangedFingerprint(t *testing.T) {
	sm := scs.New()
	handler := AuthenticationSessionHandler(sm, DefaultSecureSessionConfig())

	login := httptest.NewRequest("POST", "/login", nil)
	login.Header.Set("User-Agent", "Mozilla/5.0 (victim)")
	login.Header.Set("Accept-Language", "sv-SE")
	w := withSession(t, sm, login, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, handler.LoginUser(w, r, "user-42"))
	})
	token := sessionCookie(t, w)

	t.Run("same client validates", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (victim)")
		req.Header.Set("Accept-Language", "sv-SE")
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
			assert.True(t, handler.ValidateSession(r))
		})
	})

	t.Run("stolen cookie from another client is rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("User-Agent", "curl/8.0 (attacker)")
		req.Header.Set("Accept-Language", "sv-SE")
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
			assert.False(t, handler.ValidateSession(r),
				"a different client replayed the cookie and was accepted")
		})
	})
}

func TestValidateSessionRejectsUnauthenticated(t *testing.T) {
	sm := scs.New()
	handler := AuthenticationSessionHandler(sm, DefaultSecureSessionConfig())

	req := httptest.NewRequest("GET", "/", nil)
	withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
		assert.False(t, handler.ValidateSession(r), "a session with no user_id must not validate")
	})
}

// TestValidateSessionEnforcesMaxLifetime pins the age check.
func TestValidateSessionEnforcesMaxLifetime(t *testing.T) {
	sm := scs.New()
	config := DefaultSecureSessionConfig()
	config.MaxLifetime = time.Hour
	handler := AuthenticationSessionHandler(sm, config)

	req := httptest.NewRequest("GET", "/", nil)
	withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", "user-42")
		sm.Put(r.Context(), "auth_time", time.Now().Add(-2*time.Hour).Unix())

		assert.False(t, handler.ValidateSession(r), "a session past MaxLifetime must not validate")
	})
}

// TestValidateSessionWithoutAuthTime probes the fail-open path: an application
// that sets user_id itself, rather than going through LoginUser, leaves
// auth_time unset -- and the age check is skipped entirely when it is.
func TestValidateSessionWithoutAuthTime(t *testing.T) {
	sm := scs.New()
	config := DefaultSecureSessionConfig()
	config.MaxLifetime = time.Nanosecond
	handler := AuthenticationSessionHandler(sm, config)

	req := httptest.NewRequest("GET", "/", nil)
	withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", "user-42") // no auth_time
		time.Sleep(time.Millisecond)

		assert.False(t, handler.ValidateSession(r),
			"a session with no auth_time is never aged out, so MaxLifetime does nothing")
	})
}

func TestGenerateSessionFingerprint(t *testing.T) {
	newReq := func(ua, lang string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("User-Agent", ua)
		r.Header.Set("Accept-Language", lang)
		return r
	}

	base, err := generateSessionFingerprint(newReq("UA/1", "sv-SE"))
	require.NoError(t, err)
	assert.Len(t, base, 32, "fingerprint should be 32 hex characters")

	same, _ := generateSessionFingerprint(newReq("UA/1", "sv-SE"))
	assert.Equal(t, base, same, "the same client must produce the same fingerprint")

	otherUA, _ := generateSessionFingerprint(newReq("UA/2", "sv-SE"))
	assert.NotEqual(t, base, otherUA, "a different User-Agent must change the fingerprint")

	otherLang, _ := generateSessionFingerprint(newReq("UA/1", "en-GB"))
	assert.NotEqual(t, base, otherLang, "a different Accept-Language must change the fingerprint")
}

// TestShouldRotateSession covers the age-based rotation decision.
func TestShouldRotateSession(t *testing.T) {
	sm := scs.New()
	config := DefaultSecureSessionConfig()
	config.RegenerationTime = time.Hour

	t.Run("a brand new session is not rotated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
			assert.False(t, shouldRotateSession(sm, r, config))
			assert.True(t, sm.Exists(r.Context(), "created_at"), "creation time should be recorded")
		})
	})

	t.Run("a young session is not rotated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
			sm.Put(r.Context(), "created_at", time.Now().Add(-time.Minute).Unix())
			assert.False(t, shouldRotateSession(sm, r, config))
		})
	})

	t.Run("an old session is rotated and its clock reset", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		withSession(t, sm, req, func(w http.ResponseWriter, r *http.Request) {
			sm.Put(r.Context(), "created_at", time.Now().Add(-2*time.Hour).Unix())

			assert.True(t, shouldRotateSession(sm, r, config))
			assert.False(t, shouldRotateSession(sm, r, config),
				"created_at was not reset, so the session would rotate on every request")
		})
	})
}
