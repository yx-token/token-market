package registry

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/hostpay"
	"github.com/yx-token/token-market/market/internal/payment"
)

func validKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return payment.EncodePrivateKeyPEM(key), payment.EncodePublicKeyPEM(&key.PublicKey)
}

func TestStoreBuildsSortedInstancesAndKeepsDisabledForHistory(t *testing.T) {
	privateKey, publicKey := validKeys(t)
	snapshot := &hostpay.Config{Enabled: true, MinAmount: -1, MaxAmount: 0, OrderTimeoutMinutes: 1, Instances: []hostpay.Instance{
		{ID: 11, ProviderKey: hostpay.ProviderAlipay, Enabled: true, SortOrder: 1, SupportedTypes: "alipay",
			Config: map[string]string{"appId": "app", "privateKey": privateKey, "publicKey": publicKey, "notifyUrl": "http://notify.test:8090/a/b"}},
		{ID: 10, ProviderKey: hostpay.ProviderAlipay, Enabled: true, SortOrder: 1, SupportedTypes: "alipay",
			PaymentMode: "redirect", Config: map[string]string{"appId": "app", "privateKey": privateKey, "publicKey": publicKey, "notifyUrl": "https://pay.test/hook"}},
		{ID: 12, ProviderKey: hostpay.ProviderAlipay, Enabled: false, SortOrder: 0, SupportedTypes: "alipay",
			Config: map[string]string{"appId": "app", "privateKey": privateKey, "publicKey": publicKey, "notifyUrl": "https://old.test/hook"}},
		{ID: 13, ProviderKey: hostpay.ProviderAlipay, Enabled: true, SortOrder: -1, SupportedTypes: "alipay",
			Config: map[string]string{"appId": "missing-public-key", "privateKey": privateKey, "notifyUrl": "https://bad.test/hook"}},
		{ID: 20, ProviderKey: "stripe", Enabled: true, Config: map[string]string{"notifyUrl": "https://stripe.test"}},
	}}
	store := &Store{Load: func(context.Context) (*hostpay.Config, error) { return snapshot, nil }}
	reg, err := store.Registry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p, id := reg.Default(payment.ProviderAlipay); p == nil || id != 10 {
		t.Fatalf("default = (%v, %d), want instance 10", p, id)
	}
	if p, id := reg.Select(payment.ChannelAlipayPage); p == nil || id != 10 {
		t.Fatalf("page select = (%v, %d), want instance 10", p, id)
	}
	if p, id := reg.Select(payment.ChannelAlipayF2F); p == nil || id != 11 {
		t.Fatalf("f2f select = (%v, %d), want instance 11", p, id)
	}
	if reg.GetInstance(payment.ProviderAlipay, 12) == nil || reg.GetInstance(payment.ProviderAlipay, 13) != nil {
		t.Fatal("disabled but valid clients should be available to existing orders; invalid clients must be excluded")
	}
	if got := reg.Origin(payment.ProviderAlipay, 11); got != "http://notify.test:8090" {
		t.Fatalf("origin = %q", got)
	}
	if !reg.Configured()[payment.ChannelAlipayPage] || !reg.Configured()[payment.ChannelAlipayF2F] {
		t.Fatalf("configured = %#v", reg.Configured())
	}
	cfg, err := store.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinAmount != 1 || cfg.MaxAmount != 50000 || cfg.OrderTimeoutMinutes != 5 {
		t.Fatalf("normalized limits = %+v", cfg)
	}

	snapshot.Enabled = false
	store.Invalidate()
	reg, err = store.Registry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := reg.Default(payment.ProviderAlipay); p != nil || len(reg.Configured()) != 0 {
		t.Fatal("global host switch should stop new orders")
	}
	if reg.GetInstance(payment.ProviderAlipay, 12) == nil {
		t.Fatal("global switch must not disable historical order lookup")
	}
}

func TestStoreNoSourceAndSnapshotFingerprint(t *testing.T) {
	store := &Store{}
	reg, err := store.Registry(context.Background())
	if err != nil || reg == nil || len(reg.Configured()) != 0 {
		t.Fatalf("missing host source should produce empty registry: reg=%v err=%v", reg, err)
	}
	cfg, err := store.Config(context.Background())
	if err != nil || cfg.MinAmount != 1 || cfg.MaxAmount != 50000 || cfg.OrderTimeoutMinutes != 30 {
		t.Fatalf("missing source limits: cfg=%+v err=%v", cfg, err)
	}

	privateKey, publicKey := validKeys(t)
	instance := hostpay.Instance{ID: 1, ProviderKey: hostpay.ProviderAlipay, Enabled: true, SupportedTypes: "alipay", UpdatedAt: time.Now(),
		Config: map[string]string{"appId": "app", "privateKey": privateKey, "publicKey": publicKey, "notifyUrl": "https://notify.test"}}
	wechat := hostpay.Instance{ID: 2, ProviderKey: hostpay.ProviderWxpay, Enabled: true, SupportedTypes: "wxpay", UpdatedAt: time.Now(),
		Config: map[string]string{"appId": "wx-app", "mchId": "mch", "certSerial": "serial", "privateKey": privateKey,
			"apiV3Key": "12345678901234567890123456789012", "publicKeyId": "PUB_KEY_ID_1", "publicKey": publicKey,
			"notifyUrl": "https://notify.test"}}
	var calls int
	store = &Store{Load: func(context.Context) (*hostpay.Config, error) {
		calls++
		return &hostpay.Config{Enabled: true, Instances: []hostpay.Instance{instance, wechat}}, nil
	}}
	reg, err = store.Registry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client := reg.GetInstance(payment.ProviderAlipay, 1)
	wechatClient := reg.GetInstance(payment.ProviderWechat, 2)
	if client == nil {
		t.Fatal("first instance client missing")
	}
	if wechatClient == nil {
		t.Fatal("wechat instance client missing")
	}
	reg, err = store.Registry(context.Background())
	if err != nil || reg.GetInstance(payment.ProviderAlipay, 1) != client || reg.GetInstance(payment.ProviderWechat, 2) != wechatClient || calls != 1 {
		t.Fatalf("cache should reuse client: err=%v calls=%d", err, calls)
	}
	store.Invalidate()
	reg, err = store.Registry(context.Background())
	if err != nil || reg.GetInstance(payment.ProviderAlipay, 1) != client || reg.GetInstance(payment.ProviderWechat, 2) != wechatClient || calls != 2 {
		t.Fatalf("unchanged fingerprint should reuse client: err=%v calls=%d", err, calls)
	}
	instance.UpdatedAt = instance.UpdatedAt.Add(time.Second)
	store.Load = func(context.Context) (*hostpay.Config, error) {
		calls++
		return &hostpay.Config{Enabled: true, MinAmount: 5, Instances: []hostpay.Instance{instance, wechat}}, nil
	}
	store.Invalidate()
	reg, err = store.Registry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reg.GetInstance(payment.ProviderAlipay, 1) == client || reg.GetInstance(payment.ProviderWechat, 2) != wechatClient || calls != 3 {
		t.Fatalf("only changed instance should rebuild its client: calls=%d", calls)
	}
}

func TestSupportedMethodAcceptsSub2APIDirectAliases(t *testing.T) {
	for _, tc := range []struct {
		provider string
		support  string
		want     string
	}{
		{hostpay.ProviderAlipay, "alipay_direct", "alipay"},
		{hostpay.ProviderWxpay, "wxpay_direct", "wxpay"},
	} {
		if got := supportedMethod(tc.support, tc.provider); !got[tc.want] {
			t.Errorf("supportedMethod(%q, %q) = %#v", tc.support, tc.provider, got)
		}
	}
}
