package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"dramalist/show-service/db"
)

func newCatalogHandler(q db.Querier, pool *pgxpool.Pool) (*Handler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	h := &Handler{querier: q, pool: pool}
	r := gin.New()
	r.GET("/catalog/:id", h.GetCatalogEntry)
	r.POST("/catalog", h.CreateCatalogEntry)
	return h, r
}

func TestGetCatalogEntry_NotFound(t *testing.T) {
	_, r := newCatalogHandler(&mockQuerier{catalogDetail: nil, catalogErr: nil}, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/catalog/nonexistent-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetCatalogEntry_DBError(t *testing.T) {
	mq := &mockQuerier{catalogDetail: nil, catalogErr: errorf("db timeout")}
	_, r := newCatalogHandler(mq, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/catalog/some-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetCatalogEntry_Success(t *testing.T) {
	year := 2023
	detail := &db.CatalogDetailRow{
		CatalogRow: db.CatalogRow{
			ID:           "abc-123",
			MediaType:    "drama",
			Title:        "My Drama",
			Genre:        []string{"romance", "comedy"},
			AiringStatus: "completed",
			CreatedBy:    "user-1",
			Year:         &year,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		Cast: []db.CastMemberRow{
			{CastID: "c1", ActorID: "a1", ActorName: "Actor One", Role: "main", SortOrder: 1},
		},
	}
	_, r := newCatalogHandler(&mockQuerier{catalogDetail: detail}, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/catalog/abc-123", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp catalogDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.ID != "abc-123" {
		t.Fatalf("expected id abc-123, got %s", resp.ID)
	}
	if len(resp.Cast) != 1 {
		t.Fatalf("expected 1 cast member, got %d", len(resp.Cast))
	}
	if resp.Cast[0].ActorName != "Actor One" {
		t.Fatalf("unexpected actor name: %s", resp.Cast[0].ActorName)
	}
}

func TestCreateCatalogEntry_MissingTitle(t *testing.T) {
	_, r := newCatalogHandler(nil, nil)

	body, _ := json.Marshal(map[string]any{"media_type": "drama"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Role", "admin")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateCatalogEntry_NotAdmin(t *testing.T) {
	_, r := newCatalogHandler(nil, nil)

	body, _ := json.Marshal(map[string]any{"title": "Drama", "media_type": "drama"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func errorf(msg string) error {
	return &simpleErr{msg}
}

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }
