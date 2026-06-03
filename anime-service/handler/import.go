package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"dramalist/anime-service/anilist"
	animdb "dramalist/anime-service/db"
)

type importRequest struct {
	AnilistID      int   `json:"anilist_id" binding:"required"`
	VoiceActorIDs  []int `json:"voice_actor_ids"`
}

func (h *Handler) Import(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}

	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req importRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "anilist_id is required")
		return
	}

	ctx := c.Request.Context()

	exists, err := animdb.CheckAnilistExists(ctx, h.pool, req.AnilistID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	if exists {
		errJSON(c, http.StatusConflict, "already imported")
		return
	}

	media, err := h.client.FetchByID(ctx, req.AnilistID)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "AniList fetch failed")
		return
	}

	params := animdb.CatalogInsertParams{
		MediaType:       "anime",
		Title:           media.PrimaryTitle(),
		OriginalTitle:   media.Title.Native,
		Synopsis:        media.Description,
		PosterURL:       media.CoverImage.Large,
		Year:            media.StartDate.Year,
		Country:         media.CountryOfOrigin,
		Language:        anilist.CountryToLanguage(media.CountryOfOrigin),
		EpisodeCount:    media.Episodes,
		DurationMinutes: media.Duration,
		Genre:           media.LowercaseGenres(),
		AiringStatus:    anilist.StatusToAiringStatus(media.Status),
		AnilistID:       media.ID,
		Studio:          media.Studio(),
		AnimeFormat:     media.Format,
		CreatedBy:       userID,
	}

	catalogID, err := animdb.InsertCatalog(ctx, h.pool, params)
	if err != nil {
		slog.Error("insert catalog failed", "err", err, "anilist_id", req.AnilistID)
		errJSON(c, http.StatusInternalServerError, "failed to create catalog entry")
		return
	}

	allowedVAs := make(map[int]bool, len(req.VoiceActorIDs))
	for _, id := range req.VoiceActorIDs {
		allowedVAs[id] = true
	}

	castImported := 0
	if media.Characters != nil {
		for i, edge := range media.Characters.Edges {
			if len(edge.VoiceActors) == 0 {
				continue
			}
			va := edge.VoiceActors[0]
			if va.Name.Full == nil {
				continue
			}
			if len(allowedVAs) > 0 && !allowedVAs[va.ID] {
				continue
			}

			actorParams := animdb.ActorParams{
				Name:            *va.Name.Full,
				NativeName:      va.Name.Native,
				Nationality:     strPtr("Japanese"),
				Birthdate:       parseBirthdate(va.DateOfBirth),
				ProfileImageURL: va.Image.Large,
				Biography:       va.Description,
				AnilistPersonID: va.ID,
			}

			actorID, err := animdb.UpsertActor(ctx, h.pool, actorParams)
			if err != nil {
				slog.Warn("upsert actor failed, skipping", "actor", *va.Name.Full, "err", err)
				continue
			}

			characterName := strVal(edge.Node.Name.Full)
			role := anilist.MapCharacterRole(edge.Role)
			if err := animdb.InsertCastMember(ctx, h.pool, catalogID, actorID, characterName, role, i); err != nil {
				slog.Warn("insert cast member failed, skipping", "character", characterName, "err", err)
				continue
			}
			castImported++
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"catalog_id":    catalogID,
		"title":         media.PrimaryTitle(),
		"anilist_id":    media.ID,
		"cast_imported": castImported,
	})
}

func parseBirthdate(d anilist.ALDate) *time.Time {
	if d.Year == nil || d.Month == nil || d.Day == nil {
		return nil
	}
	t := time.Date(*d.Year, time.Month(*d.Month), *d.Day, 0, 0, 0, 0, time.UTC)
	return &t
}
