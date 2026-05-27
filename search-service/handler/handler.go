package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"dramalist/search-service/elastic"
)

type Handler struct {
	es *elastic.Client
}

func New(es *elastic.Client) *Handler {
	return &Handler{es: es}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)

	r.GET("/search", h.Search)
}

func errJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
