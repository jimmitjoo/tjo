package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewTokenIsUniqueAndVerifiable(t *testing.T) {
	seen := map[string]bool{}

	for i := 0; i < 100; i++ {
		tok, err := NewToken(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok.PlainText] {
			t.Fatalf("token %q was issued twice", tok.PlainText)
		}
		seen[tok.PlainText] = true

		if err := tok.Verify(tok.PlainText, time.Now()); err != nil {
			t.Fatalf("a freshly minted token does not verify: %v", err)
		}
	}
}

func TestVerifyRejects(t *testing.T) {
	tok, err := NewToken(time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		plain string
		now   time.Time
	}{
		{"wrong token", "AAAAAAAAAAAAAAAAAAAAAAAAAA", time.Now()},
		{"empty", "", time.Now()},
		{"truncated", tok.PlainText[:len(tok.PlainText)-1], time.Now()},
		{"expired", tok.PlainText, tok.Expiry.Add(time.Second)},
		{"exactly at expiry", tok.PlainText, tok.Expiry},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := tok.Verify(c.plain, c.now); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("Verify = %v, want ErrInvalidToken", err)
			}
		})
	}
}

// A zero Token must not verify anything. Without this guard an empty struct --
// from a failed lookup that was not checked -- would accept a token whose hash
// is also empty.
func TestZeroTokenVerifiesNothing(t *testing.T) {
	var tok Token
	if err := tok.Verify("", time.Now()); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("a zero Token accepted an empty plaintext: %v", err)
	}

	var nilTok *Token
	if err := nilTok.Verify("anything", time.Now()); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("a nil Token accepted a plaintext: %v", err)
	}
}

// The plaintext must never be serialised. v0.7.0 fixed exactly this: tokens
// were persisted in plaintext and marshalled into JSON responses, so a logged
// response body handed over working credentials.
func TestPlaintextIsNeverSerialised(t *testing.T) {
	tok, err := NewToken(time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	out, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(out), tok.PlainText) {
		t.Errorf("the plaintext token appears in JSON: %s", out)
	}
	// The hash must not leak either; it is enough to authenticate with if the
	// comparison is ever done against a client-supplied hash.
	if strings.Contains(string(out), "hash") {
		t.Errorf("the hash appears in JSON: %s", out)
	}
}

func TestHashTokenMatchesNewToken(t *testing.T) {
	tok, err := NewToken(time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	stored := HashToken(tok.PlainText)
	if len(stored) != len(tok.Hash) {
		t.Fatalf("hash lengths differ: %d and %d", len(stored), len(tok.Hash))
	}
	for i := range stored {
		if stored[i] != tok.Hash[i] {
			t.Fatal("HashToken does not agree with NewToken; a token minted by one would never verify against the other")
		}
	}
}

func TestFromAuthorizationHeader(t *testing.T) {
	valid, err := NewToken(time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("accepts a bearer token", func(t *testing.T) {
		got, err := FromAuthorizationHeader("Bearer " + valid.PlainText)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != valid.PlainText {
			t.Errorf("got %q", got)
		}
	})

	t.Run("scheme is case-insensitive", func(t *testing.T) {
		if _, err := FromAuthorizationHeader("bearer " + valid.PlainText); err != nil {
			t.Errorf("err = %v; RFC 7235 makes the scheme case-insensitive", err)
		}
	})

	rejects := map[string]string{
		"empty":            "",
		"no scheme":        valid.PlainText,
		"wrong scheme":     "Basic " + valid.PlainText,
		"scheme only":      "Bearer",
		"scheme and space": "Bearer ",
		"extra fields":     "Bearer " + valid.PlainText + " extra",
		"wrong length":     "Bearer AAAA",
		"embedded tab":     "Bearer " + valid.PlainText[:5] + "\t" + valid.PlainText[6:],
	}

	for name, header := range rejects {
		t.Run("rejects "+name, func(t *testing.T) {
			if _, err := FromAuthorizationHeader(header); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("FromAuthorizationHeader(%q) = %v, want ErrInvalidToken", header, err)
			}
		})
	}

	// Case matters for the token, but that is Verify's job, not the header
	// parser's -- extraction succeeds and the hash then fails to match. The
	// alternative, rejecting on case here, would encode base32's alphabet in
	// two places and let them disagree.
	t.Run("case is enforced by Verify, not by extraction", func(t *testing.T) {
		lower := strings.ToLower(valid.PlainText)

		got, err := FromAuthorizationHeader("Bearer " + lower)
		if err != nil {
			t.Fatalf("extraction rejected a well-formed header: %v", err)
		}
		if err := valid.Verify(got, time.Now()); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("a lowercased token verified: %v", err)
		}
	})
}

// Example shows the two halves of a token's life. It is compiled and run by
// `go test`, so it cannot drift from the API the way a README snippet can.
func Example() {
	// At issue time: show the plaintext to the user, store only the hash.
	issued, err := NewToken(24 * time.Hour)
	if err != nil {
		panic(err)
	}
	stored := struct {
		Hash   []byte
		Expiry time.Time
	}{issued.Hash, issued.Expiry}

	// At request time: the plaintext arrives in a header, and the stored hash
	// is all that is needed to check it.
	presented, err := FromAuthorizationHeader("Bearer " + issued.PlainText)
	if err != nil {
		panic(err)
	}

	check := &Token{Hash: stored.Hash, Expiry: stored.Expiry}
	fmt.Println(check.Verify(presented, time.Now()) == nil)

	// A token that is not ours does not verify.
	fmt.Println(check.Verify("AAAAAAAAAAAAAAAAAAAAAAAAAA", time.Now()) == nil)

	// Output:
	// true
	// false
}
