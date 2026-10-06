package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// PKCE holds a verifier/challenge pair for an authorization-code flow (S256),
// matching omp's generatePKCE (96 random bytes, base64url, SHA-256 challenge).
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE generates a fresh S256 PKCE pair.
func NewPKCE() (PKCE, error) {
	raw := make([]byte, 96)
	if _, err := rand.Read(raw); err != nil {
		return PKCE{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCE{Verifier: verifier, Challenge: challenge}, nil
}

// randomState returns a hex CSRF state token (16 bytes, matching omp's default).
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out), nil
}
