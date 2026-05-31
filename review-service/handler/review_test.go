package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestRDB starts an in-process Redis and returns a client connected to it.
func newTestRDB(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		rdb.Close()
		mr.Close()
	})
	return rdb, mr
}

// ── Validation tests (no DB or Redis needed) ──────────────────────────────────

func TestCreateReview_MissingCatalogID(t *testing.T) {
	h := &Handler{}
	r := gin.New()
	r.POST("/reviews", h.CreateReview)

	body, _ := json.Marshal(map[string]any{"rating": 7.5})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/reviews", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "user-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateReview_RatingOutOfRange(t *testing.T) {
	h := &Handler{}
	r := gin.New()
	r.POST("/reviews", h.CreateReview)

	rating := 11.0
	body, _ := json.Marshal(map[string]any{"catalog_id": "some-id", "rating": rating})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/reviews", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "user-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateReview_MissingUserID(t *testing.T) {
	h := &Handler{}
	r := gin.New()
	r.POST("/reviews", h.CreateReview)

	body, _ := json.Marshal(map[string]any{"catalog_id": "some-id", "rating": 7.5})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/reviews", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── Cache tests using miniredis ───────────────────────────────────────────────

func TestGetAggregate_CacheHit(t *testing.T) {
	rdb, mr := newTestRDB(t)

	// Pre-populate cache — pool is nil so we verify the handler never touches DB.
	agg := aggregateResponse{CatalogID: "cat-1", ReviewCount: 5}
	v := 8.2
	agg.AvgRating = &v
	b, _ := json.Marshal(agg)
	mr.Set("review_agg:cat-1", string(b))

	h := &Handler{rdb: rdb}
	r := gin.New()
	r.GET("/reviews/aggregate/:catalogId", h.GetAggregate)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/reviews/aggregate/cat-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp aggregateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.ReviewCount != 5 {
		t.Fatalf("expected review_count 5, got %d", resp.ReviewCount)
	}
	if resp.AvgRating == nil || *resp.AvgRating != 8.2 {
		t.Fatalf("unexpected avg_rating: %v", resp.AvgRating)
	}
}

func TestRecentPublicReviews_CacheHit(t *testing.T) {
	rdb, mr := newTestRDB(t)

	previews := []publicReviewPreview{
		{ID: "r1", CatalogID: "c1", UserID: "u1", Rating: 8.0, CreatedAt: time.Now()},
		{ID: "r2", CatalogID: "c2", UserID: "u2", Rating: 7.5, CreatedAt: time.Now()},
	}
	b, _ := json.Marshal(previews)
	mr.Set(recentReviewsCacheKey, string(b))

	// pool is nil — confirms handler returns from cache before touching DB.
	h := &Handler{rdb: rdb}
	r := gin.New()
	r.GET("/reviews/public/recent", h.RecentPublicReviews)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/reviews/public/recent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp []publicReviewPreview
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 previews, got %d", len(resp))
	}
}
