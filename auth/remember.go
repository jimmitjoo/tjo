package auth

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// "Remember me", which is a credential and was not being treated as one.
//
// The scaffolded implementation this replaces stored the cookie's value in the
// database verbatim, with no expiry, in a cookie that lived for a year. A read
// of that table -- a backup, an injection, a logged row -- was a working
// long-lived login for every user who had ticked the box. That is the same
// defect as the plaintext API tokens v0.7.0 fixed, arrived at independently in
// a second place, which is the argument for this living in one tested package
// rather than in every generated project.
//
// What changes:
//
//   - Only the hash is stored, so the table is worth nothing to whoever reads
//     it.
//   - Tokens expire.
//   - A token is single-use and rotates on every use, so a stolen cookie stops
//     working the next time the real user's browser presents theirs.
//   - The cookie carries the token and nothing else. The old format was
//     "<user id>|<token>", which meant parsing attacker-controlled input before
//     authenticating it; the hash is the lookup key, so there is nothing to
//     parse.
//
// Storage is the ResetStore the rest of this package already uses. A remember
// token is a single-use, expiring, user-bound secret consumed atomically --
// which is precisely what that interface exists to get right, and the atomicity
// is what keeps two concurrent requests from both rotating the same token.

// PurposeRemember marks a token that stands in for a completed login.
//
// Separated from the reset and activation purposes for the reason all of them
// are: without it, a token minted to keep someone signed in would be redeemable
// against the password-reset endpoint.
const PurposeRemember ResetPurpose = "remember"

// DefaultRememberTTL is thirty days. Long enough to be the convenience it is
// sold as, short enough that an abandoned laptop stops being a login within a
// month. The year the scaffolded cookie used was not a considered number.
const DefaultRememberTTL = 30 * 24 * time.Hour

// Remember issues a token that will log userID back in.
//
// Call it when a login completes and the user asked to be remembered. The
// returned plaintext goes in the cookie; the store keeps only its hash.
func Remember(ctx context.Context, store ResetStore, userID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = DefaultRememberTTL
	}

	token, err := NewResetToken(userID, PurposeRemember, ttl)
	if err != nil {
		return "", err
	}
	if err := store.Save(ctx, token); err != nil {
		return "", err
	}
	return token.PlainText, nil
}

// Recall spends a remember token and issues its replacement.
//
// It returns the account the token belonged to and a new plaintext for the
// cookie. Rotation is the point: a token presented twice is a token that was
// copied, and after this call the copy is worthless whichever of the two
// browsers used it first.
//
// The caller must renew the session before treating the user as logged in.
// Promoting an anonymous session to an authenticated one without renewing it is
// session fixation, and it is the specific mistake the scaffolded middleware
// made -- it set userID on whatever session the request arrived with.
func Recall(ctx context.Context, store ResetStore, plain string, ttl time.Duration) (userID, replacement string, err error) {
	userID, err = Redeem(ctx, store, plain, PurposeRemember)
	if err != nil {
		return "", "", err
	}

	replacement, err = Remember(ctx, store, userID, ttl)
	if err != nil {
		return "", "", err
	}
	return userID, replacement, nil
}

// ForgetOne invalidates a single remember token, the one in this browser's
// cookie.
//
// This is what logging out should call. Forget would sign the user out of every
// device they own, which is not what "log out" means on the machine they are
// sitting at -- and doing it silently is worse than not doing it.
func ForgetOne(ctx context.Context, store ResetStore, plain string) error {
	_, err := Redeem(ctx, store, plain, PurposeRemember)
	if errors.Is(err, ErrInvalidReset) {
		// Already gone, or never valid. Logging out is not the moment to
		// report that.
		return nil
	}
	return err
}

// Forget invalidates every remember token of an account.
//
// Log-out calls this. So should a password change: the whole point of changing
// a password is to end sessions you no longer control, and a remember cookie
// that survives it makes the change cosmetic.
func Forget(ctx context.Context, store ResetStore, userID string) error {
	return store.InvalidateUser(ctx, userID, PurposeRemember)
}

// RememberCookie builds the cookie for a token.
//
// name should be application-specific, and secure should be true anywhere but
// local development: a cookie that logs someone in and travels over plaintext
// HTTP is a credential handed to the network.
func RememberCookie(name, token string, ttl time.Duration, secure bool) *http.Cookie {
	if ttl <= 0 {
		ttl = DefaultRememberTTL
	}

	return &http.Cookie{
		Name:  name,
		Value: token,
		Path:  "/",
		// Both, because MaxAge is what browsers actually honour and Expires is
		// what the ones that do not read MaxAge fall back to.
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().Add(ttl),
		HttpOnly: true,
		Secure:   secure,
		// Lax rather than Strict: Strict means arriving from a link in an email
		// does not carry the cookie, so the user is signed out exactly when
		// they expected the "remember me" they ticked to work. Lax withholds it
		// from cross-site POSTs, which is the case that matters.
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearRememberCookie returns the cookie that removes one.
func ClearRememberCookie(name string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ErrNoRememberCookie is returned when the request carries no token.
var ErrNoRememberCookie = errors.New("auth: no remember cookie")

// RememberedToken reads the token out of a request.
func RememberedToken(r *http.Request, name string) (string, error) {
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return "", ErrNoRememberCookie
	}
	return cookie.Value, nil
}
