package registry

// GetTraeCNModels returns the static model list for the "trae-cn" provider.
// The Trae CN upstream currently does not expose a public "list models"
// endpoint usable from third-party integrations, so we hard-code the
// models known to be routable via the Trae CN OpenAI-compatible gateway
// (per user guidance: seed-2.1-turbo, glm-5.3, minimax-m3).
//
// NOTE: Context lengths are conservative defaults matching the
// documented specs of each vendor; the Trae CN gateway may accept a
// slightly larger window, but we prefer to under-promise on metadata
// rather than advertise a context size the backend will eventually
// truncate on.
func GetTraeCNModels() []*ModelInfo {
	now := int64(1755580800) // 2025-08-20 release-ish
	return []*ModelInfo{
		{
			ID:                  "seed-2.1-turbo",
			Object:              "model",
			Created:             now,
			OwnedBy:             "trae-cn",
			Type:                "trae-cn",
			DisplayName:         "Seed 2.1 Turbo",
			Description:         "Seed 2.1 Turbo via Trae CN gateway",
			ContextLength:       128000,
			MaxCompletionTokens: 8192,
			SupportedEndpoints:  []string{"/chat/completions"},
			Thinking:            nil,
		},
		{
			ID:                  "glm-5.3",
			Object:              "model",
			Created:             now,
			OwnedBy:             "trae-cn",
			Type:                "trae-cn",
			DisplayName:         "GLM 5.3",
			Description:         "GLM 5.3 via Trae CN gateway",
			ContextLength:       200000,
			MaxCompletionTokens: 16384,
			SupportedEndpoints:  []string{"/chat/completions"},
			Thinking:            nil,
		},
		{
			ID:                  "minimax-m3",
			Object:              "model",
			Created:             now,
			OwnedBy:             "trae-cn",
			Type:                "trae-cn",
			DisplayName:         "MiniMax M3",
			Description:         "MiniMax M3 via Trae CN gateway",
			ContextLength:       200000,
			MaxCompletionTokens: 16384,
			SupportedEndpoints:  []string{"/chat/completions"},
			Thinking:            nil,
		},
	}
}
