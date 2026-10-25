package repository

import (
	"context"

	"github.com/yx-token/token-market/internal/pkg/kimi"
	"github.com/yx-token/token-market/internal/service"
)

type kimiOAuthClient struct{}

func NewKimiOAuthClient() service.KimiOAuthClient { return &kimiOAuthClient{} }
func (c *kimiOAuthClient) DeviceAuthorization(ctx context.Context, proxyURL, deviceID string) (*kimi.DeviceAuthorizationResponse, error) {
	return kimi.NewClient(proxyURL).DeviceAuthorization(ctx, deviceID)
}
func (c *kimiOAuthClient) Poll(ctx context.Context, proxyURL, deviceCode, deviceID string) (kimi.PollStatus, *kimi.TokenResponse, int64, error) {
	return kimi.NewClient(proxyURL).Poll(ctx, deviceCode, deviceID)
}
func (c *kimiOAuthClient) Refresh(ctx context.Context, proxyURL, refreshToken, deviceID string) (*kimi.TokenResponse, error) {
	return kimi.NewClient(proxyURL).Refresh(ctx, refreshToken, deviceID)
}
