package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type recommendRequest struct {
	Mode      string `json:"mode"`
	CatalogID string `json:"catalog_id"`
}

const recommendSystem = `You are a Korean drama and Asian TV show recommendation assistant.
Suggest shows the user would enjoy based on their request.
Keep your response to 2-3 sentences.`

// Recommend handles POST /ai/recommend and streams the response as SSE.
// Supported modes: list_based, show_analysis, similar_to.
func (h *Handler) Recommend(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req recommendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}

	var userPrompt string
	switch req.Mode {
	case "show_analysis":
		userPrompt = fmt.Sprintf("Analyze the appeal of the show with catalog ID %s and explain why viewers enjoy it.", req.CatalogID)
	case "similar_to":
		userPrompt = fmt.Sprintf("Recommend shows similar to the one with catalog ID %s.", req.CatalogID)
	default:
		userPrompt = "Recommend some highly-rated Korean dramas worth watching based on popular taste."
	}

	text, err := h.llm.ChatText(c.Request.Context(), recommendSystem, userPrompt)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "recommendation failed")
		return
	}

	// Sanitize newlines so each SSE data line stays on one line.
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.TrimSpace(text)
	if text == "" {
		text = "No recommendation available."
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	fmt.Fprintf(c.Writer, "data: %s\n\ndata: [DONE]\n\n", text)
	if f, ok := c.Writer.(http.Flusher); ok {
		f.Flush()
	}
}
