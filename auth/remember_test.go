package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The cookie's value must not be what the database holds. The implementation
// this replaces stored it verbatim, so reading the table was a working login
// for every user who had ticked the box.
func TestTheStoredFormIsNotTheCookieValue(t *testing.T) {
	ctx := context.Background()
	store := sqliteStore(t)

	token, err := Remember(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var stored []byte
	err = store.db.QueryRow(`SELECT hash FROM tjo_reset_tokens WHERE user_id = ? AND purpose = ?`,
		"user-1", string(PurposeRemember)).Scan(&stored)
	if err != nil {
		t.Fatal(err)
	}

	if string(stored) == token {
		t.Fatal("the database holds the cookie's value verbatim")
	}
	if strings.Contains(string(stored), token) {
		t.Fatal("the plaintext token is recoverable from the stored form")
	}
}

// A remember token is spent when it is used and replaced with a new one, so a
// copied cookie stops working as soon as either browser presents it.
func TestRecallRotatesTheToken(t *testing.T) {
	ctx := context.Background()
	store := sqliteStore(t)

	first, err := Remember(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	user, second, err := Recall(ctx, store, first, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if user != "user-1" {
		t.Fatalf("recalled %q, want user-1", user)
	}
	if second == first {
		t.Fatal("the token was not rotated")
	}

	// The spent one is worthless, including to whoever copied it.
	if _, _, err := Recall(ctx, store, first, time.Hour); !errors.Is(err, ErrInvalidReset) {
		t.Fatalf("a spent remember token was accepted again: %v", err)
	}

	// The replacement works, once.
	if _, _, err := Recall(ctx, store, second, time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestForgetInvalidatesEveryTokenOfAnAccount(t *testing.T) {
	ctx := context.Background()
	store := sqliteStore(t)

	laptop, err := Remember(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	phone, err := Remember(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Remember(ctx, store, "user-2", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if err := Forget(ctx, store, "user-1"); err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{"laptop": laptop, "phone": phone} {
		if _, _, err := Recall(ctx, store, token, time.Hour); !errors.Is(err, ErrInvalidReset) {
			t.Errorf("%s still logs in after Forget", name)
		}
	}

	if _, _, err := Recall(ctx, store, other, time.Hour); err != nil {
		t.Errorf("Forget invalidated another account's token: %v", err)
	}
}

func TestAnExpiredRememberTokenDoesNotLogAnyoneIn(t *testing.T) {
	ctx := context.Background()
	store := sqliteStore(t)

	// Minted directly, because Remember treats a non-positive ttl as "use the
	// default" rather than as "expire immediately".
	expired, err := NewResetToken("user-1", PurposeRemember, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, expired); err != nil {
		t.Fatal(err)
	}
	token := expired.PlainText

	if _, _, err := Recall(ctx, store, token, time.Hour); !errors.Is(err, ErrInvalidReset) {
		t.Fatalf("an expired token was accepted: %v", err)
	}
}

// A remember token must not be redeemable anywhere else. Sharing a table with
// the password-reset flow is only safe because the purposes are separate.
func TestARememberTokenIsNotAPasswordResetToken(t *testing.T) {
	ctx := context.Background()
	store := sqliteStore(t)

	token, err := Remember(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Redeem(ctx, store, token, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
		t.Fatalf("a remember token was redeemed as a password reset: %v", err)
	}
}

func TestRememberCookieIsNotReadableByScripts(t *testing.T) {
	cookie := RememberCookie("_app_remember", "token", time.Hour, true)

	if !cookie.HttpOnly {
		t.Error("the cookie is readable by JavaScript, so an XSS is a permanent login")
	}
	if !cookie.Secure {
		t.Error("the cookie travels over plaintext HTTP")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("MaxAge = %d, want the ttl", cookie.MaxAge)
	}

	cleared := ClearRememberCookie("_app_remember", true)
	if cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Error("the clearing cookie does not clear anything")
	}
}

func TestRememberedTokenReadsTheCookie(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if _, err := RememberedToken(r, "_app_remember"); !errors.Is(err, ErrNoRememberCookie) {
		t.Fatalf("err = %v, want ErrNoRememberCookie", err)
	}

	r.AddCookie(&http.Cookie{Name: "_app_remember", Value: "abc"})
	token, err := RememberedToken(r, "_app_remember")
	if err != nil || token != "abc" {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}
