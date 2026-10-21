package merchants

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/payment"
)

func cert(t *testing.T, cn string) (string, *rsa.PrivateKey) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), key
}

func TestBuildAlipay(t *testing.T) {
	app, _ := rsa.GenerateKey(rand.Reader, 2048)
	priv := alipaySecrets{PrivateKey: payment.EncodePrivateKeyPEM(app)}
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	if _, err := buildAlipay(AlipayConfig{AppID: "1", AlipayPublicKey: payment.EncodePublicKeyPEM(&platform.PublicKey)}, priv, "https://x/n", nil); err != nil {
		t.Fatalf("public key mode: %v", err)
	}
	appCert, _ := cert(t, "app")
	alipayCert, _ := cert(t, "alipay")
	root1, _ := cert(t, "root1")
	root2, _ := cert(t, "root2")
	c, err := buildAlipay(AlipayConfig{AppID: "1", Mode: "cert", AppCert: appCert, AlipayCert: alipayCert, AlipayRootCert: root1 + root2}, priv, "https://x/n", nil)
	if err != nil || !c.CertMode() {
		t.Fatalf("cert mode: %v", err)
	}
	for _, tc := range []struct {
		cfg  AlipayConfig
		sec  alipaySecrets
		want string
	}{
		{AlipayConfig{}, priv, "APPID"},
		{AlipayConfig{AppID: "1"}, alipaySecrets{}, "私钥"},
		{AlipayConfig{AppID: "1"}, alipaySecrets{PrivateKey: "bad"}, "私钥无效"},
		{AlipayConfig{AppID: "1"}, priv, "支付宝公钥"},
		{AlipayConfig{AppID: "1", Mode: "cert", AppCert: appCert}, priv, "证书"},
	} {
		if _, err := buildAlipay(tc.cfg, tc.sec, "https://x/n", nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: got %v, want %q", tc.cfg, err, tc.want)
		}
	}
}

func TestBuildWechat(t *testing.T) {
	m, _ := rsa.GenerateKey(rand.Reader, 2048)
	sec := wechatSecrets{PrivateKey: payment.EncodePrivateKeyPEM(m), APIv3Key: strings.Repeat("k", 32)}
	base := WechatConfig{AppID: "wx", MchID: "1", MchSerialNo: "S"}
	if _, err := buildWechat(base, sec, "https://x/n", nil); err != nil {
		t.Fatalf("platform certificate mode: %v", err)
	}
	withKey := base
	withKey.PublicKeyID, withKey.PublicKey = "PUB_KEY_ID_1", payment.EncodePublicKeyPEM(&m.PublicKey)
	if _, err := buildWechat(withKey, sec, "https://x/n", nil); err != nil {
		t.Fatalf("public key mode: %v", err)
	}
	half := base
	half.PublicKeyID = "PUB_KEY_ID_1"
	short := sec
	short.APIv3Key = "short"
	for _, tc := range []struct {
		cfg  WechatConfig
		sec  wechatSecrets
		want string
	}{
		{WechatConfig{}, sec, "商户号"},
		{WechatConfig{AppID: "wx", MchID: "1"}, sec, "序列号"},
		{base, wechatSecrets{APIv3Key: sec.APIv3Key}, "私钥"},
		{base, short, "32"},
		{half, sec, "同时填写"},
	} {
		if _, err := buildWechat(tc.cfg, tc.sec, "https://x/n", nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: got %v, want %q", tc.cfg, err, tc.want)
		}
	}
	if _, err := notifyURL("", "alipay"); err == nil {
		t.Error("notify url requires public url")
	}
}
