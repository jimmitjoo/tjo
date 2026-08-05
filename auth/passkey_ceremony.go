package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// The two WebAuthn ceremonies, wrapped narrowly enough that the library behind
// them can be replaced.
//
// go-webauthn is the only real Go option and it is low-level, which is fine:
// the job here is not to hide it but to keep it from reaching the application.
// Nothing below returns a go-webauthn type, and the credential is stored in the
// opaque record format (see passkey.go), so replacing the backend -- with
// crypto/passkey if golang/go#80663 lands for Go 1.28 -- is a change to this
// file and nothing else.
//
// # What would change if crypto/passkey lands
//
// NewPasskeys builds a *webauthn.WebAuthn from PasskeyConfig; that becomes a
// crypto/passkey relying party. Begin/Finish call four library functions
// between them, and the record already holds whatever the new library needs to
// be handed back. The stored rows do not move. That is the entire point of
// having done the format first.
//
// # Clone detection is not implemented, deliberately
//
// A signature counter that goes backwards means a credential was copied. Acting
// on that requires writing the new counter after every login, and PasskeyStore
// has no update method because a record is not meant to be edited. The trade is
// deliberate rather than overlooked: synced passkeys -- which is what almost
// everyone now has -- report a counter of zero forever, so the check would
// protect only hardware keys, and paying for it with a mutable credential
// record is the wrong trade for a library whose storage is the caller's.
// An application that issues hardware keys and wants clone detection can keep
// the counter itself; FinishLogin reports it.

// ErrUnknownCredential is returned when an authenticator presents a credential
// this application has never stored.
//
// One error whether the credential is unknown, revoked, or belongs to an
// account that no longer exists. A login form that distinguishes them tells an
// attacker which of their guesses corresponds to a real account.
var ErrUnknownCredential = errors.New("auth: unknown passkey credential")

// PasskeyConfig identifies the relying party -- this application, as the
// authenticator sees it.
type PasskeyConfig struct {
	// RelyingPartyID is the origin's registrable domain without scheme or port,
	// "example.com" for https://app.example.com.
	//
	// It is baked into every credential the authenticator creates, and changing
	// it invalidates all of them. Set it to the registrable domain rather than
	// the current host, or moving from example.com to app.example.com becomes a
	// re-registration for every user.
	RelyingPartyID string

	// RelyingPartyName is what the authenticator shows the user.
	RelyingPartyName string

	// Origins are the full origins allowed to run ceremonies,
	// "https://app.example.com". At least one is required.
	Origins []string

	// RequireUserVerification demands a PIN, biometric or equivalent rather
	// than mere presence.
	//
	// Off by default, which is the WebAuthn default and the right one for a
	// second factor. Turn it on when a passkey is the only factor, because
	// without it "possession of an unlocked laptop" is the whole authentication.
	RequireUserVerification bool
}

// Passkeys runs the registration and authentication ceremonies.
type Passkeys struct {
	wa    *webauthn.WebAuthn
	store PasskeyStore
	uv    protocol.UserVerificationRequirement
}

// NewPasskeys returns a ceremony runner.
func NewPasskeys(cfg PasskeyConfig, store PasskeyStore) (*Passkeys, error) {
	if cfg.RelyingPartyID == "" {
		return nil, errors.New("auth: passkeys need a relying party id")
	}
	if len(cfg.Origins) == 0 {
		return nil, errors.New("auth: passkeys need at least one origin")
	}
	if store == nil {
		return nil, errors.New("auth: passkeys need a store")
	}

	uv := protocol.VerificationPreferred
	if cfg.RequireUserVerification {
		uv = protocol.VerificationRequired
	}

	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RelyingPartyID,
		RPDisplayName: cfg.RelyingPartyName,
		RPOrigins:     cfg.Origins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			// Discoverable, so the browser can offer the account without the
			// user typing an address first. This is what people mean by
			// "passkey" as opposed to "security key".
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: uv,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("auth: passkey config: %w", err)
	}

	return &Passkeys{wa: wa, store: store, uv: uv}, nil
}

// Ceremony is a challenge in flight.
//
// Options is JSON for the browser: hand it to navigator.credentials.create()
// or .get() unchanged.
//
// State is opaque and belongs in the user's server-side session until the
// matching Finish call. It must not travel through a form field, a query
// parameter or an unsigned cookie: the challenge is the entire replay
// protection of the ceremony, and one the client can choose is not a challenge.
type Ceremony struct {
	Options json.RawMessage
	State   []byte
}

// BeginRegistration starts adding a passkey to an existing, authenticated
// account.
//
// accountID is the stable internal identifier and becomes the WebAuthn user
// handle, which is stored by the authenticator and synced to the user's other
// devices. Do not pass an email address: it is neither stable nor private, and
// it ends up on hardware outside your control.
//
// name and displayName are shown in the authenticator's account picker.
func (p *Passkeys) BeginRegistration(ctx context.Context, accountID, name, displayName string) (*Ceremony, error) {
	if accountID == "" {
		return nil, errors.New("auth: registration needs an account id")
	}

	// Existing credentials go in the exclude list, so an authenticator that
	// already holds one for this account says so instead of silently creating a
	// second. Without it a user who clicks "add a passkey" twice ends up with
	// two credentials on one device and no way to tell them apart.
	existing, err := p.credentialsFor(ctx, accountID)
	if err != nil {
		return nil, err
	}

	user := &passkeyUser{
		id:          []byte(accountID),
		name:        name,
		displayName: displayName,
		credentials: existing,
	}

	options, session, err := p.wa.BeginRegistration(user,
		webauthn.WithExclusions(user.excludeList()))
	if err != nil {
		return nil, fmt.Errorf("auth: begin passkey registration: %w", err)
	}

	return marshalCeremony(options, session)
}

// FinishRegistration verifies the authenticator's response and stores the
// credential.
//
// accountID must be the account the ceremony was begun for -- read it from the
// session, never from the request body. state is what BeginRegistration
// returned.
func (p *Passkeys) FinishRegistration(ctx context.Context, accountID, label string, state []byte, r *http.Request) (*PasskeyRecord, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal(state, &session); err != nil {
		return nil, fmt.Errorf("auth: passkey ceremony state: %w", err)
	}

	// The handle the authenticator was given at Begin has to be the account
	// being registered now, or someone who starts a ceremony as themselves and
	// finishes it against another session registers their own key on somebody
	// else's account.
	//
	// go-webauthn checks this too, so removing the four lines below does not
	// currently fail the test that covers it. They stay anyway: that check is
	// an implementation detail of a dependency rather than a documented promise,
	// and this is not a property to discover the loss of after an upgrade.
	if string(session.UserID) != accountID {
		return nil, errors.New("auth: passkey ceremony belongs to a different account")
	}

	existing, err := p.credentialsFor(ctx, accountID)
	if err != nil {
		return nil, err
	}

	user := &passkeyUser{id: []byte(accountID), credentials: existing}

	credential, err := p.wa.FinishRegistration(user, session, r)
	if err != nil {
		return nil, fmt.Errorf("auth: finish passkey registration: %w", err)
	}

	record, err := EncodePasskey(credential.ID, credential, transportStrings(credential.Transport))
	if err != nil {
		return nil, err
	}

	if err := p.store.AddPasskey(ctx, accountID, record, label); err != nil {
		return nil, err
	}
	return record, nil
}

// BeginLogin starts a discoverable login: the authenticator offers the account,
// so there is no username field and nothing to enumerate.
func (p *Passkeys) BeginLogin(ctx context.Context) (*Ceremony, error) {
	options, session, err := p.wa.BeginDiscoverableLogin(
		webauthn.WithUserVerification(p.uv))
	if err != nil {
		return nil, fmt.Errorf("auth: begin passkey login: %w", err)
	}
	return marshalCeremony(options, session)
}

// Login is the outcome of a successful authentication ceremony.
type Login struct {
	// AccountID is who authenticated.
	AccountID string

	// CredentialID is which passkey they used, so an application can show
	// "signed in with your phone" or revoke that specific credential.
	CredentialID []byte

	// SignCount is the authenticator's counter. Synced passkeys report zero
	// forever; see the note on clone detection at the top of this file.
	SignCount uint32
}

// FinishLogin verifies the authenticator's response and reports who signed in.
//
// It does not create a session. That is the caller's, and it is where the
// session has to be renewed -- a passkey login that reuses the anonymous
// session id is a session-fixation bug wearing better cryptography.
func (p *Passkeys) FinishLogin(ctx context.Context, state []byte, r *http.Request) (*Login, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal(state, &session); err != nil {
		return nil, fmt.Errorf("auth: passkey ceremony state: %w", err)
	}

	var accountID string

	// The handler runs during verification, before anything is trusted: it maps
	// the credential the authenticator presented to the account that owns it.
	lookup := func(rawID, userHandle []byte) (webauthn.User, error) {
		owner, record, err := p.store.PasskeyByCredentialID(ctx, rawID)
		if err != nil {
			return nil, err
		}
		if record == nil {
			return nil, ErrUnknownCredential
		}

		// The authenticator also asserts a user handle. It must agree with the
		// account the credential is stored against, or a credential could be
		// replayed to claim a different account. Belt and braces over the
		// library's own check, for the reason given in FinishRegistration.
		if len(userHandle) > 0 && string(userHandle) != owner {
			return nil, ErrUnknownCredential
		}

		credential, err := decodeCredential(record)
		if err != nil {
			return nil, err
		}

		accountID = owner
		return &passkeyUser{id: []byte(owner), credentials: []webauthn.Credential{*credential}}, nil
	}

	_, credential, err := p.wa.FinishPasskeyLogin(lookup, session, r)
	if err != nil {
		if errors.Is(err, ErrUnknownCredential) {
			return nil, ErrUnknownCredential
		}
		return nil, fmt.Errorf("auth: finish passkey login: %w", err)
	}

	return &Login{
		AccountID:    accountID,
		CredentialID: credential.ID,
		SignCount:    credential.Authenticator.SignCount,
	}, nil
}

// Revoke removes one credential from an account.
//
// It refuses to remove the last one when the account has no password, because
// that is a lockout with no recovery path -- the account would be reachable
// only by a support ticket against the database. hasPassword is the caller's to
// answer: this package does not own the user record and cannot see it.
func (p *Passkeys) Revoke(ctx context.Context, accountID string, credentialID []byte, hasPassword bool) error {
	if !hasPassword {
		remaining, err := p.store.PasskeysFor(ctx, accountID)
		if err != nil {
			return err
		}
		if len(remaining) <= 1 {
			return errors.New("auth: refusing to revoke the last passkey of an account with no password")
		}
	}
	return p.store.RevokePasskey(ctx, accountID, credentialID)
}

// credentialsFor decodes an account's stored records.
func (p *Passkeys) credentialsFor(ctx context.Context, accountID string) ([]webauthn.Credential, error) {
	records, err := p.store.PasskeysFor(ctx, accountID)
	if err != nil {
		return nil, err
	}

	credentials := make([]webauthn.Credential, 0, len(records))
	for _, record := range records {
		credential, err := decodeCredential(record)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, *credential)
	}
	return credentials, nil
}

func decodeCredential(record *PasskeyRecord) (*webauthn.Credential, error) {
	var credential webauthn.Credential
	if _, err := DecodePasskey(record.Record, &credential); err != nil {
		return nil, err
	}
	return &credential, nil
}

func marshalCeremony(options any, session *webauthn.SessionData) (*Ceremony, error) {
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	encodedState, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	return &Ceremony{Options: encodedOptions, State: encodedState}, nil
}

func transportStrings(transports []protocol.AuthenticatorTransport) []string {
	out := make([]string, 0, len(transports))
	for _, t := range transports {
		out = append(out, string(t))
	}
	return out
}

// passkeyUser adapts an account to what go-webauthn wants, and exists so that
// interface never reaches the application.
type passkeyUser struct {
	id          []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.displayName }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (u *passkeyUser) excludeList() []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(u.credentials))
	for _, c := range u.credentials {
		out = append(out, c.Descriptor())
	}
	return out
}
