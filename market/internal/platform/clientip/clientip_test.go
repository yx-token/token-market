package clientip

import "testing"

func TestClientIPResolver(t *testing.T) {
	r := New([]string{"10.0.0.0/8", "192.168.1.5"})
	cases := []struct {
		remote string
		xff    []string
		want   string
	}{
		{"203.0.113.9:5000", []string{"1.2.3.4"}, "203.0.113.9"},             // 直连方不受信任：忽略伪造的 XFF
		{"10.1.1.1:5000", []string{"1.2.3.4"}, "1.2.3.4"},                    // 来自负载均衡：采信 XFF
		{"10.1.1.1:5000", []string{"6.6.6.6, 1.2.3.4, 10.2.2.2"}, "1.2.3.4"}, // 从右往左取第一个不受信任的地址
		{"192.168.1.5:80", []string{"8.8.8.8", "10.0.0.7"}, "8.8.8.8"},       // 多个 XFF 头
		{"10.1.1.1:5000", nil, "10.1.1.1"},                                   // 没有 XFF
		{"10.1.1.1:5000", []string{"10.3.3.3"}, "10.1.1.1"},                  // 全是受信任地址
	}
	for _, c := range cases {
		if got := r.Resolve(c.remote, c.xff); got != c.want {
			t.Errorf("resolve(%s, %v) = %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
}
