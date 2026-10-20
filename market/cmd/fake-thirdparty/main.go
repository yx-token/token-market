// fake-thirdparty 是开发与演示用的第三方服务替身：支付宝（电脑网站支付收银台、当面付扫码页）、微信支付（Native 扫码页）
// 与 BazaarLink Probe API。按真实协议签名与验签，不产生任何真实扣款或真实检测。不能用于生产。
//
// 用法：
//
//	fake-thirdparty --keys-dir data/dev/pay-keys [--listen 127.0.0.1:18082] [--public-url http://127.0.0.1:18082]
//
// 首次启动在 keys-dir 生成密钥，并写出 merchant-alipay.json、merchant-wechat.json：
// 管理后台"支付配置"接口（PUT /api/admin/payments/merchants/{alipay|wechat}）的请求体，接入这个替身（scripts/dev.sh 会自动提交）。
package main

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/yx-token/idlicat/market/internal/payment"
	"github.com/yx-token/idlicat/market/internal/testutil/fakebazaar"
	"github.com/yx-token/idlicat/market/internal/testutil/fakepay"
)

// 与 scripts/dev.sh 中的商户配置一致
const (
	alipayAppID    = "2021000000000001"
	wechatAppID    = "wx0000000000dev01"
	wechatMchID    = "1900000001"
	wechatSerial   = "DEVMERCHANTSERIAL0001"
	wechatAPIv3Key = "devapiv3keydevapiv3keydevapiv3ke"
	wechatPubKeyID = "PUB_KEY_ID_0000000000000000000001"
)

func main() {
	keysDir := flag.String("keys-dir", "data/dev/pay-keys", "密钥目录（不存在时生成）")
	listen := flag.String("listen", "127.0.0.1:18082", "监听地址")
	publicURL := flag.String("public-url", "", "浏览器访问替身的地址，默认 http://<listen>")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*keysDir, *listen, *publicURL, log); err != nil {
		log.Error("fake-thirdparty exited", "err", err)
		os.Exit(1)
	}
}

func run(keysDir, listen, publicURL string, log *slog.Logger) error {
	keys, err := loadKeys(keysDir)
	if err != nil {
		return err
	}
	if publicURL == "" {
		publicURL = "http://" + listen
	}
	if err := writeMerchants(keysDir, publicURL); err != nil {
		return err
	}
	pay := fakepay.NewUnstarted(fakepay.Config{Keys: keys, AlipayAppID: alipayAppID, WechatAppID: wechatAppID, WechatMchID: wechatMchID,
		WechatMchSerial: wechatSerial, PublicURL: publicURL, Log: log})
	bazaar := fakebazaar.NewUnstarted()
	bazaar.Delay = 2 * time.Second // 让检测在页面上能看到"检测中"

	mux := http.NewServeMux()
	mux.Handle("/api/probe/", bazaar.Handler())
	mux.Handle("/", pay.Handler())
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("fake-thirdparty listening", "addr", listen, "alipay_gateway", pay.AlipayGateway(), "wechat_base_url", pay.WechatBaseURL())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loadKeys 读取或生成密钥：商户私钥与平台公钥给 token-market，其余给替身。
func loadKeys(dir string) (fakepay.Keys, error) {
	files := map[string]string{
		"alipay_app_private":      filepath.Join(dir, "alipay_app_private.pem"),
		"alipay_platform_private": filepath.Join(dir, "alipay_platform_private.pem"),
		"alipay_platform_public":  filepath.Join(dir, "alipay_platform_public.pem"),
		"wechat_merchant_private": filepath.Join(dir, "wechat_merchant_private.pem"),
		"wechat_platform_private": filepath.Join(dir, "wechat_platform_private.pem"),
		"wechat_platform_public":  filepath.Join(dir, "wechat_platform_public.pem"),
	}
	if _, err := os.Stat(files["alipay_app_private"]); errors.Is(err, os.ErrNotExist) {
		keys, app, merchant, err := fakepay.GenerateKeys()
		if err != nil {
			return fakepay.Keys{}, err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fakepay.Keys{}, err
		}
		content := map[string]string{
			"alipay_app_private":      payment.EncodePrivateKeyPEM(app),
			"alipay_platform_private": payment.EncodePrivateKeyPEM(keys.AlipayPlatform),
			"alipay_platform_public":  payment.EncodePublicKeyPEM(&keys.AlipayPlatform.PublicKey),
			"wechat_merchant_private": payment.EncodePrivateKeyPEM(merchant),
			"wechat_platform_private": payment.EncodePrivateKeyPEM(keys.WechatPlatform),
			"wechat_platform_public":  payment.EncodePublicKeyPEM(&keys.WechatPlatform.PublicKey),
		}
		for name, path := range files {
			if err := os.WriteFile(path, []byte(content[name]), 0o600); err != nil {
				return fakepay.Keys{}, err
			}
		}
	}
	read := func(name string) (*rsa.PrivateKey, error) {
		raw, err := os.ReadFile(files[name])
		if err != nil {
			return nil, err
		}
		return payment.ParsePrivateKey(string(raw))
	}
	app, err := read("alipay_app_private")
	if err != nil {
		return fakepay.Keys{}, err
	}
	alipayPlatform, err := read("alipay_platform_private")
	if err != nil {
		return fakepay.Keys{}, err
	}
	merchant, err := read("wechat_merchant_private")
	if err != nil {
		return fakepay.Keys{}, err
	}
	wechatPlatform, err := read("wechat_platform_private")
	if err != nil {
		return fakepay.Keys{}, err
	}
	return fakepay.Keys{AlipayPlatform: alipayPlatform, AlipayApp: &app.PublicKey, WechatPlatform: wechatPlatform,
		WechatMerchant: &merchant.PublicKey, WechatAPIv3Key: wechatAPIv3Key, WechatPublicKey: wechatPubKeyID}, nil
}

// writeMerchants 写出接入替身的商户配置（含商户私钥，仅供开发环境）。
func writeMerchants(dir, publicURL string) error {
	read := func(name string) (string, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		return string(b), err
	}
	appKey, err := read("alipay_app_private.pem")
	if err != nil {
		return err
	}
	alipayPub, err := read("alipay_platform_public.pem")
	if err != nil {
		return err
	}
	merchantKey, err := read("wechat_merchant_private.pem")
	if err != nil {
		return err
	}
	wechatPub, err := read("wechat_platform_public.pem")
	if err != nil {
		return err
	}
	bodies := map[string]any{
		"merchant-alipay.json": map[string]any{"enabled": true, "private_key": appKey, "alipay": map[string]any{
			"app_id": alipayAppID, "gateway": publicURL + "/gateway.do", "mode": "public_key", "alipay_public_key": alipayPub}},
		"merchant-wechat.json": map[string]any{"enabled": true, "private_key": merchantKey, "api_v3_key": wechatAPIv3Key, "wechat": map[string]any{
			"app_id": wechatAppID, "mch_id": wechatMchID, "mch_serial_no": wechatSerial, "public_key_id": wechatPubKeyID,
			"public_key": wechatPub, "base_url": publicURL}},
	}
	for name, body := range bodies {
		raw, _ := json.MarshalIndent(body, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			return err
		}
	}
	return nil
}
