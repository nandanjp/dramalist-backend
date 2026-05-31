package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newMediaRouter() *gin.Engine {
	h := &Handler{}
	r := gin.New()
	r.POST("/media/upload", h.Upload)
	r.GET("/media/entity/:entityType/:entityID", h.ListByEntity)
	return r
}

// ── Upload validation ─────────────────────────────────────────────────────────

func TestUpload_BadEntityType(t *testing.T) {
	r := newMediaRouter()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("entity_type", "invalid_entity")
	w.WriteField("media_type", "avatar")
	w.Close()

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/media/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-User-Id", "00000000-0000-0000-0000-000000000001")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid entity_type, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestUpload_BadMediaType(t *testing.T) {
	r := newMediaRouter()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("entity_type", "user")
	w.WriteField("media_type", "invalid_media")
	w.Close()

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/media/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-User-Id", "00000000-0000-0000-0000-000000000001")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid media_type, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestUpload_MissingFile(t *testing.T) {
	r := newMediaRouter()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("entity_type", "user")
	w.WriteField("media_type", "avatar")
	w.Close()

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/media/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-User-Id", "00000000-0000-0000-0000-000000000001")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file, got %d; body: %s", rec.Code, rec.Body.String())
	}
}
