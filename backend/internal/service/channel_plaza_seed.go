package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yx-token/token-market/internal/pkg/logger"
)

// OfficialPlazaPricingChannelName 是引擎启动时幂等写入的「模型价格页」官方价目渠道。
// 不绑定任何分组，只给广场展示用，不影响路由与计费。
const OfficialPlazaPricingChannelName = "official-plaza-pricing"

// officialPlazaSeedSpec 描述某个官方平台要写入渠道定价的模型挑选规则。
type officialPlazaSeedSpec struct {
	platform string
	want     []string
	prefixes []string
	limit    int
}

var officialPlazaSeedSpecs = []officialPlazaSeedSpec{
	{PlatformAnthropic, []string{"claude-sonnet-4-5", "claude-haiku-4-5", "claude-opus-4-5"}, []string{"claude-sonnet-4", "claude-haiku-4", "claude-opus-4"}, 8},
	{PlatformOpenAI, []string{"gpt-5", "gpt-4.1", "gpt-4.1-mini", "gpt-5.1"}, []string{"gpt-5", "gpt-4.1"}, 8},
	{PlatformGemini, []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash"}, []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash"}, 8},
	{PlatformGrok, []string{"grok-4"}, []string{"grok-4", "grok-3"}, 8},
	{PlatformKimi, []string{"kimi-for-coding", "kimi-k2.5"}, []string{"kimi-", "moonshot"}, 8},
	{PlatformZhipu, []string{"glm-4.6", "glm-5"}, []string{"glm-"}, 8},
	{PlatformMiniMax, []string{"MiniMax-M2", "minimax-m2.7"}, []string{"minimax", "MiniMax"}, 8},
}

// scheduleEnsureOfficialPlazaPricing 在后台补齐远程/生产环境的广场官方价目渠道。
// 本地 dev.sh 也会写；两边幂等，互不覆盖已有真实渠道定价。
func (s *ChannelService) scheduleEnsureOfficialPlazaPricing() {
	if s == nil || s.pricingService == nil || s.repo == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := s.EnsureOfficialPlazaPricing(ctx); err != nil {
			logger.LegacyPrintf("service.channel", "[Channel] ensure official plaza pricing: %v", err)
		}
	}()
}

// EnsureOfficialPlazaPricing 当活跃渠道里还没有任何可展示的官方价目时，
// 写入/补齐 official-plaza-pricing（无分组）。已有广场价目则跳过。
func (s *ChannelService) EnsureOfficialPlazaPricing(ctx context.Context) error {
	if s == nil || s.pricingService == nil || s.repo == nil {
		return nil
	}

	catalog, err := s.ListConfiguredPricingCatalog(ctx)
	if err != nil {
		return fmt.Errorf("list configured catalog: %w", err)
	}
	if len(catalog) > 0 {
		return nil
	}

	pricing := s.buildOfficialPlazaModelPricing()
	if len(pricing) == 0 {
		return fmt.Errorf("no official models resolved from pricing catalog")
	}

	channels, err := s.repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}
	var existing *Channel
	for i := range channels {
		if channels[i].Name == OfficialPlazaPricingChannelName {
			existing = &channels[i]
			break
		}
	}

	desc := "系统自动配置：官方支持模型参考价（仅供模型价格页，未绑定分组）"
	if existing != nil {
		emptyGroups := []int64{}
		_, err = s.Update(ctx, existing.ID, &UpdateChannelInput{
			Description:  &desc,
			GroupIDs:     &emptyGroups,
			ModelPricing: &pricing,
		})
		if err != nil {
			return fmt.Errorf("update %s: %w", OfficialPlazaPricingChannelName, err)
		}
		logger.LegacyPrintf("service.channel", "[Channel] refreshed %s for model plaza (%d platforms)", OfficialPlazaPricingChannelName, len(pricing))
		return nil
	}

	_, err = s.Create(ctx, &CreateChannelInput{
		Name:         OfficialPlazaPricingChannelName,
		Description:  desc,
		GroupIDs:     nil,
		ModelPricing: pricing,
	})
	if err != nil {
		return fmt.Errorf("create %s: %w", OfficialPlazaPricingChannelName, err)
	}
	logger.LegacyPrintf("service.channel", "[Channel] created %s for model plaza (%d platforms)", OfficialPlazaPricingChannelName, len(pricing))
	return nil
}

func (s *ChannelService) buildOfficialPlazaModelPricing() []ChannelModelPricing {
	out := make([]ChannelModelPricing, 0, len(officialPlazaSeedSpecs))
	for _, spec := range officialPlazaSeedSpecs {
		provider, ok := OfficialLiteLLMProvider(spec.platform)
		if !ok || s.pricingService == nil {
			continue
		}
		models := pickOfficialPlazaModels(s.pricingService.ListModelNamesByProvider(provider), spec)
		if len(models) == 0 {
			continue
		}
		out = append(out, ChannelModelPricing{
			Platform:    spec.platform,
			Models:      models,
			BillingMode: BillingModeToken,
		})
	}
	return out
}

func pickOfficialPlazaModels(available []string, spec officialPlazaSeedSpec) []string {
	skip := []string{"audio", "tts", "realtime", "transcribe", "embedding", "moderation"}
	limit := spec.limit
	if limit <= 0 {
		limit = 8
	}
	out := make([]string, 0, limit)
	seen := make(map[string]struct{})
	ok := func(name string) bool {
		low := strings.ToLower(name)
		if _, dup := seen[low]; dup {
			return false
		}
		for _, s := range skip {
			if strings.Contains(low, s) {
				return false
			}
		}
		return true
	}
	for _, want := range spec.want {
		for _, m := range available {
			if strings.EqualFold(m, want) && ok(m) {
				out = append(out, m)
				seen[strings.ToLower(m)] = struct{}{}
				break
			}
		}
	}
	for _, prefix := range spec.prefixes {
		if len(out) >= limit {
			break
		}
		for _, m := range available {
			if len(out) >= limit {
				break
			}
			if strings.HasPrefix(strings.ToLower(m), strings.ToLower(prefix)) && ok(m) {
				out = append(out, m)
				seen[strings.ToLower(m)] = struct{}{}
			}
		}
	}
	if len(out) > limit {
		return out[:limit]
	}
	return out
}
