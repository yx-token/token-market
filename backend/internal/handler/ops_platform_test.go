package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/service"
)

func TestResolveOpsPlatformPrefersResolvedCompositeTarget(t *testing.T) {
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
	ctx := service.WithResolvedTargetPlatform(context.Background(), service.PlatformOpenAI)

	require.Equal(t, service.PlatformOpenAI, resolveOpsPlatform(ctx, apiKey, service.PlatformAnthropic))
}
