package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

func init() {
	gin.SetMode(gin.TestMode)
}

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

// ── JWT signing ───────────────────────────────────────────────────────────────

func TestSignAccessJWT_ValidClaims(t *testing.T) {
	secret := []byte("test-secret-key")
	user := dbUser{
		ID:          "user-123",
		Email:       "test@example.com",
		DisplayName: "Test User",
		IsAdmin:     false,
	}

	tokenStr, err := signAccessJWT(secret, user, 15*time.Minute)
	if err != nil {
		t.Fatalf("signAccessJWT failed: %v", err)
	}

	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		return secret, nil
	})
	if err != nil {
		t.Fatalf("JWT parse failed: %v", err)
	}

	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		t.Fatal("JWT is invalid")
	}
	if claims["userId"] != "user-123" {
		t.Fatalf("expected userId user-123, got %v", claims["userId"])
	}
	if claims["email"] != "test@example.com" {
		t.Fatalf("expected email test@example.com, got %v", claims["email"])
	}
	if claims["role"] != "user" {
		t.Fatalf("expected role user, got %v", claims["role"])
	}
}

func TestSignAccessJWT_AdminRole(t *testing.T) {
	secret := []byte("test-secret-key")
	user := dbUser{ID: "admin-1", Email: "admin@example.com", IsAdmin: true}

	tokenStr, err := signAccessJWT(secret, user, 15*time.Minute)
	if err != nil {
		t.Fatalf("signAccessJWT failed: %v", err)
	}

	tok, _ := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) { return secret, nil })
	claims := tok.Claims.(jwt.MapClaims)
	if claims["role"] != "admin" {
		t.Fatalf("expected role admin, got %v", claims["role"])
	}
}

// ── Refresh token handler ─────────────────────────────────────────────────────

func TestRefreshToken_MissingToken(t *testing.T) {
	h := &Handler{}
	r := gin.New()
	r.POST("/auth/token/refresh", h.RefreshToken)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/token/refresh", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestRefreshToken_ExpiredToken(t *testing.T) {
	rdb, mr := newTestRDB(t)

	// Store a refresh token with 1-second TTL, then fast-forward past expiry.
	mr.Set("refresh:expired-tok", "user-123")
	mr.SetTTL("refresh:expired-tok", time.Second)
	mr.FastForward(2 * time.Second)

	h := &Handler{rdb: rdb}
	r := gin.New()
	r.POST("/auth/token/refresh", h.RefreshToken)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/token/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "drl_refresh", Value: "expired-tok"})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d; body: %s", w.Code, w.Body.String())
	}
}
