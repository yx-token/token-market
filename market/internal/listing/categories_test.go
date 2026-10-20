package listing

import "testing"

func TestNormalizeCategories(t *testing.T) {
	allowed := map[string]bool{"low_rate": true, "coding": true, "featured": true}
	cats, err := NormalizeCategories(ModeConsignment, []string{"low_rate", "coding", "low_rate", "pack"}, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 || cats[0] != "coding" || cats[1] != "low_rate" {
		t.Fatalf("got %#v", cats)
	}
	if err := requireSellerCategory(cats); err != nil {
		t.Fatal(err)
	}

	packCats, err := NormalizeCategories(ModePack, []string{"featured"}, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if !HasCategory(packCats, CategoryPack) || !HasCategory(packCats, "featured") {
		t.Fatalf("pack cats %#v", packCats)
	}

	if _, err := NormalizeCategories(ModeConsignment, []string{"nope"}, allowed); err == nil {
		t.Fatal("unknown category accepted")
	}
	if err := requireSellerCategory([]string{CategoryPack}); err == nil {
		t.Fatal("pack-only categories should require seller pick")
	}
}

func TestValidCategoryID(t *testing.T) {
	if !validCategoryID("low_rate") || !validCategoryID("a1") {
		t.Fatal("valid rejected")
	}
	if validCategoryID("Low") || validCategoryID("a-b") || validCategoryID("") {
		t.Fatal("invalid accepted")
	}
}
