package market

import (
	"encoding/json"
	"testing"
)

func TestMigrateV1ToProfiles(t *testing.T) {
	old := RecommendConfig{
		Version: 1,
		Slots: []RecommendSlot{
			{ID: "deal", Title: "低倍率", Desc: "d", Eng: "DEAL", Theme: "deal", Enabled: true, Sort: 10, Boost: 1, Kind: "tag", KindVal: "low_rate", ActionTag: "low_rate"},
		},
		Supply:               &SupplyWeights{Price: 2, Grade: 1, Success: 1, TTFT: 0.5, Stock: 0.5, Fresh: 0.3},
		Platform:             &PlatformWeights{Explore: 0.1},
		LowRateMaxMultiplier: 0.5,
		PerTileSKU:           3,
		MaxTiles:             4,
		MinSKUToShowTile:     1,
		FreshDays:            5,
		DiversityPerPlat:     2,
		HideEmptyTiles:       true,
	}
	raw, _ := json.Marshal(old)
	var loaded RecommendConfig
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	loaded.normalize()
	if loaded.Version != 2 {
		t.Fatalf("version %d", loaded.Version)
	}
	if len(loaded.Profiles) != 1 || loaded.Profiles[0].ID != "algo_deal" {
		t.Fatalf("profiles %#v", loaded.Profiles)
	}
	if len(loaded.Tiles) != 1 || loaded.Tiles[0].ProfileID != "algo_deal" {
		t.Fatalf("tiles %#v", loaded.Tiles)
	}
	if loaded.Profiles[0].Weights["price"] != 2 {
		t.Fatalf("weight price %v", loaded.Profiles[0].Weights["price"])
	}
	if err := loaded.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultRecommendValid(t *testing.T) {
	cfg := DefaultRecommend()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
