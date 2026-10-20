package gateway_test

import (
	"testing"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/gateway"
)

func TestHTTPSatisfiesAdmin(t *testing.T) {
	var _ gateway.Admin = (*adminclient.Client)(nil)
	if gateway.HTTP(nil) != nil {
		t.Fatal("HTTP(nil) must be nil")
	}
	c := &adminclient.Client{}
	if gateway.HTTP(c) == nil {
		t.Fatal("HTTP(client) must not be nil")
	}
}
