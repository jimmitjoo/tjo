package urlsigner

import (
	"strings"
	"time"

	goalone "github.com/bwmarrin/go-alone"
)

type Signer struct {
	Secret []byte
}

func (s *Signer) GenerateTokenFromString(data string) string {
	var urlToSign string

	crypt := goalone.New(s.Secret, goalone.Timestamp)

	if strings.Contains(data, "?") {
		urlToSign = data + "&hash="
	} else {
		urlToSign = data + "?hash="
	}

	tokenBytes := crypt.Sign([]byte(urlToSign))
	token := string(tokenBytes)

	return token
}

func (s *Signer) VerifyToken(token string) bool {
	crypt := goalone.New(s.Secret, goalone.Timestamp)

	_, err := crypt.Unsign([]byte(token))
	if err != nil {
		return false
	}

	return true
}

// Expired reports whether the token's embedded timestamp is older than
// minutesUntilExpire.
//
// It does NOT verify the signature. A tampered token whose trailing timestamp
// section is still well-formed reports a perfectly ordinary age, so calling
// this on its own tells you nothing about authenticity. Always call VerifyToken
// first and act on its result:
//
//	if !signer.VerifyToken(url) { /* reject */ }
//	if signer.Expired(url, 60)  { /* reject */ }
//
// Input it cannot parse at all is reported as expired, so that path fails
// closed.
func (s *Signer) Expired(token string, minutesUntilExpire int) bool {
	crypt := goalone.New(s.Secret, goalone.Timestamp)
	ts := crypt.Parse([]byte(token))

	return time.Since(ts.Timestamp) > time.Duration(minutesUntilExpire)*time.Minute
}
