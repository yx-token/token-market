package buyer

import "context"

// PackPaymentInfo 在线支付指引（与充值订单 payment 字段对齐）。
type PackPaymentInfo struct {
	Channel      string `json:"channel"`
	Kind         string `json:"kind"`
	URL          string `json:"url,omitempty"`
	QRCode       string `json:"qr_code,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// PackPayOrder 额度包在线支付订单摘要。
type PackPayOrder struct {
	ID      int64            `json:"id"`
	Amount  float64          `json:"amount"`
	Channel string           `json:"channel"`
	Status  string           `json:"status"`
	Payment *PackPaymentInfo `json:"payment,omitempty"`
}

// PackFunds 额度包资金钩子（由 billing.Funds 实现），避免 buyer ↔ billing 循环依赖。
type PackFunds interface {
	RequirePackBalance(ctx context.Context, userID int64, face float64) error
	PostPackPurchase(ctx context.Context, userID, packID int64, face float64) error
	PostPackRefund(ctx context.Context, userID, packID int64, amount float64) error
	StartPackPayment(ctx context.Context, userID int64, amount float64, channel, ip string) (*PackPayOrder, error)
	CancelRecharge(ctx context.Context, userID, orderID int64) error
	PostPackRefundOriginal(ctx context.Context, userID, packID int64, amount float64, rechargeID *int64, channel string) error
}
