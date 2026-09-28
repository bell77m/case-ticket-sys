package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/models"
)

// names is a Names object with a different value in each language, all derived from en.
func names(en string) models.Names {
	return models.Names{"en": en, "zh-CN": "中文 " + en, "my": "မြန်မာ " + en, "th": "ไทย " + en}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func categoryPath(id int64) string { return "/api/staff/categories/" + strconv.FormatInt(id, 10) }
func locationPath(id int64) string { return "/api/staff/locations/" + strconv.FormatInt(id, 10) }

// getJSON sends GET with the session cookie (nil for public routes) and decodes a 200 answer into v.
func (e *testEnv) getJSON(path string, c *http.Cookie, v any) {
	e.t.Helper()
	rec := e.withCookie(http.MethodGet, path, c)
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), v) != nil {
		e.t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
}

// findLine returns the building, floor and line names of location id in a public tree.
func findLine(tree []building, id int64) (names []string, ok bool) {
	for _, b := range tree {
		for _, f := range b.Floors {
			for _, l := range f.Lines {
				if l.ID == id {
					return []string{b.Name, f.Name, l.Name}, true
				}
			}
		}
	}
	return nil, false
}

// FR-I4 (T2.11): a category is created with all four names, shown in each language on the public list while
// active, kept on the staff list when deactivated, and every change is audited once.
func TestCategoryAdmin_FRI4(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated()
	c := e.session(e.newStaff("Team Lead", true))
	since := e.lastAuditID()
	en := fmt.Sprintf("Scanners %d", time.Now().UnixNano())

	var cat adminCategory
	rec := e.sendJSON(http.MethodPost, "/api/staff/categories", c, mustJSON(map[string]any{"name": names("  " + en + " ")}))
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &cat) != nil {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	want := names(en)
	want["zh-CN"], want["my"], want["th"] = "中文   "+en, "မြန်မာ   "+en, "ไทย   "+en // trimmed at the ends only
	if !cat.IsActive || !maps.Equal(cat.Name, want) {
		t.Fatalf("created %+v, want active %v", cat, want)
	}
	thai := maps.Clone(cat.Name)
	thai["th"] = "สแกนเนอร์"
	renamed := names(en + " 2")

	steps := []struct {
		name  string
		patch any // nil: no request
	}{
		{"created", nil},
		{"deactivated", map[string]any{"is_active": false}},
		{"no change", map[string]any{"is_active": false, "name": cat.Name}},
		{"reactivated", map[string]any{"is_active": true}},
		{"Thai only", map[string]any{"name": thai}},
		{"renamed", map[string]any{"name": renamed}},
	}
	for _, s := range steps {
		if s.patch != nil {
			rec := e.sendJSON(http.MethodPatch, categoryPath(cat.ID), c, mustJSON(s.patch))
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &cat) != nil {
				t.Fatalf("%s: PATCH = %d %s", s.name, rec.Code, rec.Body)
			}
		}
		for lang := range languages {
			var pub []namedItem
			e.getJSON("/api/categories?lang="+lang, nil, &pub)
			i := slices.IndexFunc(pub, func(n namedItem) bool { return n.ID == cat.ID })
			if cat.IsActive != (i >= 0) || i >= 0 && pub[i].Name != cat.Name[lang] {
				t.Errorf("%s: public %s list has %v at %d, want %q active=%v", s.name, lang, pub, i, cat.Name[lang], cat.IsActive)
			}
		}
		var all []adminCategory
		e.getJSON("/api/staff/categories", c, &all)
		if !slices.ContainsFunc(all, func(a adminCategory) bool {
			return a.ID == cat.ID && a.IsActive == cat.IsActive && maps.Equal(a.Name, cat.Name)
		}) {
			t.Errorf("%s: staff list misses %+v", s.name, cat)
		}
	}

	if got, want := e.staffAudits(en, since), []string{"category.created", "category.deactivated", "category.reactivated",
		"category.changed " + en + ">" + en}; !slices.Equal(got, want) {
		t.Errorf("audit %s = %q, want %q", en, got, want)
	}
	if got, want := e.staffAudits(en+" 2", since), []string{"category.changed " + en + ">" + en + " 2"}; !slices.Equal(got, want) {
		t.Errorf("audit rename = %q, want %q", got, want)
	}
}

// FR-I4 (T2.11): a location (building / floor / line) is created with all four names per part, shown in each
// language in the public tree while active, kept on the staff list when deactivated, and audited.
func TestLocationAdmin_FRI4(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated()
	c := e.session(e.newStaff("Team Lead", true))
	since := e.lastAuditID()
	b := fmt.Sprintf("Plant %d", time.Now().UnixNano())
	label := b + " / 3 / Line 7"

	var loc adminLocation
	body := mustJSON(map[string]any{"building": names(b), "floor": names("3"), "line": names("Line 7")})
	rec := e.sendJSON(http.MethodPost, "/api/staff/locations", c, body)
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &loc) != nil {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if !loc.IsActive || !maps.Equal(loc.Building, names(b)) || !maps.Equal(loc.Floor, names("3")) || !maps.Equal(loc.Line, names("Line 7")) {
		t.Fatalf("created %+v", loc)
	}
	burmese := maps.Clone(loc.Line)
	burmese["my"] = "လိုင်း ၇"

	steps := []struct {
		name  string
		patch any
	}{
		{"created", nil},
		{"deactivated", map[string]any{"is_active": false}},
		{"no change", map[string]any{"is_active": false, "floor": loc.Floor}},
		{"reactivated", map[string]any{"is_active": true}},
		{"Burmese only", map[string]any{"line": burmese}},
		{"floor renamed", map[string]any{"floor": names("4")}},
	}
	for _, s := range steps {
		if s.patch != nil {
			rec := e.sendJSON(http.MethodPatch, locationPath(loc.ID), c, mustJSON(s.patch))
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &loc) != nil {
				t.Fatalf("%s: PATCH = %d %s", s.name, rec.Code, rec.Body)
			}
		}
		for lang := range languages {
			var tree []building
			e.getJSON("/api/locations?lang="+lang, nil, &tree)
			got, found := findLine(tree, loc.ID)
			want := []string{loc.Building[lang], loc.Floor[lang], loc.Line[lang]}
			if found != loc.IsActive || found && !slices.Equal(got, want) {
				t.Errorf("%s: public %s tree has %q (found %v), want %q active=%v", s.name, lang, got, found, want, loc.IsActive)
			}
		}
		var all []adminLocation
		e.getJSON("/api/staff/locations", c, &all)
		if !slices.ContainsFunc(all, func(a adminLocation) bool {
			return a.ID == loc.ID && a.IsActive == loc.IsActive && maps.Equal(a.Building, loc.Building) &&
				maps.Equal(a.Floor, loc.Floor) && maps.Equal(a.Line, loc.Line)
		}) {
			t.Errorf("%s: staff list misses %+v", s.name, loc)
		}
	}

	if got, want := e.staffAudits(label, since), []string{"location.created", "location.deactivated", "location.reactivated",
		"location.changed " + label + ">" + label}; !slices.Equal(got, want) {
		t.Errorf("audit %s = %q, want %q", label, got, want)
	}
	moved := b + " / 4 / Line 7"
	if got, want := e.staffAudits(moved, since), []string{"location.changed " + label + ">" + moved}; !slices.Equal(got, want) {
		t.Errorf("audit move = %q, want %q", got, want)
	}
}

// FR-I4: every name needs all four languages under the guest text rules; duplicates, unknown ids and bad bodies
// are refused, and no refused request writes anything.
func TestLookupAdminErrors_FRI4(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated()
	c := e.session(e.newStaff("Team Lead", true))
	spare := models.Category{Name: names("Spare category"), IsActive: true}
	spareLoc := models.Location{Building: names("Spare B"), Floor: names("1"), Line: names("L1"), IsActive: true}
	if err := e.db.Create(&spare).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Create(&spareLoc).Error; err != nil {
		t.Fatal(err)
	}
	since := e.lastAuditID()

	with := func(n models.Names, lang, v string) models.Names {
		n = maps.Clone(n)
		if v == "-" {
			delete(n, lang)
		} else {
			n[lang] = v
		}
		return n
	}
	ok := names("Fresh name")
	bidi := "Hard" + string(rune(0x202E)) + "ware"
	long := strings.Repeat("ก", 101)
	cat := func(n models.Names) string { return mustJSON(map[string]any{"name": n}) }
	loc := func(b, f, l models.Names) string {
		return mustJSON(map[string]any{"building": b, "floor": f, "line": l})
	}
	// The seeded location, in other translations and another case: still the same place.
	seeded := loc(names(strings.ToUpper(e.loc.Building["en"])), names(e.loc.Floor["en"]), names(e.loc.Line["en"]))

	tests := []struct {
		name, method, path, body string
		status                   int
		code                     string
		fields                   map[string]string
	}{
		{"missing language", "POST", "/api/staff/categories", cat(with(ok, "th", "-")), 400, "validation", map[string]string{"name.th": "required"}},
		{"blank value", "POST", "/api/staff/categories", cat(with(ok, "my", "   ")), 400, "validation", map[string]string{"name.my": "required"}},
		{"zero-width only", "POST", "/api/staff/categories", cat(with(ok, "th", string(rune(0x200B)))), 400, "validation", map[string]string{"name.th": "required"}},
		{"unknown language", "POST", "/api/staff/categories", cat(with(ok, "fr", "Nom")), 400, "validation", map[string]string{"name": "invalid"}},
		{"too long", "POST", "/api/staff/categories", cat(with(ok, "th", long)), 400, "validation", map[string]string{"name.th": "too_long"}},
		{"bidi character", "POST", "/api/staff/categories", cat(with(ok, "en", bidi)), 400, "validation", map[string]string{"name.en": "invalid_characters"}},
		{"no name", "POST", "/api/staff/categories", `{}`, 400, "validation",
			map[string]string{"name.en": "required", "name.zh-CN": "required", "name.my": "required", "name.th": "required"}},
		{"not a string", "POST", "/api/staff/categories", `{"name": {"en": 1}}`, 400, "invalid_body", nil},
		{"unknown field", "POST", "/api/staff/categories", `{"name": {}, "color": "red"}`, 400, "invalid_body", nil},
		{"duplicate", "POST", "/api/staff/categories", cat(names("hardware")), 409, "category.exists", nil},
		{"rename to duplicate", "PATCH", categoryPath(spare.ID), cat(names("HARDWARE")), 409, "category.exists", nil},
		{"bad PATCH value", "PATCH", categoryPath(spare.ID), cat(with(ok, "zh-CN", "")), 400, "validation", map[string]string{"name.zh-CN": "required"}},
		{"empty PATCH", "PATCH", categoryPath(spare.ID), `{}`, 400, "invalid_body", nil},
		{"unknown category", "PATCH", categoryPath(1 << 60), `{"is_active": false}`, 404, "category.not_found", nil},
		{"bad category id", "PATCH", "/api/staff/categories/abc", `{"is_active": false}`, 404, "category.not_found", nil},

		{"location part missing language", "POST", "/api/staff/locations", loc(ok, with(ok, "my", "-"), ok), 400, "validation", map[string]string{"floor.my": "required"}},
		{"location bidi", "POST", "/api/staff/locations", loc(with(ok, "th", bidi), ok, ok), 400, "validation", map[string]string{"building.th": "invalid_characters"}},
		{"location unknown language", "POST", "/api/staff/locations", loc(ok, ok, with(ok, "de", "Linie")), 400, "validation", map[string]string{"line": "invalid"}},
		{"location too long", "POST", "/api/staff/locations", loc(ok, ok, with(ok, "en", long)), 400, "validation", map[string]string{"line.en": "too_long"}},
		{"location no line", "POST", "/api/staff/locations", mustJSON(map[string]any{"building": ok, "floor": ok}), 400, "validation",
			map[string]string{"line.en": "required", "line.zh-CN": "required", "line.my": "required", "line.th": "required"}},
		{"location duplicate", "POST", "/api/staff/locations", seeded, 409, "location.exists", nil},
		{"location moved onto another", "PATCH", locationPath(spareLoc.ID), seeded, 409, "location.exists", nil},
		{"empty location PATCH", "PATCH", locationPath(spareLoc.ID), `{}`, 400, "invalid_body", nil},
		{"unknown location", "PATCH", locationPath(1 << 60), `{"is_active": false}`, 404, "location.not_found", nil},
	}
	for _, tt := range tests {
		rec := e.sendJSON(tt.method, tt.path, c, tt.body)
		code, fields := errorBody(rec)
		if rec.Code != tt.status || code != tt.code || !maps.Equal(fields, tt.fields) {
			t.Errorf("%s: %s %s = %d %s, want %d %s %v", tt.name, tt.method, tt.path, rec.Code, rec.Body, tt.status, tt.code, tt.fields)
		}
	}
	if id := e.lastAuditID(); id != since {
		t.Errorf("refused requests wrote audit rows up to %d (was %d)", id, since)
	}
}

// FR-R3: the admin lookup routes need category.manage. An Agent (without it) is refused on each; a Team Lead reads both lists.
func TestLookupAdminPermission_FRR3(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated()
	agent, lead := e.session(e.newStaff("Agent", true)), e.session(e.newStaff("Team Lead", true))
	var catID int64
	e.db.Raw("SELECT min(id) FROM categories").Scan(&catID)
	routes := []struct{ method, path, body string }{
		{"GET", "/api/staff/categories", ""},
		{"POST", "/api/staff/categories", mustJSON(map[string]any{"name": names("Agent category")})},
		{"PATCH", categoryPath(catID), `{"is_active": false}`},
		{"GET", "/api/staff/locations", ""},
		{"POST", "/api/staff/locations", mustJSON(map[string]any{"building": names("Agent B"), "floor": names("1"), "line": names("1")})},
		{"PATCH", locationPath(e.loc.ID), `{"is_active": false}`},
	}
	for _, r := range routes {
		if rec := e.sendJSON(r.method, r.path, agent, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("Agent %s %s = %d %s, want 403", r.method, r.path, rec.Code, rec.Body)
		}
		if r.method == "GET" {
			if rec := e.withCookie(r.method, r.path, lead); rec.Code != http.StatusOK {
				t.Errorf("Team Lead %s %s = %d, want 200", r.method, r.path, rec.Code)
			}
		}
	}
}
