package buyer

import "testing"

func TestPurchasePackRequiresListing(t *testing.T) {
	s := &KeyService{}
	_, err := s.PurchasePack(t.Context(), 1, PurchasePackInput{ListingID: 0, Channel: "balance"}, "")
	if err == nil {
		t.Fatal("empty listing should fail")
	}
}

func TestRefundPackDest(t *testing.T) {
	s := &KeyService{}
	_, err := s.RefundPack(t.Context(), 1, 1, "wallet")
	if err == nil {
		t.Fatal("unsupported dest should fail before DB")
	}
}
