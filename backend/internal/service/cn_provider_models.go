package service

import "github.com/yx-token/token-market/internal/pkg/openai"

// DefaultCNModelIDs is the local discovery fallback for CN providers. Live
// upstream catalogs and account mappings take precedence over these choices.
func DefaultCNModelIDs(platform string) []string {
	switch platform {
	case PlatformKimi:
		return []string{"kimi-k2.5", "kimi-k2-thinking", "kimi-k2"}
	case PlatformZhipu:
		return []string{"glm-5", "glm-4.7", "glm-4.6"}
	case PlatformDeepseek:
		return []string{"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-flash"}
	case PlatformMiniMax:
		return []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.5"}
	default:
		return nil
	}
}

// DefaultCNAccountModels keeps Coding Plan's canonical Kimi alias first, since
// its subscription endpoint need not accept the pay-as-you-go model IDs.
func DefaultCNAccountModels(account *Account) []openai.Model {
	ids := DefaultCNModelIDs(account.Platform)
	if account.Platform == PlatformKimi && account.IsCodingPlan() {
		ids = []string{"kimi-for-coding"}
	}
	models := make([]openai.Model, 0, len(ids))
	for _, id := range ids {
		models = append(models, openai.Model{ID: id, Object: "model", Type: "model", OwnedBy: account.Platform, DisplayName: id})
	}
	return models
}
