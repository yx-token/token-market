//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureOfficialPlazaPricing_SkipsWhenCatalogExists(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"claude-sonnet-4-5": {LiteLLMProvider: "anthropic", Mode: "chat", InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
	})
	created := false
	repo := &mockChannelRepository{
		listAllFn: func(ctx context.Context) ([]Channel, error) {
			return []Channel{{
				ID: 1, Name: "ops-channel", Status: StatusActive,
				ModelPricing: []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4-5"}}},
			}}, nil
		},
		createFn: func(ctx context.Context, channel *Channel) error {
			created = true
			return nil
		},
	}
	svc := NewChannelService(repo, &stubGroupRepoForAvailable{}, nil, pricingSvc, nil)
	require.NoError(t, svc.EnsureOfficialPlazaPricing(context.Background()))
	require.False(t, created)
}

func TestEnsureOfficialPlazaPricing_CreatesWhenCatalogEmpty(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"claude-sonnet-4-5": {LiteLLMProvider: "anthropic", Mode: "chat", InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
		"gpt-5":             {LiteLLMProvider: "openai", Mode: "chat", InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
		"gemini-2.5-pro":    {LiteLLMProvider: "gemini", Mode: "chat", InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
	})
	var stored *Channel
	repo := &mockChannelRepository{
		listAllFn: func(ctx context.Context) ([]Channel, error) {
			if stored == nil {
				return nil, nil
			}
			return []Channel{*stored}, nil
		},
		existsByNameFn: func(ctx context.Context, name string) (bool, error) {
			return stored != nil && stored.Name == name, nil
		},
		createFn: func(ctx context.Context, channel *Channel) error {
			cp := *channel
			cp.ID = 9
			stored = &cp
			channel.ID = 9
			return nil
		},
		getByIDFn: func(ctx context.Context, id int64) (*Channel, error) {
			require.NotNil(t, stored)
			cp := *stored
			return &cp, nil
		},
		replaceModelPricingFn: func(ctx context.Context, channelID int64, pricingList []ChannelModelPricing) error {
			return nil
		},
	}
	svc := NewChannelService(repo, &stubGroupRepoForAvailable{}, nil, pricingSvc, nil)
	require.NoError(t, svc.EnsureOfficialPlazaPricing(context.Background()))
	require.NotNil(t, stored)
	require.Equal(t, OfficialPlazaPricingChannelName, stored.Name)
	require.Empty(t, stored.GroupIDs)
	require.NotEmpty(t, stored.ModelPricing)

	platforms := map[string]bool{}
	for _, p := range stored.ModelPricing {
		platforms[p.Platform] = true
		require.Equal(t, BillingModeToken, p.BillingMode)
		require.NotEmpty(t, p.Models)
	}
	require.True(t, platforms[PlatformAnthropic])
	require.True(t, platforms[PlatformOpenAI])
	require.True(t, platforms[PlatformGemini])
}

func TestPickOfficialPlazaModels_PrefersExactThenPrefix(t *testing.T) {
	available := []string{"claude-opus-4-6", "claude-sonnet-4-5", "claude-sonnet-4-5-20250929", "claude-audio-1"}
	got := pickOfficialPlazaModels(available, officialPlazaSeedSpec{
		platform: PlatformAnthropic,
		want:     []string{"claude-sonnet-4-5"},
		prefixes: []string{"claude-sonnet-4", "claude-opus-4"},
		limit:    3,
	})
	require.Equal(t, []string{"claude-sonnet-4-5", "claude-sonnet-4-5-20250929", "claude-opus-4-6"}, got)
}
