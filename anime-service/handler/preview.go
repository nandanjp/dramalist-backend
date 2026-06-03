package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type characterPreview struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	NativeName *string    `json:"native_name,omitempty"`
	Image      *string    `json:"image,omitempty"`
	Role       string     `json:"role"`
	VoiceActor *vaPreview `json:"voice_actor,omitempty"`
}

type vaPreview struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Image *string `json:"image,omitempty"`
}

type previewResponse struct {
	AnilistID     int                `json:"anilist_id"`
	Title         string             `json:"title"`
	OriginalTitle *string            `json:"original_title,omitempty"`
	CoverImage    *string            `json:"cover_image,omitempty"`
	Year          *int               `json:"year,omitempty"`
	Episodes      *int               `json:"episodes,omitempty"`
	Duration      *int               `json:"duration_minutes,omitempty"`
	Format        string             `json:"format"`
	Status        string             `json:"status"`
	Genres        []string           `json:"genres"`
	AverageScore  *int               `json:"average_score,omitempty"`
	Synopsis      *string            `json:"synopsis,omitempty"`
	Studio        *string            `json:"studio,omitempty"`
	Country       *string            `json:"country,omitempty"`
	Characters    []characterPreview `json:"characters"`
}

func (h *Handler) Preview(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		errJSON(c, http.StatusBadRequest, "id must be a number")
		return
	}

	media, err := h.client.FetchByID(c.Request.Context(), id)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "AniList fetch failed")
		return
	}

	resp := previewResponse{
		AnilistID:     media.ID,
		Title:         media.PrimaryTitle(),
		OriginalTitle: media.Title.Native,
		CoverImage:    media.CoverImage.Large,
		Year:          media.StartDate.Year,
		Episodes:      media.Episodes,
		Duration:      media.Duration,
		Format:        media.Format,
		Status:        media.Status,
		Genres:        media.Genres,
		AverageScore:  media.AverageScore,
		Synopsis:      media.Description,
		Studio:        media.Studio(),
		Country:       media.CountryOfOrigin,
		Characters:    []characterPreview{},
	}

	if resp.Genres == nil {
		resp.Genres = []string{}
	}

	if media.Characters != nil {
		for _, edge := range media.Characters.Edges {
			cp := characterPreview{
				ID:         edge.Node.ID,
				Name:       strVal(edge.Node.Name.Full),
				NativeName: edge.Node.Name.Native,
				Image:      edge.Node.Image.Large,
				Role:       edge.Role,
			}
			if len(edge.VoiceActors) > 0 {
				va := edge.VoiceActors[0]
				cp.VoiceActor = &vaPreview{
					ID:    va.ID,
					Name:  strVal(va.Name.Full),
					Image: va.Image.Large,
				}
			}
			resp.Characters = append(resp.Characters, cp)
		}
	}

	c.JSON(http.StatusOK, resp)
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(s string) *string { return &s }
