package app

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/DueGin/token-market/internal/payment"
	"github.com/DueGin/token-market/internal/payment/alipay"
	"github.com/DueGin/token-market/internal/payment/wechat"
	"github.com/DueGin/token-market/internal/platform/config"
)

// BuildPayments 按配置创建支付渠道；没有配置的渠道不创建（只剩人工确认）。
func BuildPayments(cfg config.PaymentConfig) (*payment.Registry, error) {
	var providers []payment.Provider
	if cfg.Alipay.AppID != "" {
		p, err := buildAlipay(cfg)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	if cfg.Wechat.MchID != "" {
		p, err := buildWechat(cfg)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return payment.NewRegistry(providers...), nil
}

func buildAlipay(cfg config.PaymentConfig) (*alipay.Client, error) {
	a := cfg.Alipay
	priv, err := loadPrivateKey(a.PrivateKey, a.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("alipay: %w", err)
	}
	c := alipay.Config{AppID: a.AppID, Gateway: a.Gateway, PrivateKey: priv, Channels: a.Channels,
		NotifyURL: cfg.PublicURL + "/api/payments/alipay/notify"}
	if a.AppCertFile != "" || a.AlipayCertFile != "" || a.AlipayRootCertFile != "" {
		// 公钥证书模式
		if a.AppCertFile == "" || a.AlipayCertFile == "" || a.AlipayRootCertFile == "" {
			return nil, errors.New("alipay: certificate mode needs app_cert_file, alipay_cert_file and alipay_root_cert_file")
		}
		appCert, err := readCert(a.AppCertFile)
		if err != nil {
			return nil, fmt.Errorf("alipay app certificate: %w", err)
		}
		alipayCert, err := readCert(a.AlipayCertFile)
		if err != nil {
			return nil, fmt.Errorf("alipay public key certificate: %w", err)
		}
		pk, ok := alipayCert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("alipay public key certificate is not RSA")
		}
		root, err := payment.ReadKeyMaterial("", a.AlipayRootCertFile)
		if err != nil {
			return nil, err
		}
		if c.AlipayRootCertSN, err = alipay.RootCertSN(root); err != nil {
			return nil, err
		}
		c.AppCertSN, c.AlipayPublicKey = alipay.CertSN(appCert), pk
	} else {
		raw, err := payment.ReadKeyMaterial(a.AlipayPublicKey, a.AlipayPublicKeyFile)
		if err != nil {
			return nil, err
		}
		if raw == "" {
			return nil, errors.New("alipay: alipay_public_key (or certificate files) is required")
		}
		if c.AlipayPublicKey, err = payment.ParsePublicKey(raw); err != nil {
			return nil, fmt.Errorf("alipay public key: %w", err)
		}
	}
	return alipay.New(c)
}

func buildWechat(cfg config.PaymentConfig) (*wechat.Client, error) {
	w := cfg.Wechat
	priv, err := loadPrivateKey(w.PrivateKey, w.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("wechat: %w", err)
	}
	c := wechat.Config{AppID: w.AppID, MchID: w.MchID, MchSerialNo: w.MchSerialNo, PrivateKey: priv, APIv3Key: w.APIv3Key,
		BaseURL: w.BaseURL, NotifyURL: cfg.PublicURL + "/api/payments/wechat/notify"}
	raw, err := payment.ReadKeyMaterial(w.PublicKey, w.PublicKeyFile)
	if err != nil {
		return nil, err
	}
	if raw != "" || w.PublicKeyID != "" {
		if raw == "" || w.PublicKeyID == "" {
			return nil, errors.New("wechat: public_key_id and public key must be configured together")
		}
		if c.PublicKey, err = payment.ParsePublicKey(raw); err != nil {
			return nil, fmt.Errorf("wechat public key: %w", err)
		}
		c.PublicKeyID = w.PublicKeyID
	}
	return wechat.New(c)
}

func loadPrivateKey(inline, file string) (*rsa.PrivateKey, error) {
	raw, err := payment.ReadKeyMaterial(inline, file)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, errors.New("merchant private key is required")
	}
	return payment.ParsePrivateKey(raw)
}

func readCert(file string) (*x509.Certificate, error) {
	raw, err := payment.ReadKeyMaterial("", file)
	if err != nil {
		return nil, err
	}
	return payment.ParseCertificate(raw)
}
