package traecn

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBuildAuthorizeURL(t *testing.T) {
	fp := NewDeviceFingerprint()
	url := BuildAuthorizeURL(fp, 8021)
	for _, want := range []string{
		"https://www.trae.com.cn/authorization?",
		"client_id=" + ClientID,
		"x_device_id=" + fp.DeviceID,
		"x_machine_id=" + fp.MachineID,
		"redirect_uri=http%3A%2F%2F127.0.0.1%3A8021%2Fauthorize",
	} {
		if !strings.Contains(url, want) {
			t.Fatalf("authorize URL missing %q: %s", want, url)
		}
	}
}

func TestParseCallbackURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantKey string
		wantVal string
		wantErr bool
	}{
		{name: "query token", raw: "http://127.0.0.1:8021/authorize?token=jwt-abc&client_id=c1", wantKey: "token", wantVal: "jwt-abc"},
		{name: "refresh token", raw: "http://127.0.0.1:8021/authorize?refresh_token=rt-1", wantKey: "refresh_token", wantVal: "rt-1"},
		{name: "fragment params", raw: "https://www.trae.com.cn/login-success#token=jwt-frag", wantKey: "token", wantVal: "jwt-frag"},
		{name: "garbage", raw: "not a url at all %%", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := ParseCallbackURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCallbackURL: %v", err)
			}
			if got := params[tt.wantKey]; got != tt.wantVal {
				t.Fatalf("params[%q] = %q, want %q", tt.wantKey, got, tt.wantVal)
			}
		})
	}
}

func TestParamKeys(t *testing.T) {
	params := map[string]string{"z": "1", "a": "2", "m": "3"}
	keys := ParamKeys(params)
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "m" || keys[2] != "z" {
		t.Fatalf("ParamKeys = %v, want sorted [a m z]", keys)
	}
}

func TestTokenStorageSaveAndType(t *testing.T) {
	dir := t.TempDir()
	storage := &TraeCNTokenStorage{
		Token:        "jwt-1",
		RefreshToken: "rt-1",
		Email:        "user@example.com",
		DeviceID:     "dev-1",
		MachineID:    "mach-1",
		ExpireTime:   1893456000000,
	}
	path := dir + "/trae-cn-test.json"
	if err := storage.SaveTokenToFile(path); err != nil {
		t.Fatalf("SaveTokenToFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var decoded TraeCNTokenStorage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Type != "trae-cn" {
		t.Fatalf("Type = %q, want trae-cn", decoded.Type)
	}
	if decoded.Token != "jwt-1" || decoded.DeviceID != "dev-1" {
		t.Fatalf("decoded storage mismatch: %+v", decoded)
	}
}
