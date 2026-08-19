package cliproxy

import (
	"context"
	"strings"
	"testing"

	internalregistry "github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

// TestRegisterModelsForAuth_TraeCNAssignsStaticList reproduces the
// failure path the user reported: a freshly persisted trae-cn OAuth
// credential renders "该凭证暂无可用模型" in the management UI.
// The root cause is usually one of:
//
//  1. provider switch in registerModelsForAuth missed the trae-cn case
//     → fall through to the OpenAI-compat path; without a matching
//     compat name in config we UnregisterClient.
//  2. an auth attribute (compat_name/provider_key) accidentally flips
//     provider to "openai-compatibility" prematurely.
//  3. the static registry itself returns an empty slice.
//
// We assert the three models the user explicitly requested are bound
// to the auth ID as client, with Type="trae-cn", and the legacy
// UnregisterClient-for-compat path cannot erase them.
func TestRegisterModelsForAuth_TraeCNAssignsStaticList(t *testing.T) {
	service := &Service{cfg: &config.Config{}}
	auth := &coreauth.Auth{
		ID:       "trae-cn-188__03-dfe433.json",
		FileName: "trae-cn-188__03-dfe433.json",
		Provider: "trae-cn",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind":    "oauth",
			"email":        "188******03",
			"nickname":     "用户37508727791",
			"login_region": "cn",
			"region_id":    "cn",
		},
	}

	r := internalregistry.GetGlobalRegistry()
	r.UnregisterClient(auth.ID)
	t.Cleanup(func() { r.UnregisterClient(auth.ID) })

	service.registerModelsForAuth(context.Background(), auth)

	models := r.GetModelsForClient(auth.ID)
	if len(models) != 3 {
		names := make([]string, 0, len(models))
		for _, m := range models {
			if m != nil {
				names = append(names, m.ID)
			}
		}
		t.Fatalf("GetModelsForClient(%q) returned %d models %v; want exactly 3 (seed-2.1-turbo, glm-5.3, minimax-m3)",
			auth.ID, len(models), names)
	}

	want := map[string]struct{}{
		"seed-2.1-turbo": {},
		"glm-5.3":        {},
		"minimax-m3":     {},
	}
	for _, m := range models {
		if m == nil {
			t.Fatal("nil model in registered list")
		}
		if _, ok := want[m.ID]; !ok {
			t.Errorf("unexpected registered model %q", m.ID)
			continue
		}
		delete(want, m.ID)
		if !strings.EqualFold(m.Type, "trae-cn") {
			t.Errorf("model %q has Type=%q want trae-cn", m.ID, m.Type)
		}
	}
	for missing := range want {
		t.Errorf("expected registered model %q, not found", missing)
	}
}
