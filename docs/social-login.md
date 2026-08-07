# Social login

Sign in with Google, GitHub, Microsoft Entra, or any OpenID Connect provider
that publishes a discovery document.

```bash
tjo make auth
```

writes the handlers, the routes and the buttons. A provider appears on the
login page when its credentials are in `.env`, and not otherwise, so a project
that has configured none shows a plain password form.

## Configuration

```bash
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...

GITHUB_CLIENT_ID=...
GITHUB_CLIENT_SECRET=...

MICROSOFT_TENANT=...          # the tenant GUID, or "consumers"
MICROSOFT_CLIENT_ID=...
MICROSOFT_CLIENT_SECRET=...

OIDC_NAME=okta                # any OpenID provider, by discovery
OIDC_ISSUER=https://example.okta.com
OIDC_CLIENT_ID=...
OIDC_CLIENT_SECRET=...
```

Register `<APP_URL>/auth/<name>/callback` as the redirect URL with each
provider. The routes are:

| Method | Path | What it does |
|---|---|---|
| GET | `/auth/{provider}` | Starts a ceremony and redirects to the provider |
| GET | `/auth/{provider}/callback` | Completes it |
| POST | `/user/social/{provider}/unlink` | Detaches a provider from the signed-in account |

`OIDC_NAME` appears in URLs and is stored with every identity. Changing it
later orphans them, so pick it once.

### Microsoft: the tenant GUID, and nothing else

`MICROSOFT_TENANT` must be the tenant's GUID, or `consumers` for personal
Microsoft accounts (which `auth.Microsoft` substitutes for the GUID that
stands for).

Not a domain, not `common`, not `organizations`. Entra serves a discovery
document for all three, and none of them can be used, because Entra reports the
tenant **id** as the issuer however the tenant was addressed:

| `MICROSOFT_TENANT` | Discovery reports | |
|---|---|---|
| `72f988bf-…` | `…/72f988bf-…/v2.0` | matches |
| `contoso.onmicrosoft.com` | `…/72f988bf-…/v2.0` | mismatch |
| `common`, `organizations` | `…/{tenantid}/v2.0` | a placeholder |

To find a domain's GUID, read the `issuer` field of
`https://login.microsoftonline.com/<domain>/v2.0/.well-known/openid-configuration`.

`NewOAuth` refuses the unusable forms at start-up rather than letting the issuer
check fail later, because the workaround somebody reaches for on seeing an
issuer mismatch — turning the check off — accepts tokens from every Entra tenant
that exists.

Multi-tenant sign-in means having a policy about which organizations may sign
in. That is an application's decision, so it is out of scope here rather than
approximated.

## The linking policy

This is the part worth reading before changing anything.

`auth.Resolve` decides which account a verified identity signs into, in this
order:

1. **The identity is already linked.** That account, and only that account. If
   somebody else is signed in, `ErrLinkedElsewhere`.
2. **Somebody is signed in.** Link the identity to them, and sign in.
3. **Nobody is signed in and an account already uses that email.**
   `NeedsLogin`. Never an automatic merge.
4. **Otherwise.** `NoAccount` — create one, then call `auth.Link`.

```go
resolution, err := auth.Resolve(ctx, identities, identity, auth.ResolveOptions{
    CurrentAccountID: currentlySignedIn,   // "" when nobody is
    Accounts:         data.Accounts{},
})

switch resolution.Outcome {
case auth.SignedIn:
    // resolution.AccountID. Renew the session and rotate the CSRF token.
case auth.NeedsLogin:
    // An account uses that address. Send them to the password form.
case auth.NoAccount:
    // Create an account from resolution.Identity, then auth.Link.
}
```

### Why rule 3 is not a merge

The convenient version signs the person into the existing account: same email,
same person, one fewer click. It is also an account takeover. Register an
account at an identity provider using the victim's address, sign in, be handed
their account.

Trusting only providers that verify addresses does not fix it. It makes the
flow exactly as safe as every provider it is ever configured with — including
the corporate OIDC issuer somebody adds in two years, whose `email_verified`
means whatever that organization decided it means. Requiring a login makes it
depend on none of them.

So `Resolve` refuses regardless of `email_verified`. The claim is still on
`Identity.EmailVerified`, because it is the right thing to check before marking
an address verified on an account you create.

### The identity key

`(provider, subject)`, never the email. An email changes and a subject does
not, and keying on the email means somebody who changes their address at the
provider signs into a stranger's account.

## Storage

`SQLIdentityStore` for PostgreSQL, MySQL and SQLite:

```go
identities := auth.NewSQLIdentityStore(db, auth.DialectPostgres)
identities.Migrate(ctx)
```

One row per identity, unique on `(account_id, provider)` — so an account has at
most one Google identity, and "unlink Google" is unambiguous. Linking the same
provider again replaces the row, and the previous provider account genuinely
loses access rather than quietly keeping it.

Your own `IdentityStore` works too. The interface is four methods, and the
users table stays yours.

## Unlinking

```go
err := auth.Unlink(ctx, identities, account, "google", otherCredentials)
```

Refuses with `ErrLastIdentity` when it would leave an account with no way in —
no other identity, no password, and `otherCredentials` zero. Support cannot
undo that one, because there is nothing left to verify the owner with.

`otherCredentials` is the count of sign-in methods this package cannot see,
which today means passkeys. Counting them is the caller's job for the same
reason `RevokePasskey` leaves the equivalent check to its caller: guessing
means either blocking a legitimate unlink or locking somebody out.

## What the flow does not do

**It does not create a session.** `Finish` reports who signed in; renewing the
session and rotating the CSRF token is the caller's, exactly as with every
other login path in this framework. Skipping it is session fixation. The
generated handler ends in `completeLogin`, which is the one place a scaffolded
application's session becomes authenticated.

**It does not store tokens.** No refresh tokens, no access to provider APIs.
Reading somebody's calendar is a different feature with a different consent
screen and a different retention question.

**It does not do SAML.** Different protocol, different problem.

## Security notes

Every ceremony sends state, PKCE (S256) and — for OIDC providers — a nonce.
None of them is configurable, because a switch that turned one off would only
ever be turned off by mistake:

- **state** is login CSRF protection: without it, somebody else's authorization
  can be completed in this browser.
- **PKCE** binds the authorization code to the client that asked for it.
- **nonce** binds the ID token to this request. A token replayed from another
  ceremony has a perfect signature.

The ceremony state holds the PKCE verifier and the nonce, so it belongs in a
server-side session, and it is single-use: the generated handler reads it with
`Session.Pop`. It also expires after `auth.OAuthCeremonyTTL` (15 minutes).

ID tokens are verified with [`coreos/go-oidc`](https://github.com/coreos/go-oidc):
signature against the provider's JWKS, issuer, audience and expiry. This
framework prefers the standard library and has removed four dependencies for
it; this is where that stops applying. Two of its four published advisories
were in hand-written authentication code, and a subtly wrong ID token verifier
accepts a forged identity.

### GitHub

GitHub is OAuth 2.0 without OpenID Connect, so there is no ID token and the
identity comes from `GET /user`. Two consequences:

- The address on the profile is one its owner typed into a form, so it is never
  reported as verified. Under the linking policy above that changes nothing, an
  email decides no account either way.
- Most GitHub users keep that field private, so it usually arrives empty. When
  it does, `GET /user/emails` is read for the primary address — and only if
  GitHub has verified it, in which case `EmailVerified` is true, because that
  one is the provider's own assertion rather than a self-declared string.

An account with no verified primary address therefore signs in with no email.
The generated sign-up refuses to create an account from it (`users.email` is
`NOT NULL UNIQUE`, so the alternative is one account and then a constraint
violation); linking it to an existing account works normally.

## Testing

The `auth` tests run against a local test issuer that serves real discovery, a
real JWKS and really signed RS256 tokens — including wrong ones: signed by a
stranger's key, minted for another client, expired, and carrying another
ceremony's nonce. No test in this repository calls a third-party API.
