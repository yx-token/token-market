package listing

import "testing"

func TestValidatePackInput(t *testing.T) {
	m, face, stock := 0.55, 100.0, 3
	join := true
	ok := CreateListingInput{Mode: ModePack, Multiplier: &m, FaceValue: &face, Stock: &stock}
	if err := validatePackInput(ok); err != nil {
		t.Fatalf("valid pack rejected: %v", err)
	}
	if err := validatePackInput(CreateListingInput{Mode: ModePack, FaceValue: &face, Stock: &stock}); err == nil {
		t.Fatal("missing multiplier accepted")
	}
	badFace := 5.0
	if err := validatePackInput(CreateListingInput{Mode: ModePack, Multiplier: &m, FaceValue: &badFace, Stock: &stock}); err == nil {
		t.Fatal("face below 10 accepted")
	}
	badStock := 0
	if err := validatePackInput(CreateListingInput{Mode: ModePack, Multiplier: &m, FaceValue: &face, Stock: &badStock}); err == nil {
		t.Fatal("zero stock accepted")
	}
	if err := validatePackInput(CreateListingInput{Mode: ModePack, Multiplier: &m, FaceValue: &face, Stock: &stock, JoinDefaultPool: &join}); err == nil {
		t.Fatal("pack joining default pool accepted")
	}
}

func TestPackStockLeftAndRefundable(t *testing.T) {
	stock := 3
	l := &Listing{SKUKind: "pack", Stock: &stock, StockSold: 1}
	if PackStockLeft(l) != 2 {
		t.Fatalf("left=%d", PackStockLeft(l))
	}
	p := &QuotaPack{Status: "active", Remaining: 37}
	if RefundableAmount(p) != 37 {
		t.Fatalf("refundable=%v", RefundableAmount(p))
	}
	p.Status = "refunded"
	if RefundableAmount(p) != 0 {
		t.Fatal("refunded pack still refundable")
	}
}
