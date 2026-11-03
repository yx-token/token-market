package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yx-token/token-market/ent/paymentproviderinstance"
	"github.com/yx-token/token-market/internal/payment"
	"github.com/yx-token/token-market/market/hostpay"
)

// LoadHostPaymentConfig exposes the sub2api payment settings needed by an
// embedded market. Disabled official instances are included for existing orders.
func (s *PaymentConfigService) LoadHostPaymentConfig(ctx context.Context) (*hostpay.Config, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("payment config service is not initialized")
	}

	cfg, err := s.GetPaymentConfig(ctx)
	if err != nil {
		return nil, err
	}
	instances, err := s.entClient.PaymentProviderInstance.Query().
		Where(paymentproviderinstance.ProviderKeyIn(payment.TypeAlipay, payment.TypeWxpay)).
		Order(paymentproviderinstance.BySortOrder(), paymentproviderinstance.ByID()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query official payment provider instances: %w", err)
	}

	out := &hostpay.Config{
		Enabled:             cfg.Enabled && !cfg.BalanceDisabled,
		MinAmount:           cfg.MinAmount,
		MaxAmount:           cfg.MaxAmount,
		OrderTimeoutMinutes: cfg.OrderTimeoutMin,
		Instances:           make([]hostpay.Instance, 0, len(instances)),
	}
	for _, inst := range instances {
		decrypted, err := s.DecryptProviderConfig(inst.Config)
		if err != nil {
			// Keep the snapshot usable; an unreadable instance cannot safely process
			// an existing order, but must not hide other valid payment instances.
			slog.Warn("hostpay: skipping provider instance with unreadable config",
				"instance_id", inst.ID, "provider_key", inst.ProviderKey, "err", err)
			continue
		}
		if decrypted == nil {
			decrypted = map[string]string{}
		}
		out.Instances = append(out.Instances, hostpay.Instance{
			ID:             inst.ID,
			ProviderKey:    inst.ProviderKey,
			Name:           inst.Name,
			Enabled:        inst.Enabled,
			SupportedTypes: inst.SupportedTypes,
			PaymentMode:    inst.PaymentMode,
			SortOrder:      inst.SortOrder,
			Config:         decrypted,
			UpdatedAt:      inst.UpdatedAt,
		})
	}
	return out, nil
}
