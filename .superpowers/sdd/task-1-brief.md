# Task 1 Brief: `internal/auth/traecn/` 认证核心包

**Files:**
- Create: `internal/auth/traecn/endpoints.go`
- Create: `internal/auth/traecn/trae_token.go`
- Create: `internal/auth/traecn/trae_auth.go`
- Create: `internal/auth/traecn/oauth_server.go`
- Test: `internal/auth/traecn/trae_auth_test.go`

模板参考（写代码前先读）：`internal/auth/qoder/qoder_token.go`（SaveTokenToFile 的 temp+atomic rename 模式）、`internal/auth/iflow/oauth_server.go`（完整 OAuthServer 模式）。

## Step 1: 先写失败测试 `internal/auth/traecn/trae_auth_test.go`

```go
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
```

Run: `go test ./internal/auth/traecn/` — Expected: FAIL（包不存在）

## Step 2: `endpoints.go`

常量（注释仅英文）：
- `ClientID = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"` — Trae IDE public client identifier (no secret; ClientSecret is "-")
- `DefaultCallbackPort = 8021`
- `AuthorizeURL = "https://www.trae.com.cn/authorization"`
- `AuthBase = "https://www.trae.com.cn"`
- `APIBase = "https://api-cn-central.trae.com.cn"`（注释 `// CN ExchangeToken node; verify via packet capture`）
- `ModelAPIBase = "https://trae-api-cn.mchost.guru"`
- 派生：`GetRefreshTokenURL = AuthBase + "/cloudide/api/v3/trae/oauth/GetRefreshToken"`、`ExchangeTokenURL = APIBase + "/cloudide/api/v3/trae/oauth/ExchangeToken"`、`ModelListURL = ModelAPIBase + "/api/ide/v1/model_list"`、`ChatURL = ModelAPIBase + "/api/ide/v1/chat"`

## Step 3: `trae_token.go`

```go
package traecn

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
```

三个方法（镜像 `internal/auth/qoder/qoder_token.go` 模式）：
- `SaveTokenToFile(path string) error`：Type 空时设 "trae-cn"；marshal indent → 写 temp 文件 → atomic rename
- `SetMetadata()`：把 Email/UserID/ExpireTime（从 ms 转 RFC3339）写入 `Metadata` map（为 nil 先初始化）
- `IsExpired() bool`：`ExpireTime > 0 && time.Now().UnixMilli() >= ExpireTime`

## Step 4: `trae_auth.go`

```go
package traecn

// DeviceFingerprint holds the x-* header values Trae expects on every call.
type DeviceFingerprint struct {
	DeviceID       string
	MachineID      string
	DeviceBrand    string
	DeviceCPU      string
	DeviceType     string
	OSVersion      string
	IDEVersion     string
	IDEVersionCode string
	IDEVersionType string
}

func NewDeviceFingerprint() DeviceFingerprint {
	return DeviceFingerprint{
		DeviceID:       uuid.NewString(),
		MachineID:      uuid.NewString(),
		DeviceBrand:    "Microsoft",
		DeviceCPU:      "x86_64",
		DeviceType:     "Windows",
		OSVersion:      "10.0.19045",
		IDEVersion:     "2.0.0",
		IDEVersionCode: "20000",
		IDEVersionType: "stable",
	}
}
```

（uuid 用项目现有依赖，先查 `go.mod`/grep "uuid" 确认 import 路径；项目已有 `github.com/google/uuid`。）

再实现：
- `BuildAuthorizeURL(fp DeviceFingerprint, callbackPort int) string`：`AuthorizeURL` + query `client_id`、`x_device_id`、`x_machine_id`、`x_device_brand`、`x_device_type`、`x_os_version`、`redirect_uri=http://127.0.0.1:<port>/authorize`（`url.Values` 编码）
- `type TokenData struct { AccessToken, RefreshToken string; ExpiresIn int64; UserID string }`
- `type TraeCNAuth struct{ httpClient *http.Client }`；`NewTraeCNAuth(cfg *config.Config) *TraeCNAuth` — cfg nil 时用 `http.DefaultClient`（便于测试）；cfg 非 nil 时镜像 qoder 的 client 构造（项目里看 `internal/auth/qoder` 如何构造 http client/代理）
- `ExchangeToken(ctx context.Context, clientID, refreshToken string) (*TokenData, error)`：POST `ExchangeTokenURL`，JSON body `{"ClientID":clientID,"RefreshToken":refreshToken,"ClientSecret":"-","UserID":""}`；解析 `Result.Token/RefreshToken/ExpiresIn/UserID`；token 为空 → `fmt.Errorf("trae-cn: exchange returned empty token (endpoint may have changed, check endpoints.go)")`
- `ParseCallbackURL(raw string) (map[string]string, error)`：`url.Parse` 后合并 `Query()` 与 fragment（fragment 若是 query 形式再 ParseQuery）的所有参数；无参数 → error
- `CreateTokenStorage(td *TokenData, fp DeviceFingerprint, email string, expireMs int64) *TraeCNTokenStorage`：组装 storage；`LastRefresh = time.Now().Format(time.RFC3339)`；调 `SetMetadata()`

注意：测试里 `BuildAuthorizeURL` 和 `ParseCallbackURL` 是包级函数（不依赖 TraeCNAuth 实例），按测试签名实现。

## Step 5: `oauth_server.go`

镜像 `internal/auth/iflow/oauth_server.go`，改动仅：
- 回调路径 `/authorize`
- `OAuthResult struct { Params map[string]string; Error error }`（Trae 回调参数不确定是 code 还是 token，把整包 query 参数传回）
- 成功页 HTML 内联："Login Successful. You can close this window and return to the terminal."

## Step 6: 跑测试验证

Run: `gofmt -w internal/auth/traecn && go test ./internal/auth/traecn/ -v`
Expected: PASS

## Step 7: Commit

**不要执行任何 git 命令。** 实现完成后，在报告里给出以下 git 命令文本供用户人工执行：

```bash
git add internal/auth/traecn/
git commit -m "feat(auth): add trae-cn auth core package (endpoints, token storage, oauth server)"
```
