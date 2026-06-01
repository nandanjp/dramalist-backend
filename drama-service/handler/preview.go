package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type castMemberPreview struct {
	PersonID      int     `json:"person_id"`
	Name          string  `json:"name"`
	CharacterName string  `json:"character_name"`
	Role          string  `json:"role"`
	PhotoURL      string  `json:"photo_url,omitempty"`
}

type previewResponse struct {
	MDLID         int                 `json:"mdl_id"`
	Title         string              `json:"title"`
	OriginalTitle *string             `json:"original_title,omitempty"`
	PosterURL     *string             `json:"poster_url,omitempty"`
	Year          *int                `json:"year,omitempty"`
	Episodes      *int                `json:"episodes,omitempty"`
	Duration      *int                `json:"duration_minutes,omitempty"`
	Type          string              `json:"type"`
	Country       string              `json:"country"`
	Rating        *float64            `json:"rating,omitempty"`
	Synopsis      *string             `json:"synopsis,omitempty"`
	Status        string              `json:"status"`
	Genres        []string            `json:"genres"`
	Cast          []castMemberPreview `json:"cast"`
}

func (h *Handler) Preview(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		errJSON(c, http.StatusBadRequest, "slug is required")
		return
	}

	detail, err := h.client.FetchDetail(c.Request.Context(), slug)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "MDL fetch failed")
		return
	}

	resp := previewResponse{
		MDLID:         detail.MDLID,
		Title:         detail.Title,
		OriginalTitle: detail.OriginalTitle,
		PosterURL:     detail.PosterURL,
		Year:          detail.Year,
		Episodes:      detail.EpisodeCount,
		Duration:      detail.Duration,
		Type:          detail.Type,
		Country:       detail.Country,
		Rating:        detail.Rating,
		Synopsis:      detail.Synopsis,
		Status:        detail.Status,
		Genres:        detail.Genres,
		Cast:          make([]castMemberPreview, 0, len(detail.Cast)),
	}
	if resp.Genres == nil {
		resp.Genres = []string{}
	}

	for _, cm := range detail.Cast {
		resp.Cast = append(resp.Cast, castMemberPreview{
			PersonID:      cm.PersonID,
			Name:          cm.Name,
			CharacterName: cm.CharacterName,
			Role:          cm.Role,
			PhotoURL:      cm.PhotoURL,
		})
	}

	c.JSON(http.StatusOK, resp)
}
