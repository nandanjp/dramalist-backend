package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"dramalist/anime-service/anilist"
)

type Handler struct {
	pool   *pgxpool.Pool
	client *anilist.Client
}

func New(pool *pgxpool.Pool, client *anilist.Client) *Handler {
	return &Handler{pool: pool, client: client}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)

	anime := r.Group("/anime")
	anime.GET("/search", h.Search)
	anime.GET("/:id", h.Preview)
	anime.POST("/import", h.Import)
}

func errJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "anime-service"})
}
