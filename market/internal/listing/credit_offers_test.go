package listing

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func TestCreditOfferMoneyValidation(t *testing.T) {
	for _, amount := range []float64{0.01, 1, 1.23, 999999999.99, 1e9} {
		if !validCreditOfferMoney(amount) {
			t.Errorf("valid amount %v rejected", amount)
		}
	}
	for _, amount := range []float64{0, -1, 0.001, 1.001, 999999999.999, 1e9 + 0.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if validCreditOfferMoney(amount) {
			t.Errorf("invalid amount %v accepted", amount)
		}
	}
}

func TestNormalizeCreditOfferInput(t *testing.T) {
	in := CreditOfferInput{
		Title: "  店铺活动  ", Description: "  限本店使用  ", PriceCNY: 30.01, FaceValueUSD: 10,
		AllowedPlatforms: []string{" openai ", "openai", "anthropic"},
		AllowedModels:    []string{" gpt-6-astra ", "gpt-6-astra", "GPT-6-astra"},
	}
	out, err := normalizeCreditOfferInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "店铺活动" || out.Description != "限本店使用" {
		t.Fatalf("untrimmed metadata: %+v", out)
	}
	if !reflect.DeepEqual(out.AllowedPlatforms, []string{"openai", "anthropic"}) ||
		!reflect.DeepEqual(out.AllowedModels, []string{"gpt-6-astra", "GPT-6-astra"}) {
		t.Fatalf("scope must deduplicate without changing exact model identity: %+v", out)
	}
	in.AllowedPlatforms, in.AllowedModels = nil, nil
	out, err = normalizeCreditOfferInput(in)
	if err != nil || out.AllowedPlatforms == nil || out.AllowedModels == nil {
		t.Fatalf("unrestricted scopes must be JSON arrays: %+v, %v", out, err)
	}
}

func TestCreditOfferRejectsInvalidInput(t *testing.T) {
	valid := func() CreditOfferInput { return CreditOfferInput{Title: "活动", PriceCNY: 1, FaceValueUSD: 10} }
	tests := []struct {
		name string
		edit func(*CreditOfferInput)
		code string
	}{
		{"blank title", func(in *CreditOfferInput) { in.Title = "  " }, "INVALID_CREDIT_TITLE"},
		{"long title", func(in *CreditOfferInput) { in.Title = strings.Repeat("中", 121) }, "INVALID_CREDIT_TITLE"},
		{"long description", func(in *CreditOfferInput) { in.Description = strings.Repeat("中", 4001) }, "INVALID_CREDIT_DESCRIPTION"},
		{"zero price", func(in *CreditOfferInput) { in.PriceCNY = 0 }, "INVALID_CREDIT_AMOUNT"},
		{"rounded face", func(in *CreditOfferInput) { in.FaceValueUSD = 10.001 }, "INVALID_CREDIT_AMOUNT"},
		{"unknown status", func(in *CreditOfferInput) { in.Status = "deleted" }, "INVALID_CREDIT_STATUS"},
		{"empty platform", func(in *CreditOfferInput) { in.AllowedPlatforms = []string{" "} }, "INVALID_CREDIT_SCOPE"},
		{"model glob", func(in *CreditOfferInput) { in.AllowedModels = []string{"gpt-*"} }, "INVALID_CREDIT_SCOPE"},
		{"model wildcard", func(in *CreditOfferInput) { in.AllowedModels = []string{"gpt-?"} }, "INVALID_CREDIT_SCOPE"},
		{"model newline", func(in *CreditOfferInput) { in.AllowedModels = []string{"gpt\n6"} }, "INVALID_CREDIT_SCOPE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid()
			tt.edit(&in)
			_, err := normalizeCreditOfferInput(in)
			var he *httpx.Error
			if !errors.As(err, &he) || he.Code != tt.code {
				t.Fatalf("error = %v, want %s", err, tt.code)
			}
		})
	}
}
