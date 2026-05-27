package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"dramalist/show-service/config"
	"dramalist/show-service/kafka"
)

type Handler struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	producer *kafka.Producer
}

func New(cfg *config.Config, pool *pgxpool.Pool, producer *kafka.Producer) *Handler {
	return &Handler{cfg: cfg, pool: pool, producer: producer}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)

	// Internal route — no nginx location for /internal/, so it is only reachable
	// from within the Docker network. Used by search-service for cold-start backfill.
	r.GET("/internal/shows/all", h.ExportAllShows)

	shows := r.Group("/shows")

	// Static sub-path must be registered before the :id wildcard.
	shows.GET("/users/:userID", h.ListPublicShows)

	shows.GET("", h.ListShows)
	shows.POST("", h.CreateShow)
	shows.GET("/:id", h.GetShow)
	shows.PATCH("/:id", h.UpdateShow)
	shows.DELETE("/:id", h.DeleteShow)
}

func errJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}
