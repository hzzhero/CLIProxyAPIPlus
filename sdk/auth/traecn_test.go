package auth

import (
	"context"
	"testing"
	"time"
)

func TestTraeCNAuthenticatorProvider(t *testing.T) {
	a := NewTraeCNAuthenticator()
	if got := a.Provider(); got != "trae-cn" {
		t.Fatalf("Provider() = %q, want trae-cn", got)
	}
}

func TestTraeCNAuthenticatorRefreshLead(t *testing.T) {
	a := NewTraeCNAuthenticator()
	got := a.RefreshLead()
	if got == nil || *got != 30*time.Minute {
		t.Fatalf("RefreshLead() = %v, want 30m", got)
	}
}

func TestTraeCNAuthenticatorLoginNilConfig(t *testing.T) {
	a := NewTraeCNAuthenticator()
	if _, err := a.Login(context.Background(), nil, &LoginOptions{}); err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestTraeCNAuthenticatorLoginNilContext(t *testing.T) {
	a := NewTraeCNAuthenticator()
	// Should not panic with nil context
	cfg := &config.Config{}
	if _, err := a.Login(nil, cfg, &LoginOptions{}); err != nil {
		// Expected to fail due to nil context handling, not panic
		t.Logf("Login with nil context returned error (expected): %v", err)
	}
}
