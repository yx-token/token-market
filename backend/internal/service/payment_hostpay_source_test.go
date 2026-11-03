package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/payment"
	"github.com/yx-token/token-market/market/hostpay"
)

func TestLoadHostPaymentConfig(t *testing.T) {
	tests := []struct {
		name            string
		paymentEnabled  string
		balanceDisabled string
		wantEnabled     bool
	}{
		{name: "both enabled", paymentEnabled: "true", balanceDisabled: "false", wantEnabled: true},
		{name: "payment disabled", paymentEnabled: "false", balanceDisabled: "false", wantEnabled: false},
		{name: "balance disabled", paymentEnabled: "true", balanceDisabled: "true", wantEnabled: false},
		{name: "both disabled", paymentEnabled: "false", balanceDisabled: "true", wantEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			svc := &PaymentConfigService{
				entClient: client,
				settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
					SettingPaymentEnabled:      tt.paymentEnabled,
					SettingBalancePayDisabled:  tt.balanceDisabled,
					SettingMinRechargeAmount:   "0",
					SettingMaxRechargeAmount:   "-1",
					SettingOrderTimeoutMinutes: "0",
				}},
			}

			cfg, err := svc.LoadHostPaymentConfig(ctx)
			require.NoError(t, err)
			require.Equal(t, tt.wantEnabled, cfg.Enabled)
			require.Zero(t, cfg.MinAmount)
			require.Equal(t, float64(-1), cfg.MaxAmount)
			require.Zero(t, cfg.OrderTimeoutMinutes)
			require.Empty(t, cfg.Instances)
		})
	}
}

func TestLoadHostPaymentConfigIncludesOfficialInstancesAndSkipsUnreadable(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: key,
		settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
			SettingPaymentEnabled: "true",
		}},
	}

	legacy, err := payment.Encrypt(`{"privateKey":"legacy-secret"}`, key)
	require.NoError(t, err)
	wantUpdatedAt := time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC)
	inputs := []struct {
		provider, name, config, supported, mode string
		enabled                                 bool
		sort                                    int
	}{
		{provider: payment.TypeAlipay, name: "later", config: `{"appId":"a2"}`, supported: "alipay", enabled: true, sort: 10},
		{provider: payment.TypeWxpay, name: "disabled", config: legacy, supported: "", mode: "native", enabled: false, sort: 2},
		{provider: payment.TypeAlipay, name: "earlier id", config: `{"appId":"a1"}`, enabled: true, sort: 2},
		{provider: payment.TypeWxpay, name: "broken", config: "not-json-or-ciphertext", enabled: true, sort: 1},
		{provider: payment.TypeEasyPay, name: "ignored easypay", config: `{}`, enabled: true, sort: 0},
		{provider: payment.TypeStripe, name: "ignored stripe", config: `{}`, enabled: true, sort: 0},
	}
	ids := make(map[string]int64, len(inputs))
	for _, in := range inputs {
		builder := client.PaymentProviderInstance.Create().
			SetProviderKey(in.provider).SetName(in.name).SetConfig(in.config).
			SetSupportedTypes(in.supported).SetEnabled(in.enabled).
			SetPaymentMode(in.mode).SetSortOrder(in.sort)
		if in.name == "later" {
			builder.SetUpdatedAt(wantUpdatedAt)
		}
		inst, err := builder.Save(ctx)
		require.NoError(t, err)
		ids[in.name] = inst.ID
	}

	cfg, err := svc.LoadHostPaymentConfig(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Instances, 3, "只应包含可解密的 alipay/wxpay 实例，包括停用实例")
	require.Equal(t, []int64{ids["disabled"], ids["earlier id"], ids["later"]}, []int64{
		cfg.Instances[0].ID, cfg.Instances[1].ID, cfg.Instances[2].ID,
	})
	require.Equal(t, hostpay.ProviderWxpay, cfg.Instances[0].ProviderKey)
	require.False(t, cfg.Instances[0].Enabled)
	require.Equal(t, "native", cfg.Instances[0].PaymentMode)
	require.Equal(t, "legacy-secret", cfg.Instances[0].Config["privateKey"])
	require.Equal(t, "alipay", cfg.Instances[2].SupportedTypes)
	require.Equal(t, "a2", cfg.Instances[2].Config["appId"])
	require.Equal(t, wantUpdatedAt, cfg.Instances[2].UpdatedAt)
}

func TestLoadHostPaymentConfigSortsTiesByID(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{entClient: client, settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{}}}
	first, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).SetName("first").SetConfig(`{}`).SetSortOrder(1).Save(ctx)
	require.NoError(t, err)
	second, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).SetName("second").SetConfig(`{}`).SetSortOrder(1).Save(ctx)
	require.NoError(t, err)

	cfg, err := svc.LoadHostPaymentConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID, second.ID}, []int64{cfg.Instances[0].ID, cfg.Instances[1].ID})
}
