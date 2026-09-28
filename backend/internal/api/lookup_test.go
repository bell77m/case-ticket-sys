package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"ticket-app/internal/models"
)

// FR-I4: names are shown in the requested language, falling back to English.
func TestPickName_FRI4(t *testing.T) {
	n := models.Names{"en": "Line 1", "th": "ไลน์ 1", "my": ""}
	tests := []struct{ lang, want string }{
		{"th", "ไลน์ 1"},
		{"my", "Line 1"},    // empty translation falls back
		{"zh-CN", "Line 1"}, // missing translation falls back
		{"xx", "Line 1"},    // unknown language falls back
		{"", "Line 1"},
	}
	for _, tt := range tests {
		if got := pickName(n, tt.lang); got != tt.want {
			t.Errorf("pickName(%q) = %q, want %q", tt.lang, got, tt.want)
		}
	}
}

func TestBuildLocationTree(t *testing.T) {
	locs := []models.Location{
		{ID: 1, Building: models.Names{"en": "B"}, Floor: models.Names{"en": "2"}, Line: models.Names{"en": "L1"}},
		{ID: 2, Building: models.Names{"en": "A"}, Floor: models.Names{"en": "1"}, Line: models.Names{"en": "L2"}},
		{ID: 3, Building: models.Names{"en": "A"}, Floor: models.Names{"en": "1"}, Line: models.Names{"en": "L1"}},
	}
	tree := buildLocationTree(locs, "en")
	if len(tree) != 2 || tree[0].Name != "A" || tree[1].Name != "B" {
		t.Fatalf("buildings = %+v, want A then B", tree)
	}
	lines := tree[0].Floors[0].Lines
	if len(lines) != 2 || lines[0].Name != "L1" || lines[0].ID != 3 || lines[1].ID != 2 {
		t.Errorf("building A floor 1 lines = %+v, want L1(3), L2(2)", lines)
	}
}

// Handlers against the dev DB (migrated and seeded): Thai names, English fallback, active rows only.
func TestLookupHandlers_FRI4(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := models.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&Server{DB: db}).Routes(mux)

	get := func(path string, v any) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatal(err)
		}
	}

	var cats []namedItem
	get("/api/categories?lang=th", &cats)
	if len(cats) < 5 || cats[0].Name != "ฮาร์ดแวร์" {
		t.Errorf("categories th = %+v, want Thai names", cats)
	}
	get("/api/categories?lang=xx", &cats)
	if cats[0].Name != "Hardware" {
		t.Errorf("categories fallback = %q, want Hardware", cats[0].Name)
	}

	var tree []building
	get("/api/locations?lang=zh-CN", &tree)
	if len(tree) == 0 || tree[0].Name != "A栋" || len(tree[0].Floors) == 0 || len(tree[0].Floors[0].Lines) == 0 {
		t.Errorf("locations zh-CN = %+v", tree)
	}
}
