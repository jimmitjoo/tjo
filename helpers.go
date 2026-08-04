package tjo

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

const (
	randomString = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+"
)

func (g Tjo) RandomString(length int) string {
	s, r := make([]rune, length), []rune(randomString)
	
	for i := range s {
		// Use crypto/rand for secure random number generation
		b := make([]byte, 1)
		for {
			_, err := rand.Read(b)
			if err != nil {
				// In case of error, try again
				continue
			}
			// Use modulo only if within valid range to avoid bias
			if int(b[0]) < 256-(256%len(r)) {
				s[i] = r[int(b[0])%len(r)]
				break
			}
		}
	}

	return string(s)
}

func (g Tjo) CreateDirIfNotExists(path string) error {
	const mode = 0755
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.Mkdir(path, mode)

		if err != nil {
			return err
		}
	}

	return nil
}

func (g Tjo) CreateFileIfNotExists(path string) error {
	var _, err = os.Stat(path)

	if os.IsNotExist(err) {
		var file, err = os.Create(path)
		if err != nil {
			return err
		}

		defer func(file *os.File) {
			_ = file.Close()
		}(file)
	}

	return nil
}

type Encryption struct {
	Key []byte
}

// ValidateEncryptionKey checks that the key meets AES requirements.
// AES requires keys of exactly 16, 24, or 32 bytes for AES-128, AES-192, or AES-256.
func ValidateEncryptionKey(key []byte) error {
	if len(key) == 0 {
		return errors.New("encryption key cannot be empty")
	}
	switch len(key) {
	case 16, 24, 32:
		return nil
	default:
		return fmt.Errorf("encryption key must be 16, 24, or 32 bytes for AES (got %d bytes)", len(key))
	}
}

// NewEncryption creates a validated encryption instance.
// Returns an error if the key doesn't meet AES requirements.
func NewEncryption(key []byte) (*Encryption, error) {
	if err := ValidateEncryptionKey(key); err != nil {
		return nil, err
	}
	return &Encryption{Key: key}, nil
}

// Encrypt returns an authenticated ciphertext (AES-GCM) as a URL-safe base64
// string. The nonce is prepended to the sealed output.
//
// GCM rather than an unauthenticated mode: without a MAC an attacker can flip
// bits in the ciphertext and have them land as controlled changes in the
// plaintext, and Decrypt has no way to notice. Anything treating a decrypted
// value as trustworthy is then forgeable.
func (e Encryption) Encrypt(data string) (string, error) {
	if err := ValidateEncryptionKey(e.Key); err != nil {
		return "", err
	}

	block, err := aes.NewCipher(e.Key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	sealed := gcm.Seal(nonce, nonce, []byte(data), nil)

	return base64.URLEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. It returns an error if the ciphertext was modified
// in any way, rather than silently returning altered plaintext.
func (e Encryption) Decrypt(cryptoText string) (string, error) {
	if err := ValidateEncryptionKey(e.Key); err != nil {
		return "", err
	}

	cipherText, err := base64.URLEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(e.Key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(cipherText) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}

	nonce, sealed := cipherText[:gcm.NonceSize()], cipherText[gcm.NonceSize():]

	plainText, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", errors.New("ciphertext failed authentication")
	}

	return string(plainText), nil
}
