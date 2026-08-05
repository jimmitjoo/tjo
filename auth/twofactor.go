package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Time-based one-time passwords, RFC 6238, and the recovery codes without which
// 2FA is a way to lose an account rather than protect one.
//
// # Why this is forty lines rather than a dependency
//
// TOTP is HMAC-SHA1 over a counter, truncated to six digits. The specification
// fits on a page and the implementation fits below it. Every authenticator app
// in existence implements the same thing, so there is no compatibility surface
// to get wrong beyond the algorithm itself, which is pinned by its test
// vectors -- and RFC 6238's own vectors are in the test file.
//
// The generated project keeps an OTP library for one thing: turning the
// provisioning URI into a QR image. Drawing a picture is not a security
// decision, so it can live in a template. Deciding whether a code is valid is,
// so it lives here.

// ErrInvalidCode is returned for a one-time code that does not verify.
//
// One error for wrong, expired and already-used. A form that distinguishes them
// tells an attacker whether they guessed a code that existed, and there is
// nothing a legitimate user does differently with the distinction.
var ErrInvalidCode = errors.New("auth: invalid or already used code")

const (
	// totpStep is the 30-second window every authenticator app assumes.
	totpStep = 30 * time.Second

	// totpDigits is six, for the same reason.
	totpDigits = 6

	// totpSecretBytes is 160 bits, the minimum RFC 4226 §4 requires.
	totpSecretBytes = 20

	// totpSkew is how many steps either side of now are accepted, for clock
	// drift between the server and the phone. One step is ±30 seconds.
	//
	// It is not configurable. Every additional step widens the window in which
	// a shoulder-surfed code is still usable, and "our clocks are wrong by more
	// than a minute" is a problem to fix rather than to accommodate.
	totpSkew = 1
)

// totpEncoding is base32 without padding, upper case: what authenticator apps
// accept when a secret is typed in by hand.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a fresh shared secret, base32-encoded for display.
func NewTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return totpEncoding.EncodeToString(buf), nil
}

// TOTPURI builds the otpauth:// URI an authenticator app scans.
//
// issuer appears twice on purpose -- in the label and as a parameter -- which
// is what Google's key-uri-format document specifies and what apps rely on to
// show "Example (alex@example.com)" rather than just an address.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)

	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(int(totpStep.Seconds())))

	return "otpauth://totp/" + label + "?" + q.Encode()
}

// TOTPCode returns the code for a secret at a point in time.
//
// Exported because a caller sometimes needs to produce one -- a test, a
// diagnostic, a support tool that confirms the server and the phone agree.
func TOTPCode(secret string, at time.Time) (string, error) {
	return totpAt(secret, at.UTC().Unix()/int64(totpStep.Seconds()))
}

// VerifyTOTP checks a code and returns the time step it matched.
//
// lastStep is the step this account last authenticated with, and passing it is
// not optional bookkeeping: RFC 6238 §5.2 requires that a code be accepted only
// once. Without it a code stays usable for its whole window -- up to ninety
// seconds with skew -- so one observed over a shoulder, read from a phishing
// page or left in a log can be replayed. Store the returned step against the
// account and pass it back next time; pass 0 the first time.
func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (int64, error) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, ErrInvalidCode
	}

	current := now.UTC().Unix() / int64(totpStep.Seconds())

	// Every candidate is computed and compared, and the loop does not break on
	// a match, so the time taken does not depend on which step matched.
	matched := int64(-1)
	for step := current - totpSkew; step <= current+totpSkew; step++ {
		candidate, err := totpAt(secret, step)
		if err != nil {
			return 0, err
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			matched = step
		}
	}

	if matched < 0 {
		return 0, ErrInvalidCode
	}

	// Replay: this code, or one from an earlier step, has already been used.
	if matched <= lastStep {
		return 0, ErrInvalidCode
	}

	return matched, nil
}

// totpAt is HOTP over a time step: HMAC-SHA1, dynamically truncated per
// RFC 4226 §5.3.
func totpAt(secret string, step int64) (string, error) {
	key, err := totpEncoding.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", fmt.Errorf("auth: malformed TOTP secret: %w", err)
	}
	if len(key) == 0 {
		return "", errors.New("auth: empty TOTP secret")
	}

	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(step))

	mac := hmac.New(sha1.New, key)
	mac.Write(counter)
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", totpDigits, truncated%pow10(totpDigits)), nil
}

func pow10(n int) uint32 {
	out := uint32(1)
	for range n {
		out *= 10
	}
	return out
}

// PurposeRecoveryCode marks a single-use code that substitutes for an
// authenticator app.
const PurposeRecoveryCode ResetPurpose = "recovery_code"

// RecoveryCodeCount is how many codes are issued at a time. Ten is what every
// service that does this issues, and it is enough that losing a phone is an
// inconvenience rather than a support ticket.
const RecoveryCodeCount = 10

// NewRecoveryCodes issues replacement codes for an account, invalidating any it
// already had.
//
// The plaintext is returned once, for display, and never stored -- these are
// single-use tokens and are kept exactly the way the password-reset tokens are,
// on the same store, with a purpose that keeps the two from being redeemed
// against each other's endpoints.
//
// ttl is long by nature: a recovery code is useful precisely when someone has
// not been able to log in for a while. Ten years is a reasonable default and
// still an expiry rather than a token that lives forever.
func NewRecoveryCodes(ctx context.Context, store ResetStore, userID string, ttl time.Duration) ([]string, error) {
	if err := store.InvalidateUser(ctx, userID, PurposeRecoveryCode); err != nil {
		return nil, err
	}

	codes := make([]string, 0, RecoveryCodeCount)
	for range RecoveryCodeCount {
		token, err := NewResetToken(userID, PurposeRecoveryCode, ttl)
		if err != nil {
			return nil, err
		}
		if err := store.Save(ctx, token); err != nil {
			return nil, err
		}
		codes = append(codes, token.PlainText)
	}
	return codes, nil
}

// UseRecoveryCode spends a code and reports which account it belonged to.
//
// The caller must check that the account matches the one trying to log in. This
// returns the owner rather than taking it, because the atomic consumption --
// which is what stops one code logging in twice -- happens by hash, and a hash
// does not know whose it is until the row is read.
func UseRecoveryCode(ctx context.Context, store ResetStore, code string) (string, error) {
	userID, err := Redeem(ctx, store, normaliseRecoveryCode(code), PurposeRecoveryCode)
	if errors.Is(err, ErrInvalidReset) {
		return "", ErrInvalidCode
	}
	return userID, err
}

// normaliseRecoveryCode trims what a copy-paste picks up, and nothing else.
//
// Not upper-cased and not de-hyphenated, however friendly that would look on
// the page: a reset token is base64url, where case is meaningful and '-' is a
// character rather than a separator. Prettifying the display and then undoing
// it here is how a code that was issued stops verifying.
func normaliseRecoveryCode(code string) string {
	return strings.TrimSpace(code)
}
