package handler

import (
	"log/slog"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"dramalist/media-service/config"
	"dramalist/media-service/storage"
)

type Handler struct {
	cfg   *config.Config
	pool  *pgxpool.Pool
	store *storage.Store
}

func New(cfg *config.Config, pool *pgxpool.Pool, store *storage.Store) *Handler {
	return &Handler{cfg: cfg, pool: pool, store: store}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)

	media := r.Group("/media")
	media.POST("/upload", h.Upload)
	media.GET("/file/:id", h.ServeFile)
	media.GET("/entity/:entityType/:entityID", h.ListByEntity)
	media.DELETE("/:id", h.Delete)
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
