// Package hostpay 是宿主（sub2api 引擎）向闲猫市场提供「收款配置」的接口。
//
// 市场不再自存支付商户密钥：C 端钱包充值使用的支付宝 / 微信支付商户凭证、充值限额、在线支付时限，
// 全部取自 sub2api 管理端「设置 → 支付设置」里的「支付宝官方 / 微信官方」服务商实例与全局支付配置。
// 下单、异步通知、查单、关单、原路退款仍由市场自己完成（market/internal/payment 与 billing），
// 这里只负责把配置交给市场。
//
// 宿主在启动时调用 SetSource 注册数据来源（见 backend/cmd/server）；没有注册时（市场以独立进程运行）
// Load 返回 ErrNoSource，市场按「没有可用的在线支付」处理。
//
// 本包是叶子包：不得 import market/internal/... 或 backend 的任何包，宿主与市场两侧都依赖它。
package hostpay

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 服务商实例的 provider_key，与 sub2api 一致。只有这两类实例会交给市场。
const (
	ProviderAlipay = "alipay" // 支付宝官方
	ProviderWxpay  = "wxpay"  // 微信官方
)

// Config 是某一时刻 sub2api 的收款配置快照。
type Config struct {
	// Enabled：sub2api 支付总开关已打开，且没有关闭余额充值（balance_disabled=false）。
	// 为 false 时市场不再接受新的在线充值，但已有订单的通知、查单、关单、退款照常处理。
	Enabled bool
	// MinAmount / MaxAmount：单笔充值金额上下限（元）。<=0 表示 sub2api 未设置，市场使用自己的默认值。
	MinAmount float64
	MaxAmount float64
	// OrderTimeoutMinutes：在线支付订单的支付时限（分钟）。<=0 表示 sub2api 未设置，市场使用默认值。
	OrderTimeoutMinutes int
	// Instances：provider_key 为 alipay / wxpay 的全部服务商实例，**包含已停用的**——
	// 已创建的订单仍要用当时实例的凭证验签、查单、退款。其它类型（易支付、Stripe 等）不在这里。
	Instances []Instance
}

// Instance 是 sub2api 的一个支付宝官方 / 微信官方服务商实例。
type Instance struct {
	ID             int64  // payment_provider_instances.id
	ProviderKey    string // ProviderAlipay / ProviderWxpay
	Name           string
	Enabled        bool
	SupportedTypes string // 原样透传 supported_types（逗号分隔，可能为空）
	PaymentMode    string // 原样透传 payment_mode 列（支付宝 "redirect" 表示商户未开通当面付，只走电脑网站支付）
	SortOrder      int
	// Config：已解密的明文配置，键名与 sub2api 保持一致，例如
	//   支付宝：appId、privateKey、publicKey（或 alipayPublicKey）、notifyUrl、returnUrl、paymentMode
	//   微信：  appId、mchId、privateKey、apiV3Key、certSerial、publicKey、publicKeyId、notifyUrl
	// 调用方不得修改该 map。
	Config    map[string]string
	UpdatedAt time.Time
}

// Source 读取当前的收款配置。实现应当足够快（市场会做秒级缓存），并且只读。
type Source func(ctx context.Context) (*Config, error)

// ErrNoSource 表示宿主没有注册收款配置来源。
var ErrNoSource = errors.New("hostpay: payment config source is not registered by the host")

var (
	mu  sync.RWMutex
	src Source
)

// SetSource 由宿主在启动时调用一次；传 nil 取消注册（测试用）。
func SetSource(s Source) {
	mu.Lock()
	src = s
	mu.Unlock()
}

// Load 返回当前的收款配置；未注册来源时返回 ErrNoSource。
func Load(ctx context.Context) (*Config, error) {
	mu.RLock()
	s := src
	mu.RUnlock()
	if s == nil {
		return nil, ErrNoSource
	}
	return s(ctx)
}

// InFlightCounter 统计市场里仍依赖某个服务商实例凭证的订单数：待支付、已取消但仍在查单窗口内的充值单，
// 以及尚未结束的原路退款打款。方向与 Source 相反：由市场注册，宿主调用。
type InFlightCounter func(ctx context.Context, instanceID int64) (int, error)

var counter InFlightCounter

// SetInFlightCounter 由市场在构建应用时调用；传 nil 取消注册（测试用）。
func SetInFlightCounter(c InFlightCounter) {
	mu.Lock()
	counter = c
	mu.Unlock()
}

// InFlightOrders 供宿主在删除实例、停用实例或修改关键凭证前检查：市场在途订单数大于 0 时应当拒绝，
// 否则这些订单会因为拿不到原凭证而无法验签、查单、关单、退款。市场未注册时返回 0。
func InFlightOrders(ctx context.Context, instanceID int64) (int, error) {
	mu.RLock()
	c := counter
	mu.RUnlock()
	if c == nil {
		return 0, nil
	}
	return c(ctx, instanceID)
}
