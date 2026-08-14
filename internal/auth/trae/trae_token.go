// Package trae provides authentication and token management for ByteDance's Trae IDE.
package trae

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
)

// TokenData holds the raw OAuth token response from Trae.
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Email        string    `json:"email,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	Type         string    `json:"type"`
}

// TokenStorage stores OAuth2 token information for Trae API authentication.
type TokenStorage struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Email        string `json:"email,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Type         string `json:"type"`

	// Metadata holds arbitrary key-value pairs injected via hooks.
	Metadata map[string]any `json:"-"`
}

// SetMetadata allows external callers to inject metadata into the storage before saving.
func (ts *TokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

// SaveTokenToFile serializes the Trae token storage to a JSON file.
func (ts *TokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	ts.Type = "trae"

	if err := os.MkdirAll(filepath.Dir(authFilePath), 0o700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	f, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("failed to create token file: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, errMerge := misc.MergeMetadata(ts, ts.Metadata)
	if errMerge != nil {
		return fmt.Errorf("failed to merge metadata: %w", errMerge)
	}

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return nil
}

// LoadTokenFromFile loads a token from a JSON file.
func LoadTokenFromFile(path string) (*TokenStorage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ts TokenStorage
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, err
	}
	return &ts, nil
}

// IsExpired checks if the access token is expired (with 5-minute buffer).
func (ts *TokenStorage) IsExpired() bool {
	if ts.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, ts.ExpiresAt)
	if err != nil {
		return true
	}
	return time.Now().Add(5 * time.Minute).After(t)
}

// NeedsRefresh checks if the token should be refreshed.
func (ts *TokenStorage) NeedsRefresh() bool {
	if ts.RefreshToken == "" {
		return false
	}
	return ts.IsExpired()
}

// CredentialFileName returns the standard filename for Trae credential storage.
func CredentialFileName(email string) string {
	if email == "" {
		return ".trae-token.json"
	}
	return ".trae-" + email + ".json"
}
