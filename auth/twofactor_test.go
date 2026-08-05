package auth

import (
	"context"
	"encoding/base32"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// RFC 6238's own test vectors, Appendix B, for the SHA-1 variant every
// authenticator app implements.
//
// The seed there is the ASCII "12345678901234567890"; the RFC prints eight
// digits and this implementation produces six, so the expectation is the last
// six of each published value.
func TestTOTPMatchesRFC6238(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString([]byte("12345678901234567890"))

	tests := []struct {
		unix int64
		want string
	}{
		{59, "287082"},          // RFC: 94287082
		{1111111109, "081804"},  // RFC: 07081804
		{1111111111, "050471"},  // RFC: 14050471
		{1234567890, "005924"},  // RFC: 89005924
		{2000000000, "279037"},  // RFC: 69279037
		{20000000000, "353130"}, // RFC: 65353130
	}

	for _, tt := range tests {
		got, err := TOTPCode(secret, time.Unix(tt.unix, 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("TOTPCode at %d = %s, want %s", tt.unix, got, tt.want)
		}
	}
}

// A code that has been used must not work again. RFC 6238 §5.2 requires it, and
// without it a code observed over a shoulder, phished, or left in a log stays
// valid for up to ninety seconds.
func TestACodeCannotBeUsedTwice(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}

	step, err := VerifyTOTP(secret, code, now, 0)
	if err != nil {
		t.Fatalf("first use: %v", err)
	}

	if _, err := VerifyTOTP(secret, code, now, step); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("second use of the same code: %v, want ErrInvalidCode", err)
	}

	// And so must a code from an earlier step, which is the same replay one
	// window further back.
	older, err := TOTPCode(secret, now.Add(-totpStep))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyTOTP(secret, older, now, step); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("a code from an earlier step was accepted: %v", err)
	}
}

// One step of drift either way is accepted; two is not.
func TestTOTPClockSkew(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	for _, offset := range []time.Duration{-totpStep, 0, totpStep} {
		code, err := TOTPCode(secret, now.Add(offset))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyTOTP(secret, code, now, 0); err != nil {
			t.Errorf("code at %v drift was rejected: %v", offset, err)
		}
	}

	for _, offset := range []time.Duration{-3 * totpStep, 3 * totpStep} {
		code, err := TOTPCode(secret, now.Add(offset))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyTOTP(secret, code, now, 0); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("code at %v drift was accepted", offset)
		}
	}
}

func TestVerifyTOTPRejectsNonsense(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	for name, code := range map[string]string{
		"empty":     "",
		"too short": "1234",
		"too long":  "1234567",
		"letters":   "abcdef",
	} {
		if _, err := VerifyTOTP(secret, code, now, 0); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("%s was not rejected", name)
		}
	}

	if _, err := VerifyTOTP("not base32!", "123456", now, 0); err == nil {
		t.Error("a malformed secret verified something")
	}
}

// The URI is what an authenticator app scans, and every app is fussy about it.
func TestTOTPURI(t *testing.T) {
	uri := TOTPURI("Example App", "alex@example.com", "JBSWY3DPEHPK3PXP")

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" {
		t.Fatalf("uri = %q, want an otpauth://totp/ URI", uri)
	}
	if !strings.Contains(parsed.Path, "Example App:alex@example.com") {
		t.Fatalf("label = %q, want issuer:account", parsed.Path)
	}

	q := parsed.Query()
	if q.Get("secret") != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("secret = %q", q.Get("secret"))
	}
	// The issuer parameter is what apps read; the label prefix is the fallback.
	// Apps show the wrong name when only one of them is present.
	if q.Get("issuer") != "Example App" {
		t.Fatalf("issuer = %q", q.Get("issuer"))
	}
	if q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Fatalf("parameters = %v, want the defaults every app assumes", q)
	}
}

func TestRecoveryCodesAreSingleUse(t *testing.T) {
	ctx := context.Background()
	store := sqliteStore(t)

	codes, err := NewRecoveryCodes(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("%d codes, want %d", len(codes), RecoveryCodeCount)
	}

	owner, err := UseRecoveryCode(ctx, store, codes[0])
	if err != nil {
		t.Fatal(err)
	}
	if owner != "user-1" {
		t.Fatalf("code belonged to %q, want user-1", owner)
	}

	if _, err := UseRecoveryCode(ctx, store, codes[0]); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("a recovery code was spent twice: %v", err)
	}

	// The others still work.
	if _, err := UseRecoveryCode(ctx, store, codes[1]); err != nil {
		t.Fatalf("a second code was invalidated by the first: %v", err)
	}
}

// Re-issuing invalidates the old set. Otherwise "regenerate my recovery codes"
// leaves the ones printed on the paper someone lost still working.
func TestReissuingRecoveryCodesInvalidatesTheOldOnes(t *testing.T) {
	ctx := context.Background()
	store := sqliteStore(t)

	old, err := NewRecoveryCodes(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewRecoveryCodes(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := UseRecoveryCode(ctx, store, old[0]); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("a superseded recovery code still works")
	}
	if _, err := UseRecoveryCode(ctx, store, fresh[0]); err != nil {
		t.Fatal(err)
	}
}

// Purposes keep the flows apart. A recovery code redeemable at the password
// reset endpoint would be a second way in that nobody designed.
func TestARecoveryCodeIsNotAPasswordResetToken(t *testing.T) {
	ctx := context.Background()
	store := sqliteStore(t)

	codes, err := NewRecoveryCodes(ctx, store, "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Redeem(ctx, store, codes[0], PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
		t.Fatalf("a recovery code was redeemed as a password reset: %v", err)
	}
}
