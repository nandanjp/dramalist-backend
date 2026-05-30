package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"dramalist/review-service/config"
	"dramalist/review-service/kafka"
)

type Handler struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	rdb      *redis.Client
	producer *kafka.Producer
}

func New(cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client, producer *kafka.Producer) *Handler {
	return &Handler{cfg: cfg, pool: pool, rdb: rdb, producer: producer}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)

	reviews := r.Group("/reviews")

	// Static sub-paths registered before /:id wildcard so Gin's radix tree
	// routes them correctly (static nodes take priority over wildcard nodes).
	reviews.GET("/me", h.ListMyReviews)
	reviews.GET("/public/recent", h.RecentPublicReviews)
	reviews.GET("/catalog/:catalogId", h.ListShowReviews)
	reviews.GET("/aggregate/:catalogId", h.GetAggregate)

	reviews.POST("", h.CreateReview)
	reviews.GET("/:id", h.GetReview)
	reviews.PATCH("/:id", h.UpdateReview)
	reviews.DELETE("/:id", h.DeleteReview)
}

func errJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}
