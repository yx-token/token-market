package service

import (
	"context"

	"github.com/yx-token/token-market/market/hostreg"
)

// LoadHostRegistrationPolicy 供嵌入市场读取系统设置里的邮箱域名白名单 / 限量策略。
func (s *SettingService) LoadHostRegistrationPolicy(ctx context.Context) (hostreg.Policy, error) {
	if s == nil {
		return hostreg.Policy{}, nil
	}
	return hostreg.Policy{
		Whitelist:          s.GetRegistrationEmailSuffixWhitelist(ctx),
		DomainQuotaEnabled: s.IsRegistrationEmailDomainQuotaEnabled(ctx),
	}, nil
}
