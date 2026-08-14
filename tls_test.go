package tjo

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCertificate puts a self-signed certificate and key on disk.
func writeCertificate(t *testing.T, host string) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certPEM, _ := os.Create(certFile)
	pem.Encode(certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	certPEM.Close()

	marshalled, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, _ := os.Create(keyFile)
	pem.Encode(keyPEM, &pem.Block{Type: "EC PRIVATE KEY", Bytes: marshalled})
	keyPEM.Close()

	return certFile, keyFile
}

// A certificate already on disk, serving HTTP/2 -- which is the point, because
// the sse package's requirement is met by TLS rather than by h2c.
func TestAManualCertificateServesHTTP2(t *testing.T) {
	certFile, keyFile := writeCertificate(t, "localhost")

	settings, challenge, err := (&TLSConfig{CertFile: certFile, KeyFile: keyFile}).tlsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if challenge != nil {
		t.Error("a manual certificate does not need an ACME challenge handler")
	}

	if settings.NextProtos[0] != "h2" {
		t.Errorf("NextProtos is %v, and without h2 first the browser gets HTTP/1.1 and the six-connection cap", settings.NextProtos)
	}
	if settings.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion is %x", settings.MinVersion)
	}

	// It really serves, and really negotiates HTTP/2.
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Proto))
	}))
	server.TLS = settings
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.Proto != "HTTP/2.0" {
		t.Errorf("negotiated %s, and SSE needs HTTP/2", response.Proto)
	}
}

// The one that stops a stranger burning somebody's rate limit.
//
// autocert's own default host policy permits every hostname, so a manager
// without one attempts issuance for anything pointed at the server. This
// asserts the policy is set and refuses, without going anywhere near an ACME
// server.
func TestAnUnlistedHostnameDoesNotTriggerIssuance(t *testing.T) {
	settings, _, err := (&TLSConfig{
		Hosts:    []string{"example.com"},
		CacheDir: t.TempDir(),
	}).tlsConfig()
	if err != nil {
		t.Fatal(err)
	}

	// GetCertificate is what a TLS handshake calls, and it consults the host
	// policy before it does anything else. A refusal here is a refusal to
	// start an ACME order.
	_, err = settings.GetCertificate(&tls.ClientHelloInfo{
		ServerName: "someone-elses-domain.example",
	})

	if err == nil {
		t.Fatal("an unlisted hostname was accepted, so pointing any domain at this server starts an ACME order")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// And the policy discriminates rather than refusing everything, which is what
// makes the refusal above meaningful.
//
// Asserted through the challenge handler, which consults the host policy and
// then looks for a token -- both without a network call, so the test says
// nothing to an ACME server.
func TestTheHostPolicyDiscriminates(t *testing.T) {
	_, challenge, err := (&TLSConfig{
		Hosts:    []string{"example.com"},
		CacheDir: t.TempDir(),
	}).tlsConfig()
	if err != nil {
		t.Fatal(err)
	}

	const path = "/.well-known/acme-challenge/token"

	unlisted := httptest.NewRecorder()
	challenge.ServeHTTP(unlisted, httptest.NewRequest("GET", "http://elsewhere.example"+path, nil))

	if unlisted.Code != http.StatusForbidden {
		t.Errorf("an unlisted host answered %d, want 403", unlisted.Code)
	}

	listed := httptest.NewRecorder()
	challenge.ServeHTTP(listed, httptest.NewRequest("GET", "http://example.com"+path, nil))

	// 404: past the host policy, and there is no such token because no order
	// was ever started. Anything but 403 is the point.
	if listed.Code == http.StatusForbidden {
		t.Error("a configured host was refused by the host policy")
	}
	if listed.Code != http.StatusNotFound {
		t.Errorf("a configured host answered %d, want 404 for a token that does not exist", listed.Code)
	}
}

// TLS-ALPN-01 is what makes certificates obtainable when only 443 is open, so
// the protocol has to be advertised.
func TestTheALPNChallengeProtocolIsAdvertised(t *testing.T) {
	settings, _, err := (&TLSConfig{Hosts: []string{"example.com"}, CacheDir: t.TempDir()}).tlsConfig()
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, proto := range settings.NextProtos {
		if proto == acmeALPN {
			found = true
		}
	}
	if !found {
		t.Errorf("NextProtos is %v, without %s, so a server with only 443 open cannot get a certificate",
			settings.NextProtos, acmeALPN)
	}
	if settings.NextProtos[0] != "h2" {
		t.Errorf("NextProtos is %v, and h2 has to be first", settings.NextProtos)
	}
}

// Redirecting the challenge path to HTTPS makes the certificate a prerequisite
// for obtaining the certificate.
func TestTheACMEChallengePathIsNotRedirected(t *testing.T) {
	_, challenge, err := (&TLSConfig{Hosts: []string{"example.com"}, CacheDir: t.TempDir()}).tlsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if challenge == nil {
		t.Fatal("automatic certificates come with no challenge handler")
	}

	handler := redirectHandler(challenge)

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://example.com/.well-known/acme-challenge/token", nil)
	handler.ServeHTTP(rec, r)

	if rec.Code >= 300 && rec.Code < 400 {
		t.Fatalf("the challenge path is redirected (%d to %q), so no certificate can ever be issued",
			rec.Code, rec.Header().Get("Location"))
	}

	// Everything else does go to HTTPS.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/login", nil))

	if rec.Code < 300 || rec.Code >= 400 {
		t.Fatalf("a plain-HTTP request answered %d rather than redirecting", rec.Code)
	}
	if location := rec.Header().Get("Location"); !strings.HasPrefix(location, "https://") {
		t.Errorf("redirected to %q", location)
	}
}

// A POST redirected as a 301 arrives as a GET with no body, which loses a form
// submission and is very hard to see.
func TestTheRedirectPreservesTheMethod(t *testing.T) {
	rec := httptest.NewRecorder()
	redirectHandler(nil).ServeHTTP(rec, httptest.NewRequest("POST", "http://example.com/pay?id=1", nil))

	if rec.Code != http.StatusPermanentRedirect {
		t.Errorf("%d, want 308 -- 301 turns a POST into a GET", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://example.com/pay?id=1" {
		t.Errorf("redirected to %q", got)
	}
}

// Configurations that cannot work have to fail at start-up, where somebody is
// watching, rather than at the first handshake.
func TestUnusableTLSConfigurationsAreRefused(t *testing.T) {
	certFile, keyFile := writeCertificate(t, "localhost")

	cases := map[string]*TLSConfig{
		"nothing at all":       {},
		"a cert with no key":   {CertFile: certFile},
		"a key with no cert":   {KeyFile: keyFile},
		"both sources at once": {CertFile: certFile, KeyFile: keyFile, Hosts: []string{"example.com"}},
		"no cache directory":   {Hosts: []string{"example.com"}},
		"a url as a hostname":  {Hosts: []string{"https://example.com"}, CacheDir: t.TempDir()},
		"an empty hostname":    {Hosts: []string{""}, CacheDir: t.TempDir()},
		"a missing cert file":  {CertFile: filepath.Join(t.TempDir(), "nope.pem"), KeyFile: keyFile},
	}

	for name, cfg := range cases {
		if _, _, err := cfg.tlsConfig(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Nothing happens without a TLSConfig, which is what keeps every deployment
// that terminates upstream exactly as it was.
func TestNoTLSConfigMeansNoTLS(t *testing.T) {
	var cfg *TLSConfig

	settings, challenge, err := cfg.tlsConfig()
	if err != nil || settings != nil || challenge != nil {
		t.Fatalf("a nil TLSConfig produced %v, %v, %v", settings, challenge, err)
	}
}

// The plain-HTTP listener defaults to :80 for autocert, because HTTP-01
// challenges arrive there -- and to nothing for a manual certificate, where
// there is no challenge to answer and a redirect is the application's choice.
func TestTheRedirectListenerDefaults(t *testing.T) {
	cases := []struct {
		cfg  TLSConfig
		want string
	}{
		{TLSConfig{Hosts: []string{"example.com"}}, ":80"},
		{TLSConfig{Hosts: []string{"example.com"}, RedirectFrom: "-"}, ""},
		{TLSConfig{Hosts: []string{"example.com"}, RedirectFrom: ":8080"}, ":8080"},
		{TLSConfig{CertFile: "a", KeyFile: "b"}, ""},
		{TLSConfig{CertFile: "a", KeyFile: "b", RedirectFrom: ":80"}, ":80"},
	}

	for _, c := range cases {
		if got := c.cfg.redirectAddr(); got != c.want {
			t.Errorf("%+v -> %q, want %q", c.cfg, got, c.want)
		}
	}
}

// The certificate cache holds the ACME account key and every private key.
func TestTheCertificateCacheIsNotWorldReadable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certs")

	if _, _, err := (&TLSConfig{Hosts: []string{"example.com"}, CacheDir: dir}).tlsConfig(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("the certificate cache is %o", mode)
	}
}
