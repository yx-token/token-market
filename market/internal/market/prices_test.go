package market

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

func TestPriceBookPrefersCatalog(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/channels/pricing/catalog", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "message": "success",
			"data": map[string]any{
				"models": []map[string]any{
					{"model": "a-model", "provider": "openai", "mode": "chat", "input_price": 1e-6, "output_price": 2e-6},
					{"model": "b-model", "provider": "anthropic", "mode": "chat", "input_price": 3e-6, "output_price": 4e-6},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/channels/model-pricing", func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("should not fall back to per-model pricing when catalog works")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	book := &PriceBook{
		Admin: adminclient.New(srv.URL, "k", time.Second),
		Models: func(context.Context) ([]string, error) {
			t.Fatal("Models shortlist should not be used")
			return nil, nil
		},
	}
	items, err := book.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Model != "a-model" || items[0].Provider != "openai" || items[0].InputPer1M != 1 {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
}

func TestPriceBookCatalogAlwaysRefetches(t *testing.T) {
	calls := 0
	models := []map[string]any{
		{"model": "a-model", "provider": "openai", "mode": "chat", "input_price": 1e-6, "output_price": 2e-6},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/channels/pricing/catalog", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "message": "success",
			"data": map[string]any{"models": models},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	book := &PriceBook{
		Admin: adminclient.New(srv.URL, "k", time.Second),
		TTL:   time.Hour, // 即使设很长，catalog 也要每次现取
	}
	if _, err := book.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	models = append(models, map[string]any{
		"model": "b-model", "provider": "anthropic", "mode": "chat", "input_price": 3e-6, "output_price": 4e-6,
	})
	items, err := book.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("catalog calls=%d, want >= 2 (must refetch after channel pricing change)", calls)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items after catalog change, want 2", len(items))
	}
}

func TestPriceBookInvalidate(t *testing.T) {
	book := &PriceBook{items: []Price{{Model: "stale"}}, at: time.Now(), sourceCatalog: true}
	book.Invalidate()
	if book.items != nil || !book.at.IsZero() || book.sourceCatalog {
		t.Fatalf("Invalidate did not clear cache: items=%v at=%v catalog=%v", book.items, book.at, book.sourceCatalog)
	}
}
