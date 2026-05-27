package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"dramalist/user-service/config"
)

type Handler struct {
	cfg  *config.Config
	pool *pgxpool.Pool
	rdb  *redis.Client
}

func New(cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client) *Handler {
	return &Handler{cfg: cfg, pool: pool, rdb: rdb}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)

	users := r.Group("/users")
	users.GET("/me", h.GetMe)
	users.PATCH("/me", h.PatchMe)
	users.GET("/me/stats", h.GetMyStats)
	users.GET("/:slug", h.GetBySlug)
}

func errJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}
