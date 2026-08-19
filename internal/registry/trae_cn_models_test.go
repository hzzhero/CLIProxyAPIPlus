package registry

import "testing"

// TestGetTraeCNModels_MatchesUserRequestedList ensures the hard-coded
// Trae CN model catalog contains exactly the three models the user
// asked for (seed-2.1-turbo / glm-5.3 / minimax-m3) and that every
// entry is well-formed enough to appear in the management UI model
// list. Keep this test tight — adding extra models here requires
// explicit user approval.
func TestGetTraeCNModels_MatchesUserRequestedList(t *testing.T) {
	models := GetTraeCNModels()
	if len(models) != 3 {
		t.Fatalf("len(GetTraeCNModels())=%d want exactly 3", len(models))
	}
	want := map[string]struct {
		DisplayName string
		ContextLen  int
	}{
		"seed-2.1-turbo": {DisplayName: "Seed 2.1 Turbo", ContextLen: 128000},
		"glm-5.3":        {DisplayName: "GLM 5.3", ContextLen: 200000},
		"minimax-m3":     {DisplayName: "MiniMax M3", ContextLen: 200000},
	}
	for _, m := range models {
		if m == nil {
			t.Fatal("nil model in list")
		}
		w, ok := want[m.ID]
		if !ok {
			t.Errorf("unexpected model id %q; only %v allowed", m.ID, []string{"seed-2.1-turbo", "glm-5.3", "minimax-m3"})
			continue
		}
		delete(want, m.ID)
		if m.Type != "trae-cn" {
			t.Errorf("%s: Type=%q want trae-cn", m.ID, m.Type)
		}
		if m.DisplayName != w.DisplayName {
			t.Errorf("%s: DisplayName=%q want %q", m.ID, m.DisplayName, w.DisplayName)
		}
		if m.ContextLength != w.ContextLen {
			t.Errorf("%s: ContextLength=%d want %d", m.ID, m.ContextLength, w.ContextLen)
		}
		if len(m.SupportedEndpoints) == 0 {
			t.Errorf("%s: SupportedEndpoints empty", m.ID)
		} else {
			hasChat := false
			for _, ep := range m.SupportedEndpoints {
				if ep == "/chat/completions" {
					hasChat = true
				}
			}
			if !hasChat {
				t.Errorf("%s: missing /chat/completions endpoint", m.ID)
			}
		}
	}
	for id := range want {
		t.Errorf("missing requested model %q", id)
	}
}

func TestGetTraeCNModels_NoDups(t *testing.T) {
	models := GetTraeCNModels()
	seen := map[string]bool{}
	for _, m := range models {
		if seen[m.ID] {
			t.Errorf("duplicate model %q", m.ID)
		}
		seen[m.ID] = true
	}
}
