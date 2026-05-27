package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type moodSearchRequest struct {
	Prompt string `json:"prompt" binding:"required"`
}

type moodSearchResponse struct {
	Query  string   `json:"query"`
	Genres []string `json:"genres"`
	Tags   []string `json:"tags"`
}

const moodSearchSystem = `You are a search assistant for a drama and TV show tracking app. Convert a user's mood or description into structured search parameters.

Return ONLY valid JSON with this exact structure, no other text:
{"query": "...", "genres": [...], "tags": [...]}

Available genres (use only from this list): action, comedy, drama, fantasy, horror, mystery, romance, sci-fi, thriller
Common tags (pick the most relevant): healing, slice-of-life, time-travel, detective, historical, school, office, family, revenge, supernatural, friendship, political, legal, medical, sports, food

Rules:
- query: 2-5 words capturing the essence, suitable for a text search
- genres: 1-3 genres from the available list only
- tags: 2-5 relevant tags
- If the user references a specific show (e.g. "like Signal"), interpret their taste, do not return the title itself`

func (h *Handler) MoodSearch(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req moodSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}

	userPrompt := fmt.Sprintf("Find me: %s", req.Prompt)

	raw, err := h.llm.Chat(c.Request.Context(), moodSearchSystem, userPrompt)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "mood search failed")
		return
	}

	var result moodSearchResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to parse search response")
		return
	}
	if result.Genres == nil {
		result.Genres = []string{}
	}
	if result.Tags == nil {
		result.Tags = []string{}
	}

	c.JSON(http.StatusOK, result)
}
