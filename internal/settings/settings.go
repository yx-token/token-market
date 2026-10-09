// Package settings 管理平台级可调参数（抽佣率、冻结天数等），存放在 settings 表，
// 未设置的项使用默认值。管理员通过后台修改。
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const key = "platform"

type Settings struct {
	CommissionRate             float64            `json:"commission_rate"`               // 寄售抽佣率 c：买家价 = m_s × (1 + c)
	FreezeDays                 int                `json:"freeze_days"`                   // 卖家收益冻结天数
	WithdrawMin                float64            `json:"withdraw_min"`                  // 最低提现金额
	WithdrawAutoApproveMax     float64            `json:"withdraw_auto_approve_max"`     // 低于该金额且无风险时自动审核通过
	ManualRetestDailyLimit     int                `json:"manual_retest_daily_limit"`     // 卖家每天手动复测次数
	QuoteValidityDays          int                `json:"quote_validity_days"`           // 回收报价有效期
	GradeCoefficients          map[string]float64 `json:"grade_coefficients"`            // 等级 → 回收报价系数
	PriceChangeCooldownMinutes int                `json:"price_change_cooldown_minutes"` // 寄售改价冷却
	ShadowConcurrency          int                `json:"shadow_concurrency"`            // 买家影子用户的默认并发
	RefundLockSeconds          int                `json:"refund_lock_seconds"`           // 退款前暂停 Key 后等待的秒数
	NewSellerDays              int                `json:"new_seller_days"`               // 新卖家期
	NewSellerDirectCap         float64            `json:"new_seller_direct_cap"`         // 新卖家来自单一买家指定流量的收益上限
	SchedulerChangeBudget      int                `json:"scheduler_change_budget"`       // 每轮最多改动的 sub2api 账号数
	BalanceMismatchTolerance   float64            `json:"balance_mismatch_tolerance"`    // 余额对账容差
	AllowNoProxy               bool               `json:"allow_no_proxy"`                // 没有可用代理时是否允许上架（仅开发环境）
	RequireSellerProxy         bool               `json:"require_seller_proxy"`          // 新接入账号必须选择卖家自己配置的代理
	PriceModels                []string           `json:"price_models"`                  // 市场展示官方价格的模型（价格取自 sub2api 内置价目表）
	// PaymentPublicURL 是控制面对外地址：支付异步通知发到 <地址>/api/payments/{alipay|wechat}/notify，网页支付完成后跳回钱包
	PaymentPublicURL string `json:"payment_public_url"`
	// 支付方式开关：alipay_page / alipay_f2f / wechat_native / manual。在线渠道还需要在"支付配置"中配置并启用商户才可用
	PaymentChannels             map[string]bool `json:"payment_channels"`
	RechargeMin                 float64         `json:"recharge_min"`                   // 单笔充值下限（元）
	RechargeMax                 float64         `json:"recharge_max"`                   // 单笔充值上限（元）
	RechargeOrderTimeoutMinutes int             `json:"recharge_order_timeout_minutes"` // 在线支付订单的支付时限
	RefundOriginalRoute         bool            `json:"refund_original_route"`          // 余额退款优先原路退回到在线充值单
	RefundOriginalRouteDays     int             `json:"refund_original_route_days"`     // 只原路退回这么多天内的充值单（渠道的可退款期限）
}

func Defaults() Settings {
	return Settings{
		CommissionRate:             0.20,
		FreezeDays:                 7,
		WithdrawMin:                10,
		WithdrawAutoApproveMax:     100,
		ManualRetestDailyLimit:     3,
		QuoteValidityDays:          7,
		GradeCoefficients:          map[string]float64{"S": 1.2, "A": 1.0, "B": 0.85, "C": 0.7, "D": 0},
		PriceChangeCooldownMinutes: 60,
		ShadowConcurrency:          5,
		RefundLockSeconds:          60,
		NewSellerDays:              30,
		NewSellerDirectCap:         500,
		SchedulerChangeBudget:      50,
		BalanceMismatchTolerance:   0.05,
		RequireSellerProxy:         true,
		PriceModels: []string{"claude-opus-4-1", "claude-sonnet-4-5", "claude-haiku-4-5", "gpt-5.1", "gpt-5.1-codex",
			"gemini-2.5-pro", "gemini-2.5-flash", "grok-4", "kimi-for-coding", "glm-4.6", "MiniMax-M2"},
		PaymentChannels:             map[string]bool{"alipay_page": true, "alipay_f2f": true, "wechat_native": true, "manual": true},
		RechargeMin:                 1,
		RechargeMax:                 50000,
		RechargeOrderTimeoutMinutes: 30,
		RefundOriginalRoute:         true,
		RefundOriginalRouteDays:     90,
	}
}

func (s Settings) Validate() error {
	switch {
	case s.CommissionRate < 0 || s.CommissionRate > 5:
		return errors.New("commission_rate must be in [0, 5]")
	case s.FreezeDays < 0:
		return errors.New("freeze_days must be >= 0")
	case s.WithdrawMin < 0 || s.WithdrawAutoApproveMax < 0:
		return errors.New("withdraw limits must be >= 0")
	case s.ManualRetestDailyLimit < 0:
		return errors.New("manual_retest_daily_limit must be >= 0")
	case s.QuoteValidityDays <= 0:
		return errors.New("quote_validity_days must be > 0")
	case s.ShadowConcurrency <= 0:
		return errors.New("shadow_concurrency must be > 0")
	case s.SchedulerChangeBudget <= 0:
		return errors.New("scheduler_change_budget must be > 0")
	case s.RechargeMin <= 0 || s.RechargeMax < s.RechargeMin:
		return errors.New("recharge_min must be > 0 and recharge_max >= recharge_min")
	case s.RechargeOrderTimeoutMinutes < 5 || s.RechargeOrderTimeoutMinutes > 1440:
		return errors.New("recharge_order_timeout_minutes must be between 5 and 1440")
	case s.RefundOriginalRouteDays < 0:
		return errors.New("refund_original_route_days must be >= 0")
	}
	if s.PaymentPublicURL != "" {
		u, err := url.Parse(s.PaymentPublicURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" {
			return errors.New("payment_public_url must be an http(s) url")
		}
	}
	for ch := range s.PaymentChannels {
		switch ch {
		case "alipay_page", "alipay_f2f", "wechat_native", "manual":
		default:
			return fmt.Errorf("unknown payment channel %s", ch)
		}
	}
	for g, c := range s.GradeCoefficients {
		if c < 0 {
			return fmt.Errorf("grade coefficient %s must be >= 0", g)
		}
	}
	return nil
}

// GradeCoefficient 返回等级系数，未配置的等级视为 0（不报价）。
func (s Settings) GradeCoefficient(grade string) float64 {
	return s.GradeCoefficients[grade]
}

type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Load 读取设置，未保存的字段取默认值。
func Load(ctx context.Context, db DB) (Settings, error) {
	s := Defaults()
	var raw []byte
	err := db.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("decode settings: %w", err)
	}
	return s, nil
}

// Update 用 patch（部分字段）覆盖当前设置并保存。
func Update(ctx context.Context, db DB, patch json.RawMessage) (Settings, error) {
	s, err := Load(ctx, db)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(patch, &s); err != nil {
		return s, fmt.Errorf("decode patch: %w", err)
	}
	s.PaymentPublicURL = strings.TrimRight(strings.TrimSpace(s.PaymentPublicURL), "/")
	if err := s.Validate(); err != nil {
		return s, err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return s, err
	}
	_, err = db.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, raw)
	return s, err
}
