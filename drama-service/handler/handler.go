package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"dramalist/drama-service/mdl"
)

type Handler struct {
	pool   *pgxpool.Pool
	client *mdl.Client
}

func New(pool *pgxpool.Pool, client *mdl.Client) *Handler {
	return &Handler{pool: pool, client: client}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/drama/search", h.Search)
	r.GET("/drama/:slug", h.Preview)
	r.POST("/drama/import", h.Import)
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func errJSON(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"error": msg})
}
