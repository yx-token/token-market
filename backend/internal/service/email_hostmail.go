package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/yx-token/token-market/market/hostmail"
)

// MarketHostStatus 供嵌入市场读取：是否开启邮箱验证、SMTP 是否已配。
func (s *EmailService) MarketHostStatus(ctx context.Context) (hostmail.Status, error) {
	if s == nil || s.settingRepo == nil {
		return hostmail.Status{}, fmt.Errorf("email service unavailable")
	}
	settings, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyEmailVerifyEnabled,
		SettingKeySMTPHost,
		SettingKeySMTPFrom,
		SettingKeySMTPFromName,
	})
	if err != nil {
		return hostmail.Status{}, err
	}
	from := strings.TrimSpace(settings[SettingKeySMTPFrom])
	name := strings.TrimSpace(settings[SettingKeySMTPFromName])
	display := from
	if name != "" && from != "" {
		display = name + " <" + from + ">"
	} else if name != "" {
		display = name
	}
	return hostmail.Status{
		VerifyEnabled: settings[SettingKeyEmailVerifyEnabled] == "true",
		SMTPReady:     strings.TrimSpace(settings[SettingKeySMTPHost]) != "",
		From:          display,
	}, nil
}
