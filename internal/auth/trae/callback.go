package trae

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ParseCallbackFromURL normalizes a Trae `/authorize` callback URL into a
// CallbackParams. It is the single source of truth shared by both code
// paths: the HTTP callback server (handleCallback) and the CLI manual
// paste fallback. This guarantees consistent handling of:
//
//   - flat authCode= / refreshToken= / authCodeInfo=<JSON>
//   - "host" as an alias for "loginHost" (which Trae CN actually returns)
//   - userRegion / Region as LoginRegion
//   - query + fragment parameter merging
//   - error / error_description propagation
func ParseCallbackFromURL(u *url.URL) *CallbackParams {
	if u == nil {
		return nil
	}
	params := flattenCallbackParams(u)
	cb := &CallbackParams{RawQuery: params}

	errCode := pickAny(params,
		"error", "errorCode", "error_code", "err", "Err")
	if errCode != "" {
		cb.Error = errCode
		cb.ErrorDescription = pickAny(params,
			"error_description", "errorDesc", "errorDescription", "message", "Message")
		return cb
	}

	cb.AuthCode = ExtractAuthCode(params)
	cb.RefreshToken = pickAny(params,
		"refreshToken", "refresh_token", "RefreshToken", "refresh-token", "Refresh_token")
	cb.LoginHost = pickAny(params,
		"loginHost", "login_host", "LoginHost", "Login_host",
		"host", "Host", "consoleHost", "console_host",
		"loginOrigin", "login_origin", "LoginOrigin")
	cb.LoginRegion = pickAny(params,
		"loginRegion", "login_region", "LoginRegion",
		"region", "Region",
		"userRegion", "user_region", "UserRegion",
		"AIRegion", "aiRegion", "ai_region",
		"storeRegion", "store_region", "StoreRegion")
	cb.LoginTraceID = pickAny(params,
		"loginTraceID", "loginTraceId", "login_trace_id", "traceId", "trace_id", "LoginTraceID")
	cb.CloudIDEToken = pickAny(params,
		"x-cloudide-token", "xCloudideToken", "X-Cloudide-Token",
		"accessToken", "access_token", "AccessToken", "Token", "token",
		"bearerToken", "bearer_token")
	cb.UserTag = pickAny(params, "userTag", "user_tag", "UserTag", "Usertag")
	return cb
}

// ExtractAuthCode returns the authorization code from a flattened callback
// parameter map. It handles the documented variants:
//
//  1. Direct flat parameters (authCode / auth_code / code).
//  2. Nested authCodeInfo JSON object — both key-value and base64-wrapped.
func ExtractAuthCode(params map[string]string) string {
	if direct := pickAny(params,
		"authCode", "auth_code", "AuthCode", "Authorization_Code",
		"authorization_code", "code", "Code"); direct != "" {
		return direct
	}

	raw := pickAny(params, "authCodeInfo", "AuthCodeInfo", "auth_code_info", "Auth_code_info",
		"authCodeData", "auth_code_data", "codeInfo", "code_info")
	if raw == "" {
		return ""
	}

	// Variant A: standard JSON object with AuthCode / Result.AuthCode
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		if m, ok := parsed.(map[string]any); ok {
			paths := [][]string{
				{"AuthCode"}, {"authCode"}, {"auth_code"}, {"Code"}, {"code"},
				{"Result", "AuthCode"}, {"result", "authCode"}, {"Result", "Code"},
				{"Data", "AuthCode"}, {"data", "authCode"},
				{"Payload", "AuthCode"}, {"payload", "authCode"},
			}
			for _, p := range paths {
				if s, ok := digAnyString(m, p); ok && s != "" {
					return s
				}
			}
		}
	}

	// Variant B: legacy "key1=val1&key2=val2" pseudo form string inside
	// the authCodeInfo value (some older code paths return this).
	if strings.Contains(raw, "AuthCode=") || strings.Contains(raw, "authCode=") {
		if vals, err := url.ParseQuery(raw); err == nil {
			if s := strings.TrimSpace(vals.Get("AuthCode")); s != "" {
				return s
			}
			if s := strings.TrimSpace(vals.Get("authCode")); s != "" {
				return s
			}
		}
	}
	return ""
}

// ParseUserInfoFromCallback extracts a UserInfoResponse from the callback
// parameters when available. Trae CN's actual redirect includes a full
// userInfo JSON object; consuming it directly lets the authenticator
// skip the secondary GetUserInfo RPC entirely (avoids flaky auth
// failures when GetUserInfo needs additional permissions).
func ParseUserInfoFromCallback(cb *CallbackParams) *UserInfoResponse {
	out := &UserInfoResponse{}
	if cb == nil || cb.RawQuery == nil {
		return out
	}
	raw := pickAny(cb.RawQuery, "userInfo", "user_info", "UserInfo", "profile", "Profile")
	if raw == "" {
		return out
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return out
	}
	// Each call passes a single-element path (since digAnyString walks a
	// nested path). Using pickStringFrom alternatives keeps the semantics
	// of "try these top-level keys in order".
	out.UserID = pickStringFrom(m, "UserID", "user_id", "userId", "Uid", "uid", "id", "Id", "ID")
	out.Nickname = pickStringFrom(m,
		"ScreenName", "screen_name", "screenName",
		"Nickname", "nickname", "nick_name",
		"Name", "name",
		"UserName", "userName", "username",
		"DisplayName", "displayName", "display_name")
	out.Email = pickStringFrom(m, "Email", "email", "emailAddress", "EmailAddress", "email_address")

	// CN users often only get NonPlainTextMobile (masked phone) back.
	// Promote it to Email so downstream still has a human label.
	if out.Email == "" {
		if mobile := pickStringFrom(m, "NonPlainTextMobile", "NonPlainTextPhone", "Mobile", "mobile", "Phone", "phone", "PhoneNumber", "phoneNumber"); mobile != "" {
			out.Email = mobile
		}
	}
	// Fallback: NonPlainTextEmail if Email was empty but there is one.
	if out.Email == "" {
		if masked := pickStringFrom(m, "NonPlainTextEmail"); masked != "" {
			out.Email = masked
		}
	}
	return out
}

// pickStringFrom returns the first non-empty string value from the map at
// any of the candidate top-level keys (case-insensitive fallback if no
// exact match exists).
func pickStringFrom(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	// Case-insensitive scan as a fallback.
	for _, k := range keys {
		for mk, mv := range m {
			if strings.EqualFold(mk, k) {
				if s, ok := mv.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						return s
					}
				}
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Internal helpers (kept in one place for consistency)
// ---------------------------------------------------------------------------

// flattenCallbackParams merges URL query and fragment into a single map.
// Query keys take precedence over fragment keys (cockpit-tools behavior).
func flattenCallbackParams(u *url.URL) map[string]string {
	out := make(map[string]string)
	for k, vs := range u.Query() {
		if len(vs) > 0 && strings.TrimSpace(vs[0]) != "" {
			out[k] = strings.TrimSpace(vs[0])
		}
	}
	if u.Fragment == "" {
		return out
	}
	rawFrag := strings.TrimLeft(u.Fragment, "#?")
	// Fragments can be either "?k=v..." or "k=v..."
	if vals, err := url.ParseQuery(rawFrag); err == nil {
		for k, vs := range vals {
			if len(vs) > 0 && strings.TrimSpace(vs[0]) != "" {
				if _, exists := out[k]; !exists {
					out[k] = strings.TrimSpace(vs[0])
				}
			}
		}
	}
	return out
}

// pickAny returns the first non-empty value under any of the provided keys.
func pickAny(params map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := params[k]; ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// digAnyString walks nested maps and returns the first string-typed value
// at the end of the path (also accepts number→string and bool→string).
func digAnyString(root map[string]any, path []string) (string, bool) {
	if len(path) == 0 {
		return "", false
	}
	var cur any = root
	for i, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		next, exists := m[p]
		if !exists {
			// Case-insensitive fallback for keys we may have missed in the
			// explicit path list.
			if !exists {
				for mk, mv := range m {
					if strings.EqualFold(mk, p) {
						next = mv
						exists = true
						break
					}
				}
			}
			if !exists {
				return "", false
			}
		}
		if i == len(path)-1 {
			switch v := next.(type) {
			case string:
				s := strings.TrimSpace(v)
				return s, s != ""
			case bool:
				if v {
					return "true", true
				}
				return "false", true
			case float64:
				return floatToString(v), true
			case int:
				return intToString(int64(v)), true
			case int64:
				return intToString(v), true
			case int32:
				return intToString(int64(v)), true
			case uint:
				return intToString(int64(v)), true
			case uint64:
				return intToString(int64(v)), true
			case json.Number:
				return v.String(), true
			}
			return "", false
		}
		cur = next
	}
	return "", false
}

func intToString(v int64) string     { return fmt.Sprintf("%d", v) }
func floatToString(v float64) string { return fmt.Sprintf("%d", int64(v)) }
