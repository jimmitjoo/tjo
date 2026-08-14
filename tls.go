package tjo

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Serving HTTPS from the binary itself.
//
// # When not to use this
//
// Most production deployments terminate TLS upstream -- a load balancer, nginx,
// Caddy -- and that is a good architecture. The generated docker-compose and
// nginx configuration assume it, and they are right for what they describe.
// Leave TLS unset there: the framework serves cleartext HTTP/2 to the proxy,
// which is what UnencryptedHTTP2 in ListenAndServe is for.
//
// This is for the case Go is good at and this framework's `deploy` command aims
// at: one binary on one VM. Requiring a reverse proxy to get HTTPS makes the
// simplest deployment the one with the most moving parts.
//
// It also resolves the interaction the sse package documents. Browsers cap
// concurrent HTTP/1.1 connections to an origin at six, an SSE response never
// completes, and six open streams therefore deadlock everything else. Go
// negotiates HTTP/2 over TLS automatically, so a binary serving its own TLS has
// HTTP/2 without h2c and without a proxy.
//
// # Nothing here happens by default
//
// TLSConfig is nil unless an application sets it. A framework that started
// requesting certificates because a configuration value looked set is a
// framework that gets somebody's domain rate-limited, and Let's Encrypt counts
// per registered domain per week.

// TLSConfig configures HTTPS. Set it on Tjo.Server.TLS before ListenAndServe.
//
// Either a certificate you already have:
//
//	app.Server.TLS = &tjo.TLSConfig{
//	    CertFile: "/etc/ssl/site.pem",
//	    KeyFile:  "/etc/ssl/site.key",
//	}
//
// or automatic certificates from Let's Encrypt:
//
//	app.Server.TLS = &tjo.TLSConfig{
//	    Hosts:    []string{"example.com", "www.example.com"},
//	    CacheDir: "/var/lib/myapp/certs",
//	    Email:    "ops@example.com",
//	}
type TLSConfig struct {
	// CertFile and KeyFile are a certificate and key already on disk.
	//
	// Mutually exclusive with Hosts: a configuration with both has not decided
	// where its certificates come from, and guessing means guessing wrong on
	// the day one of them expires.
	CertFile string
	KeyFile  string

	// Hosts are the hostnames to obtain certificates for, and the only ones.
	//
	// Required for automatic certificates, and required rather than optional:
	// autocert's own default host policy allows every hostname, so a manager
	// without one attempts issuance for anything pointed at the server. That is
	// a way for a stranger to burn a rate limit that belongs to somebody else.
	Hosts []string

	// CacheDir is where certificates and the ACME account key are kept.
	//
	// It has to survive restarts, or every restart re-issues and a week's rate
	// limit is gone by Thursday. Created with 0700 if it does not exist,
	// because it holds private keys.
	CacheDir string

	// Email is registered with the ACME account, so expiry warnings reach
	// somebody. Optional and worth setting.
	Email string

	// Addr is what to listen on. Empty means ":443".
	Addr string

	// RedirectFrom is the plain-HTTP address to redirect to HTTPS from. Empty
	// means ":80" when Hosts is set -- because HTTP-01 challenges arrive there
	// and a port 80 that is closed is one fewer way to get a certificate -- and
	// no redirect at all otherwise.
	//
	// Set to "-" to run no plain-HTTP listener.
	RedirectFrom string

	// MinVersion is the lowest TLS version accepted. Zero means TLS 1.2, which
	// is what every current browser and every compliance checklist expects.
	MinVersion uint16
}

// DefaultTLSAddr is where HTTPS is served when Addr is empty.
const DefaultTLSAddr = ":443"

// tlsConfig builds the server's TLS settings and, for autocert, the handler
// that answers HTTP-01 challenges on port 80.
func (c *TLSConfig) tlsConfig() (*tls.Config, http.Handler, error) {
	if c == nil {
		return nil, nil, nil
	}

	manual := c.CertFile != "" || c.KeyFile != ""

	switch {
	case manual && len(c.Hosts) > 0:
		return nil, nil, errors.New("tjo: TLSConfig has both a certificate file and Hosts; pick one source of certificates")

	case manual:
		if c.CertFile == "" || c.KeyFile == "" {
			return nil, nil, errors.New("tjo: TLSConfig needs both CertFile and KeyFile")
		}

		certificate, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("tjo: loading the certificate: %w", err)
		}

		return &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   c.minVersion(),
			// h2 first, so HTTP/2 is negotiated. Without it Go serves
			// HTTP/1.1 over TLS and the six-connection cap is back.
			NextProtos: []string{"h2", "http/1.1"},
		}, nil, nil

	case len(c.Hosts) > 0:
		return c.autocertConfig()

	default:
		return nil, nil, errors.New("tjo: TLSConfig needs either CertFile and KeyFile, or Hosts")
	}
}

func (c *TLSConfig) autocertConfig() (*tls.Config, http.Handler, error) {
	if c.CacheDir == "" {
		// No in-memory fallback. autocert's zero value keeps nothing, so every
		// restart re-issues -- and it works perfectly in testing, where nobody
		// restarts fifty times a week.
		return nil, nil, errors.New("tjo: automatic certificates need a CacheDir that survives restarts")
	}

	for _, host := range c.Hosts {
		if host == "" || strings.ContainsAny(host, "/:") {
			return nil, nil, fmt.Errorf("tjo: %q is not a hostname", host)
		}
	}

	// 0700: this directory holds the account key and every private key.
	if err := os.MkdirAll(c.CacheDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("tjo: preparing the certificate cache: %w", err)
	}

	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(c.CacheDir),
		Email:  c.Email,
		// Explicit, always. autocert falls back to a policy that permits every
		// hostname, and this is the only line standing between that and
		// somebody else's rate limit.
		HostPolicy: autocert.HostWhitelist(c.Hosts...),
	}

	settings := manager.TLSConfig()
	settings.MinVersion = c.minVersion()

	// HTTPHandler answers /.well-known/acme-challenge/ and sends everything
	// else to HTTPS. Calling it is also what enables HTTP-01, so a server
	// behind a firewall that only opens 443 still gets certificates through
	// TLS-ALPN-01 -- and one that opens 80 has both.
	return settings, manager.HTTPHandler(nil), nil
}

func (c *TLSConfig) minVersion() uint16 {
	if c.MinVersion != 0 {
		return c.MinVersion
	}
	return tls.VersionTLS12
}

// addr is where HTTPS listens.
func (c *TLSConfig) addr() string {
	if c.Addr != "" {
		return c.Addr
	}
	return DefaultTLSAddr
}

// redirectAddr is the plain-HTTP address, or "" for none.
func (c *TLSConfig) redirectAddr() string {
	switch {
	case c.RedirectFrom == "-":
		return ""
	case c.RedirectFrom != "":
		return c.RedirectFrom
	case len(c.Hosts) > 0:
		return ":80"
	default:
		return ""
	}
}

// redirectHandler sends plain HTTP to HTTPS.
//
// challenge is autocert's, when there is one. It has to come first: redirecting
// /.well-known/acme-challenge/ to HTTPS makes the certificate that would let
// the redirect work a prerequisite for obtaining it.
func redirectHandler(challenge http.Handler) http.Handler {
	if challenge != nil {
		return challenge
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}

		target := "https://" + host + r.URL.RequestURI()

		// 308 rather than 301: a permanent redirect that preserves the method,
		// so a POST to http:// arrives as a POST rather than silently becoming
		// a GET and losing its body.
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}

// serveRedirect runs the plain-HTTP listener.
func (g *Tjo) serveRedirect(addr string, challenge http.Handler, failed chan<- error) *http.Server {
	srv := &http.Server{
		Addr:              addr,
		Handler:           redirectHandler(challenge),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          g.Logging.Error,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Not fatal to the HTTPS listener. A port 80 already in use means
			// no HTTP-01 challenges and no redirect, and TLS-ALPN-01 still
			// works -- so this is worth saying loudly and not worth exiting
			// over.
			failed <- err
		}
	}()

	return srv
}

// acmeALPN is exported for a test that asserts the ALPN challenge protocol is
// advertised, since that is what makes certificates obtainable without port 80.
var acmeALPN = acme.ALPNProto
