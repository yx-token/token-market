// Package payment 对接支付渠道：支付宝（电脑网站支付、当面付）与微信支付（Native 扫码）。
//
// 每个渠道实现 Provider：下单、查单、关单、退款、解析并验签异步通知。订单状态的推进（到账、过期、退款）
// 由 billing 负责，这里只做协议。金额一律以分为单位。
package payment

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 支付方式（充值单的 channel）。
const (
	ChannelManual       = "manual"        // 人工确认到账
	ChannelAlipayPage   = "alipay_page"   // 支付宝电脑网站支付：跳转到支付宝收银台
	ChannelAlipayF2F    = "alipay_f2f"    // 支付宝当面付：展示二维码，用支付宝扫码
	ChannelWechatNative = "wechat_native" // 微信支付 Native：展示二维码，用微信扫码
)

// 渠道（Provider.Name）。
const (
	ProviderManual = "manual"
	ProviderAlipay = "alipay"
	ProviderWechat = "wechat"
)

// AllChannels 是全部支付方式，按前端展示顺序排列。
var AllChannels = []string{ChannelAlipayPage, ChannelAlipayF2F, ChannelWechatNative, ChannelManual}

// ProviderOf 返回支付方式所属的渠道。
func ProviderOf(channel string) string {
	switch channel {
	case ChannelAlipayPage, ChannelAlipayF2F:
		return ProviderAlipay
	case ChannelWechatNative:
		return ProviderWechat
	case ChannelManual:
		return ProviderManual
	}
	return ""
}

// Order 是发给渠道的下单参数。
type Order struct {
	OutTradeNo string
	AmountFen  int64
	Subject    string
	ExpireAt   time.Time
	ClientIP   string
	ReturnURL  string // 电脑网站支付完成后浏览器跳回的地址
}

// Intent 告诉前端如何完成支付。
type Intent struct {
	Kind   string `json:"kind"`              // redirect：跳转到 URL；qrcode：把 QRCode 渲染成二维码让用户扫
	URL    string `json:"url,omitempty"`     // 支付页地址
	QRCode string `json:"qr_code,omitempty"` // 二维码内容
}

type TradeState string

const (
	TradePending TradeState = "pending" // 未支付（含当面付尚未扫码）
	TradePaid    TradeState = "paid"    // 已支付（含支付后发生过退款）
	TradeClosed  TradeState = "closed"  // 已关闭，未支付
)

// Trade 是渠道侧的交易状态。
type Trade struct {
	OutTradeNo string
	State      TradeState
	TradeNo    string // 渠道交易号
	AmountFen  int64  // 订单金额
	PaidAt     *time.Time
}

// Refund 是一笔原路退款。
type Refund struct {
	OutTradeNo  string
	OutRefundNo string // 退款单号，重复提交同一单号是幂等的
	AmountFen   int64  // 本次退款金额
	TotalFen    int64  // 原订单金额
	Reason      string
}

type RefundState string

const (
	RefundProcessing RefundState = "processing"
	RefundSuccess    RefundState = "success"
	RefundFailed     RefundState = "failed"
)

type RefundResult struct {
	State    RefundState
	RefundNo string // 渠道退款单号
	Message  string
}

var (
	// ErrTradeNotFound 渠道侧没有这笔交易（当面付在用户扫码前也是这样）。
	ErrTradeNotFound = errors.New("payment: trade not found")
	// ErrRefundNotFound 渠道侧没有这笔退款。
	ErrRefundNotFound = errors.New("payment: refund not found")
)

// Error 是渠道返回的业务错误。
type Error struct {
	Provider string
	Code     string
	Message  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s %s", e.Provider, e.Code, e.Message)
}

// Provider 是一个支付渠道。
type Provider interface {
	Name() string
	// Channels 返回已配置可用的支付方式。
	Channels() []string
	Create(ctx context.Context, channel string, o Order) (*Intent, error)
	// Query 查询交易；交易不存在时返回 ErrTradeNotFound。
	Query(ctx context.Context, outTradeNo string) (*Trade, error)
	// Close 关闭未支付的交易；交易不存在视为成功。
	Close(ctx context.Context, outTradeNo string) error
	Refund(ctx context.Context, r Refund) (*RefundResult, error)
	// QueryRefund 查询退款；退款不存在时返回 ErrRefundNotFound。
	QueryRefund(ctx context.Context, outTradeNo, outRefundNo string) (*RefundResult, error)
	// ParseNotify 校验签名并解析异步通知。返回的 Trade 为 nil 表示通知与支付状态无关（例如退款通知）。
	ParseNotify(r *http.Request) (*Trade, error)
	// AckNotify 按渠道要求应答通知；err 不为空表示处理失败，渠道会重试。
	AckNotify(w http.ResponseWriter, err error)
}

// Registry 是已配置的渠道。
type Registry struct {
	providers map[string]Provider
}

func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{providers: map[string]Provider{}}
	for _, p := range ps {
		if p != nil {
			r.providers[p.Name()] = p
		}
	}
	return r
}

// Get 返回渠道，未配置时返回 nil。
func (r *Registry) Get(name string) Provider {
	if r == nil {
		return nil
	}
	return r.providers[name]
}

// Configured 返回已配置的支付方式（不含人工确认）。
func (r *Registry) Configured() map[string]bool {
	out := map[string]bool{}
	if r == nil {
		return out
	}
	for _, p := range r.providers {
		for _, c := range p.Channels() {
			out[c] = true
		}
	}
	return out
}

// Fen 把元转换成分，要求最多两位小数。
func Fen(yuan string) (int64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(yuan), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", yuan)
	}
	fen := math.Round(v * 100)
	if math.Abs(fen-v*100) > 1e-6 || fen <= 0 {
		return 0, fmt.Errorf("invalid amount %q", yuan)
	}
	return int64(fen), nil
}

// Yuan 把分格式化为两位小数的元。
func Yuan(fen int64) string {
	return fmt.Sprintf("%d.%02d", fen/100, fen%100)
}

// China 是渠道接口使用的时区（北京时间）。
var China = time.FixedZone("CST", 8*3600)
