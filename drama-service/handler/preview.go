package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type castMemberPreview struct {
	TMDBPersonID int    `json:"tmdb_person_id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name,omitempty"`
	Character    string `json:"character,omitempty"`
	ProfileURL   string `json:"profile_url,omitempty"`
	Order        int    `json:"order"`
}

type previewResponse struct {
	TMDBID        int                 `json:"tmdb_id"`
	Title         string              `json:"title"`
	OriginalTitle string              `json:"original_title,omitempty"`
	PosterURL     string              `json:"poster_url,omitempty"`
	Year          *int                `json:"year,omitempty"`
	Episodes      *int                `json:"episodes,omitempty"`
	Duration      *int                `json:"duration_minutes,omitempty"`
	Country       string              `json:"country,omitempty"`
	Synopsis      string              `json:"synopsis,omitempty"`
	Status        string              `json:"status"`
	Genres        []string            `json:"genres"`
	Cast          []castMemberPreview `json:"cast"`
}

func (h *Handler) Preview(c *gin.Context) {
	idStr := c.Param("id")
	tmdbID, err := strconv.Atoi(idStr)
	if err != nil || tmdbID <= 0 {
		errJSON(c, http.StatusBadRequest, "id must be a positive integer")
		return
	}

	ctx := c.Request.Context()

	detail, err := h.tmdb.FetchTV(ctx, tmdbID)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "TMDB fetch failed")
		return
	}

	resp := previewResponse{
		TMDBID:   detail.ID,
		Title:    detail.Name,
		Synopsis: detail.Overview,
		Status:   strings.ToLower(detail.Status),
		Genres:   make([]string, 0, len(detail.Genres)),
		Cast:     make([]castMemberPreview, 0),
	}

	if detail.OriginalName != "" && detail.OriginalName != detail.Name {
		resp.OriginalTitle = detail.OriginalName
	}
	if detail.PosterPath != "" {
		resp.PosterURL = h.tmdb.PosterURL(detail.PosterPath)
	}
	if len(detail.FirstAirDate) >= 4 {
		if y, err := strconv.Atoi(detail.FirstAirDate[:4]); err == nil {
			resp.Year = &y
		}
	}
	if detail.NumberOfEpisodes > 0 {
		ep := detail.NumberOfEpisodes
		resp.Episodes = &ep
	}
	if len(detail.EpisodeRunTime) > 0 && detail.EpisodeRunTime[0] > 0 {
		d := detail.EpisodeRunTime[0]
		resp.Duration = &d
	}
	for _, g := range detail.Genres {
		resp.Genres = append(resp.Genres, g.Name)
	}

	credits, err := h.tmdb.FetchTVCredits(ctx, tmdbID)
	if err == nil {
		limit := 20
		for i, cm := range credits {
			if i >= limit {
				break
			}
			entry := castMemberPreview{
				TMDBPersonID: cm.ID,
				Name:         cm.Name,
				OriginalName: cm.OriginalName,
				Character:    cm.Character,
				Order:        cm.Order,
			}
			if cm.ProfilePath != "" {
				entry.ProfileURL = h.tmdb.ProfileURL(cm.ProfilePath)
			}
			resp.Cast = append(resp.Cast, entry)
		}
	}

	c.JSON(http.StatusOK, resp)
}
