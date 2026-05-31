package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"dramalist/show-service/db"
)

// ── Mock querier ──────────────────────────────────────────────────────────────

type mockQuerier struct {
	rows []db.CatalogRow
	err  error

	catalogDetail *db.CatalogDetailRow
	catalogErr    error

	actorDetail *db.ActorDetailRow
	actorErr    error
}

func (m *mockQuerier) DiscoverCatalog(_ context.Context, _ string, _ int) ([]db.CatalogRow, error) {
	return m.rows, m.err
}

func (m *mockQuerier) GetCatalogDetail(_ context.Context, _ string) (*db.CatalogDetailRow, error) {
	return m.catalogDetail, m.catalogErr
}

func (m *mockQuerier) GetActorDetail(_ context.Context, _ string) (*db.ActorDetailRow, error) {
	return m.actorDetail, m.actorErr
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func newDiscoverHandler(q db.Querier) (*Handler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	h := &Handler{querier: q}
	r := gin.New()
	r.GET("/shows/public/trending", h.TrendingShows)
	r.GET("/shows/public/recent", h.RecentShows)
	return h, r
}

func sampleCatalogRow(id string) db.CatalogRow {
	genre := []string{"romance"}
	year := 2022
	return db.CatalogRow{
		ID:           id,
		MediaType:    "drama",
		Title:        "Test Drama " + id,
		Genre:        genre,
		AiringStatus: "completed",
		CreatedBy:    "user-1",
		Year:         &year,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestTrendingShows_Success(t *testing.T) {
	rows := []db.CatalogRow{
		sampleCatalogRow("1"),
		sampleCatalogRow("2"),
		sampleCatalogRow("3"),
	}
	_, r := newDiscoverHandler(&mockQuerier{rows: rows})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/shows/public/trending", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp []catalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(resp))
	}
}

func TestTrendingShows_DBError(t *testing.T) {
	_, r := newDiscoverHandler(&mockQuerier{err: errors.New("db down")})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/shows/public/trending", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestRecentShows_Success(t *testing.T) {
	rows := make([]db.CatalogRow, 5)
	for i := range rows {
		rows[i] = sampleCatalogRow(string(rune('a' + i)))
	}
	_, r := newDiscoverHandler(&mockQuerier{rows: rows})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/shows/public/recent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp []catalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(resp))
	}
}

func TestTrendingShows_EmptyResult(t *testing.T) {
	_, r := newDiscoverHandler(&mockQuerier{rows: []db.CatalogRow{}})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/shows/public/trending", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp []catalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected empty array, got %d entries", len(resp))
	}
}
