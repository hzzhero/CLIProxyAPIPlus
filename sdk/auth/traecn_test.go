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
	lead := a.RefreshLead()
	if lead == nil {
		t.Fatal("RefreshLead() = nil, want 30m")
	}
	if *lead != 30*time.Minute {
		t.Fatalf("RefreshLead() = %v, want 30m", *lead)
	}
}

func TestTraeCNAuthenticatorLoginNilConfig(t *testing.T) {
	a := NewTraeCNAuthenticator()
	if _, err := a.Login(context.Background(), nil, &LoginOptions{}); err == nil {
		t.Fatal("expected error for nil config")
	}
}
