package handler

import (
	"log/slog"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"dramalist/user-service/config"
	"dramalist/user-service/db"
)

type Handler struct {
	cfg   *config.Config
	store db.Store
	rdb   *redis.Client
}

func New(cfg *config.Config, store db.Store, rdb *redis.Client) *Handler {
	return &Handler{cfg: cfg, store: store, rdb: rdb}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)

	users := r.Group("/users")
	users.GET("/me", h.GetMe)
	users.PATCH("/me", h.PatchMe)
	users.GET("/me/stats", h.GetMyStats)
	users.GET("/admin/list", h.AdminListUsers) // registered before /:slug wildcard
	users.GET("/:slug", h.GetBySlug)
}

func errJSON(c *gin.Context, status int, msg string) {
	if status >= 500 {
		slog.Error("internal error",
			"status", status,
			"msg", msg,
			"request_id", c.GetString("request_id"),
			"path", c.Request.URL.Path,
		)
	}
	c.JSON(status, gin.H{"error": msg})
}
