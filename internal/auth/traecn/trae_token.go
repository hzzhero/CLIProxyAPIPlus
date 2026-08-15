package traecn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
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

// SaveTokenToFile serializes the token storage to a JSON file. It writes to a
// temp file first and atomically renames onto the target path so the file
// watcher never observes a partially-written credentials file.
func (ts *TraeCNTokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	if strings.TrimSpace(ts.Type) == "" {
		ts.Type = "trae-cn"
	}

	if err := os.MkdirAll(filepath.Dir(authFilePath), 0700); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	data, errMerge := misc.MergeMetadata(ts, ts.Metadata)
	if errMerge != nil {
		return fmt.Errorf("failed to merge metadata: %w", errMerge)
	}

	tmp, err := os.CreateTemp(filepath.Dir(authFilePath), ".tmp-traecn-*")
	if err != nil {
		return fmt.Errorf("failed to create temp token file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err = json.NewEncoder(tmp).Encode(data); err != nil {
		return fmt.Errorf("failed to write token to temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err = os.Rename(tmpName, authFilePath); err != nil {
		return fmt.Errorf("failed to commit token file: %w", err)
	}
	cleanup = false // rename succeeded, don't remove
	return nil
}

// SetMetadata copies Email/UserID/ExpireTime (ms epoch converted to RFC3339)
// into the Metadata map so management listing can read them without decoding
// the full storage struct.
func (ts *TraeCNTokenStorage) SetMetadata() {
	if ts.Metadata == nil {
		ts.Metadata = make(map[string]any)
	}
	ts.Metadata["email"] = ts.Email
	ts.Metadata["user_id"] = ts.UserID
	if ts.ExpireTime > 0 {
		ts.Metadata["expire_time"] = time.UnixMilli(ts.ExpireTime).Format(time.RFC3339)
	}
}

// IsExpired reports whether the access token has already expired.
func (ts *TraeCNTokenStorage) IsExpired() bool {
	if ts.ExpireTime <= 0 {
		return false
	}
	return time.Now().UnixMilli() >= ts.ExpireTime
}
