package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"dramalist/auth-service/config"
)

type Handler struct {
	cfg       *config.Config
	pool      *pgxpool.Pool
	rdb       *redis.Client
	jwtSecret []byte
}

func New(cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client) *Handler {
	return &Handler{cfg: cfg, pool: pool, rdb: rdb, jwtSecret: []byte(cfg.JWTSecret)}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)

	auth := r.Group("/auth")
	auth.POST("/register", h.EmailRegister)
	auth.POST("/login", h.EmailLogin)
	auth.GET("/oauth/:provider", h.OAuthInitiate)
	auth.GET("/oauth/:provider/callback", h.OAuthCallback)
	auth.POST("/token/refresh", h.RefreshToken)
	auth.POST("/logout", h.Logout)
	auth.POST("/logout-all", h.LogoutAll)
	auth.GET("/totp/setup", h.TOTPSetup)
	auth.POST("/totp/confirm", h.TOTPConfirm)
	auth.POST("/totp/verify", h.TOTPVerify)
}

// dbUser holds the columns we commonly fetch from auth_db.users.
type dbUser struct {
	ID          string
	Email       string
	DisplayName string
	TOTPEnabled bool
	IsAdmin     bool
}

// tokenPair is the result of a successful authentication.
type tokenPair struct {
	AccessToken  string
	RefreshToken string
}

// issueTokenPair signs a new JWT access token and generates an opaque refresh
// token, persisting the refresh token in both Redis (fast lookup) and Postgres
// (audit trail / "revoke all sessions" capability).
func (h *Handler) issueTokenPair(ctx context.Context, user dbUser) (tokenPair, error) {
	role := "user"
	if user.IsAdmin {
		role = "admin"
	}
	claims := jwt.MapClaims{
		"userId":      user.ID,
		"email":       user.Email,
		"displayName": user.DisplayName,
		"role":        role,
		"iat":         time.Now().Unix(),
		"exp":         time.Now().Add(time.Duration(h.cfg.AccessTokenTTL) * time.Second).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString(h.jwtSecret)
	if err != nil {
		return tokenPair{}, fmt.Errorf("sign JWT: %w", err)
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return tokenPair{}, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshToken := hex.EncodeToString(b)

	ttl := time.Duration(h.cfg.RefreshTokenTTL) * time.Second
	if err := h.rdb.Set(ctx, "refresh:"+refreshToken, user.ID, ttl).Err(); err != nil {
		return tokenPair{}, fmt.Errorf("store refresh token in redis: %w", err)
	}

	expiresAt := time.Now().Add(ttl)
	_, err = h.pool.Exec(ctx,
		"INSERT INTO refresh_tokens (token, user_id, expires_at) VALUES ($1, $2, $3)",
		refreshToken, user.ID, expiresAt,
	)
	if err != nil {
		return tokenPair{}, fmt.Errorf("persist refresh token: %w", err)
	}

	return tokenPair{AccessToken: accessToken, RefreshToken: refreshToken}, nil
}

// setRefreshCookie writes the refresh token as an HttpOnly cookie.
func (h *Handler) setRefreshCookie(c *gin.Context, refreshToken string) {
	c.SetCookie(
		"drl_refresh",
		refreshToken,
		h.cfg.RefreshTokenTTL,
		"/",
		h.cfg.CookieDomain,
		h.cfg.IsProduction(), // Secure flag
		true,                 // HttpOnly
	)
}

func errJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

func redirect(c *gin.Context, url string) {
	c.Redirect(http.StatusFound, url)
}
