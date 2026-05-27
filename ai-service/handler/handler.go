package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"dramalist/ai-service/llm"
)

type Handler struct {
	llm *llm.Client
}

func New(llm *llm.Client) *Handler {
	return &Handler{llm: llm}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)

	ai := r.Group("/ai")
	ai.POST("/recommendations", h.Recommendations)
	ai.POST("/mood-search", h.MoodSearch)
	ai.POST("/shows/:showID/summary", h.Summary)
}

func errJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
