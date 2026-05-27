package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"dramalist/search-service/elastic"
)

type searchResponse struct {
	Results []elastic.SearchResult `json:"results"`
	Total   int64                  `json:"total"`
	Page    int                    `json:"page"`
	Limit   int                    `json:"limit"`
}

func (h *Handler) Search(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	mineOnly := c.Query("mine") == "true"

	params := elastic.SearchParams{
		Query:    c.Query("q"),
		UserID:   userID,
		MineOnly: mineOnly,
		Status:   c.Query("status"),
		Genre:    c.Query("genre"),
		Page:     page,
		Limit:    limit,
	}

	results, total, err := h.es.Search(c.Request.Context(), params)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "search failed")
		return
	}

	c.JSON(http.StatusOK, searchResponse{
		Results: results,
		Total:   total,
		Page:    page,
		Limit:   limit,
	})
}
