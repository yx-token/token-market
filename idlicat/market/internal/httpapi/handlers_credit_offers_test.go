package httpapi

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

func TestMarketCreditOffersRequiresSeller(t *testing.T) {
	server := &Server{}
	for _, query := range []string{"", "?seller_id=0", "?seller_id=-1", "?seller_id=abc", "?seller_id=9223372036854775808"} {
		r := httptest.NewRequest("GET", "/api/market/credit-offers"+query, nil)
		err := server.marketCreditOffers(httptest.NewRecorder(), r)
		var he *httpx.Error
		if !errors.As(err, &he) || he.Status != 400 {
			t.Fatalf("invalid seller filter %q: %v", query, err)
		}
	}
}

func TestCreateCreditOfferRejectsOwnershipInjection(t *testing.T) {
	server := &Server{}
	r := httptest.NewRequest("POST", "/api/seller/credit-offers", strings.NewReader(
		`{"seller_id":123,"title":"活动","price_cny":30,"face_value_usd":10}`))
	err := server.createCreditOffer(httptest.NewRecorder(), r)
	var he *httpx.Error
	if !errors.As(err, &he) || he.Code != "INVALID_JSON" {
		t.Fatalf("seller_id override should be rejected before persistence: %v", err)
	}
}
