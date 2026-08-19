package trae

import (
	"net/url"
	"strings"
	"testing"
)

// realCallbackURL mirrors the URL the user pasted into the CLI.
// Key fields: authCodeInfo (JSON-wrapped AuthCode), host= (NOT loginHost=),
// userRegion=cn, userInfo= (full profile JSON).
const realCallbackURL = "http://127.0.0.1:44887/authorize?isRedirect=true&scope=trae&authCodeInfo=%7B%22AuthCode%22%3A%22544huVtGDCAtHVdMQ6dfKVriMd7StziTmqgQjX3wkOQ%22%2C%22ExpireAt%22%3A1787119217162%2C%22ExpireDuration%22%3A600000%7D&loginTraceID=03273b22805668476f4ade03ef46c5b9&host=https%3A%2F%2Fapi.trae.com.cn&userRegion=cn&userInfo=%7B%22AIRegion%22%3A%22CN%22%2C%22AvatarUrl%22%3A%22https%3A%2F%2Fp9-passport.byteacctimg.com%2Fimg%2Fuser-avatar%2Fassets%2F66549570146f542f030d63b85a8a009f_192_192.png%7E128x128.image%22%2C%22Description%22%3A%22%22%2C%22Gender%22%3A%220%22%2C%22LastLoginTime%22%3A%222026-08-19T13%3A50%3A15%2B08%3A00%22%2C%22LastLoginType%22%3A%22sms%22%2C%22MigrateToSG%22%3Afalse%2C%22NonPlainTextEmail%22%3A%22%22%2C%22NonPlainTextMobile%22%3A%22188******03%22%2C%22Region%22%3A%22CN%22%2C%22RegisterTime%22%3A%222025-09-06T15%3A54%3A08.357%2B08%3A00%22%2C%22ScreenName%22%3A%22%E7%94%A8%E6%88%B737508727791%22%2C%22TenantID%22%3A%227o2d894p7dr0o4%22%2C%22UserID%22%3A%2289523199153946%22%7D"

// TestParseCallbackFromURL_RealUserPayload reproduces the user-reported bug:
// the callback URL only has authCodeInfo (JSON) — no flat authCode — and
// uses "host" instead of "loginHost", plus a fully populated userInfo JSON.
// Before the fix, both manual-paste and HTTP-callback code paths returned
// empty AuthCode/LoginHost, producing "callback contained neither authCode
// nor refreshToken".
func TestParseCallbackFromURL_RealUserPayload(t *testing.T) {
	u, err := url.Parse(realCallbackURL)
	if err != nil {
		t.Fatalf("url parse failed: %v", err)
	}

	cb := ParseCallbackFromURL(u)

	if cb == nil {
		t.Fatal("ParseCallbackFromURL returned nil")
	}
	if cb.Error != "" {
		t.Fatalf("unexpected error in callback: %s (%s)", cb.Error, cb.ErrorDescription)
	}
	if got, want := cb.AuthCode, "544huVtGDCAtHVdMQ6dfKVriMd7StziTmqgQjX3wkOQ"; got != want {
		t.Errorf("AuthCode = %q, want %q", got, want)
	}
	if got, want := cb.LoginHost, "https://api.trae.com.cn"; got != want {
		t.Errorf("LoginHost = %q, want %q", got, want)
	}
	if got, want := cb.LoginRegion, "cn"; !strings.EqualFold(got, want) {
		t.Errorf("LoginRegion = %q, want %q (case-insensitive)", got, want)
	}
	if got, want := cb.LoginTraceID, "03273b22805668476f4ade03ef46c5b9"; got != want {
		t.Errorf("LoginTraceID = %q, want %q", got, want)
	}
	if cb.RawQuery == nil {
		t.Fatal("RawQuery should not be nil")
	}
	// The callback also ships userInfo — the authenticator should use it.
	if _, ok := cb.RawQuery["userInfo"]; !ok {
		t.Error("RawQuery missing 'userInfo' key (authenticator needs it)")
	}
}

// TestParseUserInfoFromCallback verifies that ParseUserInfoFromCallback
// returns a usable UserInfoResponse when the callback-side userInfo JSON
// is supplied via a flat string, or directly from CallbackParams.RawQuery.
func TestParseUserInfoFromCallback(t *testing.T) {
	u, err := url.Parse(realCallbackURL)
	if err != nil {
		t.Fatalf("url parse failed: %v", err)
	}
	cb := ParseCallbackFromURL(u)
	info := ParseUserInfoFromCallback(cb)
	if info == nil {
		t.Fatal("ParseUserInfoFromCallback returned nil")
	}
	if got, want := info.UserID, "89523199153946"; got != want {
		t.Errorf("UserID = %q, want %q", got, want)
	}
	if got, want := info.Nickname, "用户37508727791"; got != want {
		t.Errorf("Nickname = %q, want %q", got, want)
	}
	if got, want := info.Email, "188******03"; got != want {
		// Trae CN userInfo sometimes only sends NonPlainTextMobile as the
		// user-visible identifier; we intentionally fall back to it so the
		// credential filename and label stay human-readable.
		t.Errorf("Email (mobile fallback) = %q, want %q", got, want)
	}
}

// TestParseCallback_FlatAuthCodeAndRefresh ensures we still accept the
// documented shortcuts: flat authCode= or refreshToken= parameters.
func TestParseCallback_FlatAuthCodeAndRefresh(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:1/authorize?authCode=flat_code_123&refreshToken=re_456&loginHost=https://www.trae.cn&loginRegion=sg")
	cb := ParseCallbackFromURL(u)
	if cb.AuthCode != "flat_code_123" {
		t.Errorf("AuthCode flat: got %q", cb.AuthCode)
	}
	if cb.RefreshToken != "re_456" {
		t.Errorf("RefreshToken flat: got %q", cb.RefreshToken)
	}
	if cb.LoginHost != "https://www.trae.cn" {
		t.Errorf("LoginHost flat: got %q", cb.LoginHost)
	}
	if cb.LoginRegion != "sg" {
		t.Errorf("LoginRegion flat: got %q", cb.LoginRegion)
	}
}

// TestParseCallback_Error propagates OAuth error codes.
func TestParseCallback_Error(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:1/authorize?error=access_denied&error_description=user%20cancelled")
	cb := ParseCallbackFromURL(u)
	if cb.Error != "access_denied" {
		t.Errorf("Error: got %q", cb.Error)
	}
	if cb.ErrorDescription != "user cancelled" {
		t.Errorf("ErrorDescription: got %q", cb.ErrorDescription)
	}
}
