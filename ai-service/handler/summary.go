package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type reviewInput struct {
	Rating  float64 `json:"rating"`
	Content string  `json:"content"`
}

type summaryRequest struct {
	ShowTitle string       `json:"show_title"`
	Reviews   []reviewInput `json:"reviews" binding:"required,min=1"`
}

type summaryResponse struct {
	Summary    string   `json:"summary"`
	Sentiment  string   `json:"sentiment"`
	Highlights []string `json:"highlights"`
	Criticisms []string `json:"criticisms"`
}

const summarySystem = `You are a TV show review summarizer. Analyze user reviews and produce a concise summary of community sentiment.

Return ONLY valid JSON with this exact structure, no other text:
{"summary": "...", "sentiment": "positive|mixed|negative", "highlights": [...], "criticisms": [...]}

Rules:
- summary: 2-3 sentences capturing the overall community view
- sentiment: "positive" if mostly praised, "negative" if mostly criticized, "mixed" if opinions are divided
- highlights: 2-4 short phrases (5-8 words each) about what people loved
- criticisms: 0-3 short phrases about common complaints; use an empty array if overwhelmingly positive
- Base the analysis on the actual review content, not just the numeric ratings`

func (h *Handler) Summary(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req summaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}
	// Cap to 50 reviews to avoid massive prompts.
	if len(req.Reviews) > 50 {
		req.Reviews = req.Reviews[:50]
	}

	var sb strings.Builder
	if req.ShowTitle != "" {
		sb.WriteString(fmt.Sprintf("Show: %s\n\n", req.ShowTitle))
	}

	written := 0
	for i, r := range req.Reviews {
		if r.Content == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("%d. [%.1f/10] %s\n", i+1, r.Rating, r.Content))
		written++
	}

	if written == 0 {
		errJSON(c, http.StatusBadRequest, "no reviews with content provided")
		return
	}

	userPrompt := fmt.Sprintf("Summarize these %d reviews:\n\n%s", written, sb.String())

	raw, err := h.llm.Chat(c.Request.Context(), summarySystem, userPrompt)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "summary failed")
		return
	}

	var result summaryResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to parse summary response")
		return
	}
	if result.Highlights == nil {
		result.Highlights = []string{}
	}
	if result.Criticisms == nil {
		result.Criticisms = []string{}
	}

	c.JSON(http.StatusOK, result)
}
