package traecn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// TraeCNTokenStorage persists the Trae CN IDE token plus the device
// fingerprint captured at login. All upstream calls reuse these headers.
type TraeCNTokenStorage struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	Email        string `json:"email"`
	Name         string `json:"name,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	ExpireTime   int64  `json:"expire_time"` // ms epoch
	LastRefresh  string `json:"last_refresh"`
	Type         string `json:"type"`

	DeviceID       string `json:"x_device_id"`
	MachineID      string `json:"x_machine_id"`
	DeviceBrand    string `json:"x_device_brand,omitempty"`
	DeviceCPU      string `json:"x_device_cpu,omitempty"`
	DeviceType     string `json:"x_device_type,omitempty"`
	OSVersion      string `json:"x_os_version,omitempty"`
	IDEVersion     string `json:"x_ide_version,omitempty"`
	IDEVersionCode string `json:"x_ide_version_code,omitempty"`
	IDEVersionType string `json:"x_ide_version_type,omitempty"`

	Metadata map[string]any `json:"-"`
}

// SaveTokenToFile writes the token storage to a JSON file atomically.
func (s *TraeCNTokenStorage) SaveTokenToFile(path string) error {
	if s.Type == "" {
		s.Type = "trae-cn"
	}
	s.SetMetadata()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SetMetadata populates the Metadata map with computed fields.
// This is called before saving so that Auth.ExpirationTime() can
// read the expires_at value via the standard sdk/cliproxy/auth path.
func (s *TraeCNTokenStorage) SetMetadata() {
	if s.Metadata == nil {
		s.Metadata = make(map[string]any)
	}
	s.Metadata["email"] = s.Email
	s.Metadata["user_id"] = s.UserID
	if s.Name != "" {
		s.Metadata["name"] = s.Name
	}
	if s.ExpireTime > 0 {
		s.Metadata["expires_at"] = time.UnixMilli(s.ExpireTime).UTC().Format(time.RFC3339)
	}
}

// IsExpired returns true when the token has passed its expiry time.
func (s *TraeCNTokenStorage) IsExpired() bool {
	return s.ExpireTime > 0 && time.Now().UnixMilli() >= s.ExpireTime
}
