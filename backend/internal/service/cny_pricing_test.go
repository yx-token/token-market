package service

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/config"
	"github.com/yx-token/token-market/internal/pkg/currency"
)

func TestPricingCNYRebuildKeepsUSDRawCacheAndMultipliers(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{"sample":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_above_200k_tokens":0.000002,"output_cost_per_token_above_200k_tokens":0.000004,"output_cost_per_image":0.02}}`)
	file := filepath.Join(dir, "prices.json")
	require.NoError(t, os.WriteFile(file, raw, 0600))
	svc := NewPricingService(&config.Config{}, nil)
	for range 2 {
		require.NoError(t, svc.loadPricingData(file))
		price := svc.GetModelPricing("sample")
		require.InDelta(t, 0.000007, price.InputCostPerToken, 1e-12)
		require.InDelta(t, 0.14, price.OutputCostPerImage, 1e-12)
		require.Equal(t, 2.0, price.LongContextInputCostMultiplier)
		require.Equal(t, 2.0, price.LongContextOutputCostMultiplier)
	}
	actual, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, raw, actual)
	hash := sha256.Sum256(raw)
	require.Equal(t, hex.EncodeToString(hash[:]), svc.localHash)
}

func TestPricingCNYConvertsEveryPriceFieldAndKeepsFlags(t *testing.T) {
	input := &LiteLLMModelPricing{}
	value := reflect.ValueOf(input).Elem()
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).Kind() == reflect.Float64 {
			value.Field(i).SetFloat(0.125)
		}
	}
	input.LongContextInputTokenThreshold = 200000
	input.SupportsPromptCaching = true
	input.CacheCreationInputTokenCostExplicit = true
	pricingUSDToCNY(input)
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).Kind() != reflect.Float64 {
			continue
		}
		name := value.Type().Field(i).Name
		expected := 0.125 * currency.USDToCNY
		if name == "LongContextInputCostMultiplier" || name == "LongContextOutputCostMultiplier" {
			expected = 0.125
		}
		require.Equal(t, expected, value.Field(i).Float(), name)
	}
	require.Equal(t, 200000, input.LongContextInputTokenThreshold)
	require.True(t, input.SupportsPromptCaching)
	require.True(t, input.CacheCreationInputTokenCostExplicit)
}

func TestPricingCNYFallbackOverrideLayersAndCatalog(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(dir, "fallback.json")
	override := filepath.Join(dir, "override.json")
	require.NoError(t, os.WriteFile(fallback, []byte(`{"fallback":{"input_cost_per_token":0.2}}`), 0600))
	require.NoError(t, os.WriteFile(override, []byte(`{"remote":{"output_cost_per_token":0.4},"added":{"input_cost_per_token":0.3}}`), 0600))
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{FallbackFile: fallback, OverrideFile: override}}, nil)
	for range 2 {
		data, _, err := svc.buildPricingData([]byte(`{"remote":{"input_cost_per_token":0.1,"output_cost_per_token":0.2}}`))
		require.NoError(t, err)
		svc.pricingData = data
		require.InDelta(t, 0.7, data["remote"].InputCostPerToken, 1e-12)
		require.InDelta(t, 2.8, data["remote"].OutputCostPerToken, 1e-12)
		require.InDelta(t, 1.4, data["fallback"].InputCostPerToken, 1e-12)
		require.InDelta(t, 2.1, data["added"].InputCostPerToken, 1e-12)
		for _, entry := range svc.ListPricingCatalog() {
			if entry.Model == "remote" {
				require.InDelta(t, 0.7, entry.InputPrice, 1e-12)
				require.InDelta(t, 2.8, entry.OutputPrice, 1e-12)
			}
		}
	}
}
