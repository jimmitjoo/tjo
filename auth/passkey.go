package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Passkey storage, in a format designed to outlive the library that produced
// it.
//
// The reason to be careful here is a date. Filippo Valsorda opened
// golang/go#80663, `proposal: crypto/passkey`, on 2026-07-31, targeting Go
// 1.28, and his companion post proposes an opaque, interoperable record format
// shaped deliberately like a PHC password hash:
//
//	$webauthn$v=1$transports=hybrid+internal$<base64 authdata>
//
// The point of that shape is that the application stores an opaque string and
// never parses it, so the library behind it can be replaced without migrating
// credentials. A bespoke schema -- one column per WebAuthn field -- locks the
// database to whichever library wrote it, and there is a credential migration
// at the end of that.
//
// So: this package stores records in that format. If crypto/passkey lands, the
// backend swaps and the stored rows do not move. If it does not, nothing is
// lost, because the format is self-describing either way.
//
// # Ship passkeys as an option, never as the only route
//
// The 2026 discourse is genuinely negative and the volume is large: "Passkeys
// were invented by engineers with zero understanding of consumer brain" ran to
// 781 comments in July 2026. Account recovery is the unsolved part, and a
// framework that makes passkeys the only way in inherits that problem on its
// users' behalf. Password and reset-link flows stay first-class.

// ErrMalformedRecord is returned when a stored record cannot be parsed.
var ErrMalformedRecord = errors.New("auth: malformed passkey record")

// recordPrefix identifies the format. Versioned, because the whole argument for
// this shape is that it survives change.
const recordPrefix = "$webauthn$"

// PasskeyRecord is one stored credential.
//
// It is produced by EncodePasskey and consumed by DecodePasskey; applications
// store Record and nothing else. The struct is exported so a caller can inspect
// what they have, not so they can build the string themselves.
type PasskeyRecord struct {
	// Record is the opaque stored form. This is the only field that belongs in
	// a database column.
	Record string

	// CredentialID identifies the credential to the authenticator. Stored
	// separately only because lookups need an indexable key -- it is derivable
	// from Record.
	CredentialID []byte

	// Transports is a hint about how the authenticator is reachable
	// ("internal", "hybrid", "usb", "nfc", "ble").
	Transports []string
}

// EncodePasskey renders a credential into the opaque record format.
//
// credential is whatever the WebAuthn library produced. It is marshalled to
// JSON and base64'd rather than being decomposed into fields, which is the
// whole point: this package does not claim to know the shape of a credential,
// and neither should the database.
func EncodePasskey(credentialID []byte, credential any, transports []string) (*PasskeyRecord, error) {
	if len(credentialID) == 0 {
		return nil, errors.New("auth: passkey needs a credential id")
	}

	payload, err := json.Marshal(credential)
	if err != nil {
		return nil, fmt.Errorf("auth: encoding passkey: %w", err)
	}

	// Normalised and sorted so the same credential always renders identically,
	// which makes records comparable and diffable.
	transports = slices.Clone(transports)
	for i := range transports {
		transports[i] = strings.ToLower(strings.TrimSpace(transports[i]))
	}
	transports = slices.DeleteFunc(transports, func(s string) bool { return s == "" })
	slices.Sort(transports)
	transports = slices.Compact(transports)

	var b strings.Builder
	b.WriteString(recordPrefix)
	b.WriteString("v=1$")
	if len(transports) > 0 {
		b.WriteString("transports=")
		b.WriteString(strings.Join(transports, "+"))
		b.WriteString("$")
	}
	b.WriteString(base64.RawStdEncoding.EncodeToString(payload))

	return &PasskeyRecord{
		Record:       b.String(),
		CredentialID: credentialID,
		Transports:   transports,
	}, nil
}

// DecodePasskey parses a stored record into credential, which must be a pointer
// to the type the WebAuthn library expects.
//
// The version is checked but the payload is not interpreted here. A record from
// a future version is refused rather than guessed at.
func DecodePasskey(record string, credential any) ([]string, error) {
	if !strings.HasPrefix(record, recordPrefix) {
		return nil, ErrMalformedRecord
	}

	rest := strings.TrimPrefix(record, recordPrefix)
	parts := strings.Split(rest, "$")
	if len(parts) < 2 {
		return nil, ErrMalformedRecord
	}

	if parts[0] != "v=1" {
		return nil, fmt.Errorf("%w: unsupported version %q", ErrMalformedRecord, parts[0])
	}

	var transports []string
	payload := parts[len(parts)-1]

	for _, p := range parts[1 : len(parts)-1] {
		if after, ok := strings.CutPrefix(p, "transports="); ok {
			transports = strings.Split(after, "+")
		}
		// Unknown parameters are ignored rather than rejected, so a record
		// written by a newer minor version still decodes. That is what makes
		// the format forward-compatible instead of merely versioned.
	}

	raw, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrMalformedRecord
	}

	if err := json.Unmarshal(raw, credential); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedRecord, err)
	}

	return transports, nil
}

// PasskeyStore is the storage the caller provides.
//
// Note what it does not have: an update method for the credential itself.
// Passkeys are not edited, they are added and revoked, and an interface that
// offered mutation would invite an application to rewrite a record it should
// treat as opaque.
type PasskeyStore interface {
	// AddPasskey stores a record for an account. An account may have several.
	AddPasskey(ctx context.Context, accountID string, rec *PasskeyRecord, label string) error

	// PasskeysFor returns every record for an account.
	PasskeysFor(ctx context.Context, accountID string) ([]*PasskeyRecord, error)

	// PasskeyByCredentialID finds the account and record for a credential.
	// Returns ("", nil, nil) when there is no such credential.
	PasskeyByCredentialID(ctx context.Context, credentialID []byte) (accountID string, rec *PasskeyRecord, err error)

	// RevokePasskey removes one credential from an account.
	//
	// Revoking must not be able to leave an account with no way in. That check
	// belongs to the caller, which knows whether a password is also set --
	// this interface deliberately does not, because guessing would either
	// block a legitimate revocation or allow a lockout.
	RevokePasskey(ctx context.Context, accountID string, credentialID []byte) error
}
