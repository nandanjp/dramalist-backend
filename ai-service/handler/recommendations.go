package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type recommendationsRequest struct {
	GenreBreakdown map[string]int `json:"genre_breakdown"`
	AvgRating      float64        `json:"avg_rating"`
	TotalWatched   int            `json:"total_watched"`
	RecentShows    []string       `json:"recent_shows"`
}

type recommendation struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type recommendationsResponse struct {
	Recommendations []recommendation `json:"recommendations"`
}

const recommendationsSystem = `You are a Korean drama and Asian TV show recommendation assistant. Suggest shows based on a user's watch history and taste.

Return ONLY valid JSON with this exact structure, no other text:
{"recommendations": [{"title": "...", "reason": "..."}]}

Rules:
- Return exactly 6 recommendations
- Use the English title; include the original title in parentheses if it is well known by both
- Reason should be 1-2 sentences explaining why it fits their taste
- Do not recommend shows already in their recently watched list
- Prioritize variety within their preferred genres`

func (h *Handler) Recommendations(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req recommendationsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}

	genreParts := make([]string, 0, len(req.GenreBreakdown))
	for genre, count := range req.GenreBreakdown {
		genreParts = append(genreParts, fmt.Sprintf("%s (%d shows)", genre, count))
	}
	genreStr := "none"
	if len(genreParts) > 0 {
		genreStr = strings.Join(genreParts, ", ")
	}

	recentStr := "none"
	if len(req.RecentShows) > 0 {
		recentStr = strings.Join(req.RecentShows, ", ")
	}

	userPrompt := fmt.Sprintf(
		"User has watched %d shows total with an average rating of %.1f/10.\nGenre breakdown: %s.\nRecently watched: %s.\nRecommend shows they would enjoy.",
		req.TotalWatched, req.AvgRating, genreStr, recentStr,
	)

	raw, err := h.llm.Chat(c.Request.Context(), recommendationsSystem, userPrompt)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "recommendation failed")
		return
	}

	var result recommendationsResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to parse recommendation response")
		return
	}
	if result.Recommendations == nil {
		result.Recommendations = []recommendation{}
	}

	c.JSON(http.StatusOK, result)
}
