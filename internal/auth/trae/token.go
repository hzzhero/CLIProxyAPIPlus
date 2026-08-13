// Package trae provides authentication and token management functionality
// for Trae CN AI services. It handles OAuth token storage, serialization,
// and retrieval for maintaining authenticated sessions with the Trae SOLO agent API.
package trae

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
)

// TraeTokenStorage stores OAuth token information for Trae CN API authentication.
type TraeTokenStorage struct {
	// Token is the Cloud-IDE-JWT used for authenticating API requests.
	Token string `json:"token"`
	// RefreshToken is the OAuth refresh token used to obtain new JWTs.
	RefreshToken string `json:"refresh_token"`
	// UserID is the user ID associated with this token.
	UserID string `json:"user_id"`
	// TenantID is the tenant ID from the OAuth response.
	TenantID string `json:"tenant_id,omitempty"`
	// ClientID is the OAuth client ID used during login.
	ClientID string `json:"client_id"`
	// TokenExpireAt is when the JWT expires.
	TokenExpireAt time.Time `json:"token_expire_at"`
	// RefreshExpireAt is when the refresh token expires.
	RefreshExpireAt time.Time `json:"refresh_expire_at,omitempty"`
	// Type indicates the authentication provider type, always "trae-cn".
	Type string `json:"type"`

	// Provider-specific data for common_params in SOLO API requests.
	WebID        string `json:"web_id,omitempty"`
	BizUserID    string `json:"biz_user_id,omitempty"`
	UserUniqueID string `json:"user_unique_id,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Tenant       string `json:"tenant,omitempty"`
	Region       string `json:"region,omitempty"`
	AIRegion     string `json:"ai_region,omitempty"`
	UserIdentity string `json:"user_identity,omitempty"`
}

// SaveTokenToFile serializes the Trae token storage to a JSON file.
func (s *TraeTokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	s.Type = "trae-cn"
	if err := os.MkdirAll(filepath.Dir(authFilePath), 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	f, err := os.OpenFile(authFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create token file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	if err = json.NewEncoder(f).Encode(s); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return nil
}
