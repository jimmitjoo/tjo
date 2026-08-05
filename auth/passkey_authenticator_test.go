package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// A virtual authenticator, so the ceremonies are tested by running them.
//
// The alternative is a mock that returns whatever the test wants, which proves
// the code calls a function -- not that a browser and a security key can
// actually sign in. Two of this project's four advisories were in generated
// authentication code that nothing could test; a ceremony verified only against
// a stub is the same failure with more ceremony.
//
// It implements just enough of CTAP: an ES256 key pair, "none" attestation, and
// a signature over the concatenation the specification asks for.
type virtualAuthenticator struct {
	key          *ecdsa.PrivateKey
	credentialID []byte
	rpID         string
	origin       string
	signCount    uint32
}

func newAuthenticator(t *testing.T, rpID, origin string) *virtualAuthenticator {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}

	return &virtualAuthenticator{key: key, credentialID: id, rpID: rpID, origin: origin}
}

// WebAuthn flags, from §6.1 of the specification.
const (
	flagUserPresent   = 0x01
	flagUserVerified  = 0x04
	flagAttestedData  = 0x40
	flagBackupEligbl  = 0x08
	flagBackupCurrent = 0x10
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// challengeOf pulls the challenge out of the options the browser would have
// been given, which is exactly what a real client does.
func challengeOf(t *testing.T, options json.RawMessage) string {
	t.Helper()

	var envelope struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.PublicKey.Challenge == "" {
		t.Fatalf("no challenge in ceremony options: %s", options)
	}
	return envelope.PublicKey.Challenge
}

func (a *virtualAuthenticator) clientData(t *testing.T, ceremony, challenge string) []byte {
	t.Helper()

	data, err := json.Marshal(map[string]any{
		"type":        ceremony,
		"challenge":   challenge,
		"origin":      a.origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// authenticatorData is rpIdHash || flags || signCount, plus the attested
// credential when this is a registration.
func (a *virtualAuthenticator) authenticatorData(t *testing.T, attested bool) []byte {
	t.Helper()

	rpIDHash := sha256.Sum256([]byte(a.rpID))

	flags := byte(flagUserPresent | flagUserVerified | flagBackupEligbl | flagBackupCurrent)
	if attested {
		flags |= flagAttestedData
	}

	out := make([]byte, 0, 128)
	out = append(out, rpIDHash[:]...)
	out = append(out, flags)

	a.signCount++
	out = binary.BigEndian.AppendUint32(out, a.signCount)

	if attested {
		out = append(out, make([]byte, 16)...) // zero AAGUID, as a platform authenticator reports
		out = binary.BigEndian.AppendUint16(out, uint16(len(a.credentialID)))
		out = append(out, a.credentialID...)
		out = append(out, a.coseKey(t)...)
	}

	return out
}

// coseKey is the public key in the COSE_Key form WebAuthn stores.
func (a *virtualAuthenticator) coseKey(t *testing.T) []byte {
	t.Helper()

	x := make([]byte, 32)
	y := make([]byte, 32)
	a.key.PublicKey.X.FillBytes(x)
	a.key.PublicKey.Y.FillBytes(y)

	key, err := cbor.Marshal(map[int]any{
		1:  2,  // kty: EC2
		3:  -7, // alg: ES256
		-1: 1,  // crv: P-256
		-2: x,
		-3: y,
	})
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// Register produces the JSON a browser posts back after
// navigator.credentials.create().
func (a *virtualAuthenticator) Register(t *testing.T, challenge string) []byte {
	t.Helper()

	clientData := a.clientData(t, "webauthn.create", challenge)
	authData := a.authenticatorData(t, true)

	attestation, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(map[string]any{
		"id":    b64(a.credentialID),
		"rawId": b64(a.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64(clientData),
			"attestationObject": b64(attestation),
			"transports":        []string{"internal", "hybrid"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// Assert produces the JSON a browser posts back after
// navigator.credentials.get().
func (a *virtualAuthenticator) Assert(t *testing.T, challenge string, userHandle []byte) []byte {
	t.Helper()

	clientData := a.clientData(t, "webauthn.get", challenge)
	authData := a.authenticatorData(t, false)

	clientDataHash := sha256.Sum256(clientData)
	signed := append(append([]byte{}, authData...), clientDataHash[:]...)
	digest := sha256.Sum256(signed)

	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(map[string]any{
		"id":    b64(a.credentialID),
		"rawId": b64(a.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64(clientData),
			"authenticatorData": b64(authData),
			"signature":         b64(signature),
			"userHandle":        b64(userHandle),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}
