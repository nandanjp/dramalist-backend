package handler

import (
	"context"
	"errors"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"dramalist/search-service/elastic"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ── Mock searcher ─────────────────────────────────────────────────────────────

type mockSearcher struct {
	results []elastic.SearchResult
	total   int64
	err     error
}

func (m *mockSearcher) Search(_ context.Context, _ elastic.SearchParams) ([]elastic.SearchResult, int64, error) {
	return m.results, m.total, m.err
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func newSearchRouter(s Searcher) *gin.Engine {
	h := &Handler{es: s}
	r := gin.New()
	r.GET("/search", h.Search)
	return r
}

func TestSearch_EmptyQuery(t *testing.T) {
	r := newSearchRouter(&mockSearcher{results: []elastic.SearchResult{}, total: 0})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/search?q=", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp searchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Total != 0 || len(resp.Results) != 0 {
		t.Fatalf("expected empty results, got total=%d results=%d", resp.Total, len(resp.Results))
	}
}

func TestSearch_StoreError(t *testing.T) {
	r := newSearchRouter(&mockSearcher{err: errors.New("es timeout")})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/search?q=goblin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestSearch_WithResults(t *testing.T) {
	results := []elastic.SearchResult{
		{CatalogID: "1", Title: "Goblin", MediaType: "drama"},
		{CatalogID: "2", Title: "Goblin 2", MediaType: "drama"},
	}
	r := newSearchRouter(&mockSearcher{results: results, total: 2})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/search?q=goblin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp searchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
}
