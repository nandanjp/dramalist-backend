package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"dramalist/search-service/elastic"
)

// Searcher abstracts the Elasticsearch client for testability.
type Searcher interface {
	Search(ctx context.Context, p elastic.SearchParams) ([]elastic.SearchResult, int64, error)
}

type Handler struct {
	es Searcher
}

func New(es *elastic.Client) *Handler {
	return &Handler{es: es}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)

	r.GET("/search", h.Search)
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

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
