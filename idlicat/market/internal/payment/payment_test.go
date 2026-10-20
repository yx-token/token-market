package payment

import "testing"

func TestFenAndYuan(t *testing.T) {
	for in, want := range map[string]int64{"12.34": 1234, "0.01": 1, "9.9": 990, "100": 10000, " 66.00 ": 6600} {
		if got, err := Fen(in); err != nil || got != want {
			t.Errorf("Fen(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "-1", "1.234", "abc", ""} {
		if _, err := Fen(bad); err == nil {
			t.Errorf("Fen(%q) should fail", bad)
		}
	}
	if Yuan(1234) != "12.34" || Yuan(5) != "0.05" || Yuan(10000) != "100.00" {
		t.Fatal("Yuan formatting")
	}
}

func TestProviderOf(t *testing.T) {
	if ProviderOf(ChannelAlipayF2F) != ProviderAlipay || ProviderOf(ChannelWechatNative) != ProviderWechat ||
		ProviderOf(ChannelManual) != ProviderManual || ProviderOf("x") != "" {
		t.Fatal("ProviderOf")
	}
}
