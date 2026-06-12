package smoke_test

import (
	"net/http"
	"testing"
	"time"
)

// TestSearchFlow verifies catalog and actor search via search-service.
func TestSearchFlow(t *testing.T) {
	// Ensure there is something to search for.
	requireCatalogEntry(t)

	// ── Catalog search — full-text ────────────────────────────────────────────
	t.Log("step 1: full-text catalog search")
	pollUntil(t, "catalog search returns hits", 20*time.Second, 2*time.Second, func() (bool, error) {
		raw, s := do(t, http.MethodGet, searchURL()+"/search?q=Smoke+Test", nil, nil)
		if s != http.StatusOK {
			return false, nil
		}
		var resp struct {
			Hits  []map[string]any `json:"hits"`
			Total int              `json:"total"`
		}
		decodeJSON(t, raw, &resp)
		return len(resp.Hits) > 0, nil
	})
	logf(t, "full-text search returns hits")

	// ── Catalog search — filtered by type ─────────────────────────────────────
	t.Log("step 2: filtered catalog search (type=drama)")
	raw, status := do(t, http.MethodGet, searchURL()+"/search?q=Smoke&type=drama", nil, nil)
	assertStatus(t, status, http.StatusOK, raw)
	logf(t, "filtered search (type=drama) returned 200")

	// ── Catalog search — Korean language query ────────────────────────────────
	t.Log("step 3: CJK title search")
	raw, status = do(t, http.MethodGet, searchURL()+"/search?q=스모크+테스트", nil, nil)
	assertStatus(t, status, http.StatusOK, raw)
	logf(t, "CJK search returned 200")

	// ── Actor search ──────────────────────────────────────────────────────────
	t.Log("step 4: actor search endpoint")
	raw, status = do(t, http.MethodGet, searchURL()+"/search/actors?q=smoke", nil, nil)
	assertStatus(t, status, http.StatusOK, raw)
	logf(t, "actor search returned 200")
}

// TestSearchFilterCombinations verifies that the search endpoint handles
// combined filter params without erroring.
func TestSearchFilterCombinations(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"by country", "/search?q=drama&country=South+Korea"},
		{"by genre", "/search?q=drama&genre=Drama"},
		{"by year", "/search?q=drama&year=2024"},
		{"by status", "/search?q=drama&status=completed"},
		{"sort by rating", "/search?q=drama&sort=avg_rating:desc"},
		{"combined filters", "/search?q=drama&type=drama&country=South+Korea&sort=avg_rating:desc"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, status := do(t, http.MethodGet, searchURL()+tc.query, nil, nil)
			assertStatus(t, status, http.StatusOK, raw)
		})
	}
}
