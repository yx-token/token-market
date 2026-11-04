package enginehost

import "testing"

func TestHostGatewayDefaultsToLocal8080(t *testing.T) {
	previous := hostGateway.Swap(nil)
	t.Cleanup(func() { hostGateway.Store(previous) })
	t.Setenv("TM_SUB2API_BASE_URL", "http://outside.invalid:1234")
	t.Setenv("TM_SUB2API_GATEWAY_URL", "http://outside.invalid:5678")
	if got := hostGatewayURL(); got != "http://127.0.0.1:8080" {
		t.Fatalf("fallback gateway = %q", got)
	}
}

func TestSetHostGatewayUsesHostListener(t *testing.T) {
	previous := hostGateway.Load()
	t.Cleanup(func() { hostGateway.Store(previous) })
	for _, test := range []struct {
		host string
		port int
		want string
	}{
		{"", 18280, "http://127.0.0.1:18280"},
		{"0.0.0.0", 18280, "http://127.0.0.1:18280"},
		{"::", 18280, "http://127.0.0.1:18280"},
		{"[::]", 18280, "http://127.0.0.1:18280"},
		{"127.0.0.1", 18280, "http://127.0.0.1:18280"},
		{"::1", 18280, "http://[::1]:18280"},
		{"[2001:db8::1]", 18280, "http://[2001:db8::1]:18280"},
		{"host.internal", 18280, "http://host.internal:18280"},
		{"", 0, "http://127.0.0.1:8080"},
	} {
		SetHostGateway(test.host, test.port)
		if got := hostGatewayURL(); got != test.want {
			t.Errorf("SetHostGateway(%q, %d) = %q, want %q", test.host, test.port, got, test.want)
		}
	}
}
