package payment

import "testing"

type registryProvider struct {
	Provider
	name     string
	channels []string
}

func (p *registryProvider) Name() string       { return p.name }
func (p *registryProvider) Channels() []string { return p.channels }

func TestInstanceRegistrySelectsEnabledDefaultAndKeepsDisabledInstances(t *testing.T) {
	disabled := &registryProvider{name: ProviderAlipay, channels: []string{ChannelAlipayPage}}
	first := &registryProvider{name: ProviderAlipay, channels: []string{ChannelAlipayPage}}
	second := &registryProvider{name: ProviderAlipay, channels: []string{ChannelAlipayPage, ChannelAlipayF2F}}
	r := NewInstanceRegistry(
		Instance{ID: 1, Provider: disabled, Enabled: false, Origin: "https://old.example"},
		Instance{ID: 5, Provider: second, Enabled: true, SortOrder: 2},
		Instance{ID: 3, Provider: first, Enabled: true, SortOrder: 2, Origin: "https://new.example"},
	)
	p, id := r.Default(ProviderAlipay)
	if p != first || id != 3 || r.Get(ProviderAlipay) != first || r.GetInstance(ProviderAlipay, 0) != first {
		t.Fatalf("wrong default: provider=%v id=%d", p, id)
	}
	if r.GetInstance(ProviderAlipay, 1) != disabled || r.GetInstance(ProviderWechat, 1) != nil || r.GetInstance(ProviderAlipay, 99) != nil {
		t.Fatal("instance lookup must retain disabled clients, check provider, and never fall back for missing IDs")
	}
	if r.Origin(ProviderAlipay, 0) != "https://new.example" || r.Origin(ProviderAlipay, 1) != "https://old.example" {
		t.Fatal("instance origin lookup")
	}
	if p, id := r.Select(ChannelAlipayF2F); p != second || id != 5 || !r.Configured()[ChannelAlipayF2F] {
		t.Fatal("channel selection must use the first enabled instance offering that channel")
	}
	r.SetEnabled(false)
	if p, _ := r.Default(ProviderAlipay); p != nil || len(r.Configured()) != 0 || r.Enabled(ProviderAlipay) {
		t.Fatal("global switch must disable new orders")
	}
	if r.GetInstance(ProviderAlipay, 1) != disabled || r.Get(ProviderAlipay) != first {
		t.Fatal("global switch must retain clients for existing orders")
	}
}

func TestInstanceRegistryFallsBackToDisabledForLegacyOrders(t *testing.T) {
	p := &registryProvider{name: ProviderWechat, channels: []string{ChannelWechatNative}}
	r := NewInstanceRegistry(Instance{ID: 7, Provider: p})
	if r.GetInstance(ProviderWechat, 0) != p || r.Get(ProviderWechat) != p {
		t.Fatal("legacy orders need the configured client even when disabled")
	}
	if p, _ := r.Default(ProviderWechat); p != nil || len(r.Configured()) != 0 {
		t.Fatal("disabled instances must not accept new orders")
	}
}

func TestLegacyRegistryKeepsProviderAPI(t *testing.T) {
	p := &registryProvider{name: ProviderWechat, channels: []string{ChannelWechatNative}}
	r := NewRegistry(p)
	if got, id := r.Default(ProviderWechat); got != p || id != 0 || !r.Configured()[ChannelWechatNative] {
		t.Fatal("legacy registry should remain available")
	}
	r.Disable(ProviderWechat)
	if r.Enabled(ProviderWechat) || len(r.Configured()) != 0 || r.Get(ProviderWechat) != p {
		t.Fatal("Disable should retain existing-order access")
	}
}
