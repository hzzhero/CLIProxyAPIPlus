package trae

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCECodes holds a generated PKCE pair matching the Trae server's
// expectations. Trae uses a 48-byte random verifier URL-safe-base64
// encoded without padding, and a SHA256 code challenge encoded the
// same way (method S256).
type PKCECodes struct {
	CodeVerifier  string
	CodeChallenge string
}

// GeneratePKCECodes generates a new PKCE pair.
func GeneratePKCECodes() (*PKCECodes, error) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("trae pkce: failed to generate code verifier: %w", err)
	}
	challenge := generateCodeChallenge(verifier)
	return &PKCECodes{CodeVerifier: verifier, CodeChallenge: challenge}, nil
}

// generateCodeVerifier creates a 48-byte random verifier encoded with
// URL-safe base64 without padding. The result is always 64 characters,
// which matches the cockpit-tools Rust implementation.
func generateCodeVerifier() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto/rand read failed: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// generateCodeChallenge returns SHA256(verifier) encoded as URL-safe
// base64 without padding.
func generateCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
