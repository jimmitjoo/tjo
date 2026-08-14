# HTTPS

Serving TLS from the binary, with a certificate you have or one from Let's
Encrypt.

```go
app.Server.TLS = &tjo.TLSConfig{
    Hosts:    []string{"example.com", "www.example.com"},
    CacheDir: "/var/lib/myapp/certs",
    Email:    "ops@example.com",
}

app.ListenAndServe()
```

or, with a certificate already on disk:

```go
app.Server.TLS = &tjo.TLSConfig{
    CertFile: "/etc/ssl/site.pem",
    KeyFile:  "/etc/ssl/site.key",
}
```

## When not to use this

**Most production deployments should not.** If a load balancer, nginx or Caddy
already terminates TLS, leave `Server.TLS` nil. The framework then serves
cleartext HTTP/2 to the proxy, which is what `UnencryptedHTTP2` in
`ListenAndServe` is for, and the generated `docker-compose.yml` and nginx
configuration are the right answer for that shape.

This is for the case Go is good at and `tjo deploy` aims at: one binary on one
VM. Requiring a reverse proxy to get HTTPS makes the simplest deployment the
one with the most moving parts.

It also settles the HTTP/2 question. The `sse` package documents that HTTP/2 is
not optional — browsers cap concurrent HTTP/1.1 connections to an origin at six,
an SSE response never completes, so six open streams deadlock every other
request including stylesheets and form posts. Go negotiates HTTP/2 over TLS
automatically, so a binary serving its own TLS has HTTP/2 with no h2c and no
proxy.

## Nothing happens by default

`Server.TLS` is nil unless you set it. A framework that started requesting
certificates because a configuration value looked set is a framework that gets
somebody's domain rate-limited — and Let's Encrypt counts per registered domain
per week, so the mistake costs days.

## `Hosts` is required, and it is the important field

```go
Hosts: []string{"example.com", "www.example.com"},
```

`autocert`'s own default host policy permits **every** hostname. A manager
without one attempts issuance for anything anybody points at the server, which
is a way for a stranger to burn a rate limit that belongs to you. This package
refuses to build a configuration without `Hosts`, and a request for an unlisted
name is refused before an ACME order starts.

Both `example.com` and `www.example.com` need listing if both are served. A
certificate is issued per name.

## `CacheDir` is required too

It holds the certificates and the ACME account key, and it has to survive
restarts. `autocert`'s zero-value cache keeps nothing, so every restart
re-issues — which works perfectly in testing, where nobody restarts fifty times
a week, and exhausts a rate limit in production by Thursday.

Created with `0700` if it does not exist: it contains private keys.

## Ports and challenges

| | |
|---|---|
| `Addr` | HTTPS. Empty means `:443` |
| `RedirectFrom` | Plain HTTP. Empty means `:80` with `Hosts`, nothing without. `"-"` for none |

Two challenge types are available and both are configured:

- **TLS-ALPN-01**, over 443. Works when port 80 is closed. `acme-tls/1` is
  advertised in `NextProtos`.
- **HTTP-01**, over 80, answered by the plain-HTTP listener.

The plain-HTTP listener serves `/.well-known/acme-challenge/` and redirects
everything else to HTTPS. **The challenge path is never redirected** —
redirecting it would make the certificate a prerequisite for obtaining the
certificate.

The redirect is a **308**, not a 301: a 301 turns a POST into a GET and drops
its body, which loses a form submission in a way that is very hard to see.

If port 80 is already in use the listener logs and the process carries on. That
costs the HTTP-01 challenge and the redirect; TLS-ALPN-01 still obtains
certificates on 443.

## HSTS

`Strict-Transport-Security` is sent only over a secure connection — over TLS, or
behind a proxy that sets `X-Forwarded-Proto: https`.

RFC 6797 §7.2 requires that, and the reason is practical: a site that sends HSTS
before TLS works pins every visitor's browser to HTTPS for a year, cached
client-side, and no server-side change undoes it. Enabling HSTS and enabling TLS
are separate acts, and the first must not break a site while the second is being
arranged.

`X-Forwarded-Proto` is trusted because the common deployment terminates TLS
upstream, and not trusting it would mean no HSTS for most production sites. It
is the proxy's job to overwrite that header rather than pass a client's through;
the worst a spoofed one does is make a visitor's own browser insist on HTTPS.

## What this is not

No certificate management, no renewal dashboard, no control plane. `autocert`
renews in the background; there is nothing to operate.

ACME is not reimplemented. `golang.org/x/crypto/acme/autocert` is the standard
and `x/crypto` was already a direct dependency.
